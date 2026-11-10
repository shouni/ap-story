package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/shouni/gcp-kit/worker"
	"github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-utils/slogctx"

	"github.com/shouni/ap-story/internal/domain"
)

// Dependencies is the bundle of dependencies Runner needs. New checks for missing required
// dependencies at construction, so a missing dependency is not discovered only at run time.
type Dependencies struct {
	Ops    *ports.Operations
	Reader ports.ContentReader
	Writer remoteio.Writer
	// Layout is the image aspect ratio and resolution. The kit does not hold them as configuration and
	// receives them per call, so their default comes from the app. The zero value means the kit's defaults (3:4 / 1K / 2K).
	Layout ImageLayout
	// Bucket is the name of the GCS bucket holding job artifacts (without gs://).
	Bucket string
	// Planner decides the execution plan per command.
	// Unset, the zero-value DefaultPlanner is used (optional).
	Planner Planner
	// Notifier notifies job completion and failure (optional). Unset, it does nothing.
	Notifier domain.Notifier
	// Timeout caps the run time of one task. 0 or less means unlimited (optional).
	Timeout time.Duration
	// JobStatus is where job progress is recorded. Unset, status recording and
	// the re-run guard are disabled (optional).
	JobStatus domain.JobStatusStore
}

// noopNotifier is the do-nothing implementation used when no Notifier is set.
type noopNotifier struct{}

func (noopNotifier) NotifyComplete(context.Context, domain.Task) error     { return nil }
func (noopNotifier) NotifyError(context.Context, domain.Task, error) error { return nil }

// Runner is the worker pipeline that feeds a domain.Task through the step sequence for its command, in order.
type Runner struct {
	deps Dependencies
}

var _ domain.Pipeline = (*Runner)(nil)

// New validates the dependencies and creates a Runner. It returns an error when a required dependency is
// missing, so a misconfiguration is detected at startup rather than at run time.
func New(deps Dependencies) (*Runner, error) {
	// The batch generators (Ops.Panel.GenerateAllPanels / Ops.Page.ComposeAllPages) are called directly from steps, so if missing,
	// fail at startup rather than in the middle of a job. workflow.New always sets them, so this
	// only catches a hand-assembled Operations.
	var ops = deps.Ops
	var missing []string
	for _, dep := range []struct {
		name string
		ok   bool
	}{
		{"Ops", ops != nil},
		{"Ops.Panel", ops != nil && ops.Panel != nil},
		{"Ops.Page", ops != nil && ops.Page != nil},
		{"Reader", deps.Reader != nil},
		{"Writer", deps.Writer != nil},
		{"Bucket", deps.Bucket != ""},
	} {
		if !dep.ok {
			missing = append(missing, dep.name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("pipeline runner missing required dependencies: %s", strings.Join(missing, ", "))
	}
	if deps.Planner == nil {
		deps.Planner = DefaultPlanner{}
	}
	if deps.Notifier == nil {
		deps.Notifier = noopNotifier{}
	}
	return &Runner{deps: deps}, nil
}

// Execute satisfies worker.TaskExecutor.
//
// A job's life (redelivery guard → validation → run → recording the outcome) belongs to gcp-kit/worker.Lifecycle,
// and this only hands over what goes into each stage (the worker convention in public-docs). Panic recovery,
// the run-time cap and detaching the ctx for recording the outcome are the library's job and are not written here.
func (r *Runner) Execute(ctx context.Context, task domain.Task) error {
	return r.lifecycle().Execute(ctx, task)
}

// lifecycle fits this app's content into each stage of a job's life.
func (r *Runner) lifecycle() worker.Lifecycle[domain.Task, string] {
	// The destination is taken from Dependencies every time, so that recording works even when a test
	// assembles a Runner as a literal; creating a Recorder is cheap.
	status := newStatusRecorder(r.deps.JobStatus)
	return worker.Lifecycle[domain.Task, string]{
		// Put job_id / command on every log line this job emits from here on,
		// so each step's logs can be followed per job.
		Prepare: func(ctx context.Context, task domain.Task) context.Context {
			return slogctx.With(ctx,
				slog.String("job_id", task.JobID),
				slog.String("command", string(task.Command)),
			)
		},
		Labels: func(task domain.Task) map[string]string {
			return map[string]string{"job_id": task.JobID, "command": string(task.Command)}
		},
		Begin: func(ctx context.Context, task domain.Task) (bool, error) {
			return r.begin(ctx, status, task)
		},
		// If the request itself is invalid, redelivering it fails on the same line (Lifecycle wraps it as Permanent).
		Validate: func(task domain.Task) error { return task.ValidateSubmission() },
		Run: func(ctx context.Context, task domain.Task) (string, error) {
			return r.run(ctx, &task)
		},
		Finish: func(ctx context.Context, task domain.Task, title string, cause error) error {
			return r.recordOutcome(ctx, &task, status, title, cause)
		},
		Timeout: r.deps.Timeout,
	}
}

// begin is the guard that keeps a Cloud Tasks redelivery from rebuilding a completed job.
//
// Returning an error even once, for example when a notice fails, causes a redelivery, so without stopping
// here the image generation cost is simply paid twice. If not completed, running is recorded here.
// Placing it before input validation puts every attempt into Attempts, and every job reaches a terminal
// state via running. OutputDir does not depend on validation, so it is resolved first
// (if it cannot be resolved, the record only checks). If the state cannot be read no decision is possible,
// so it returns an error without running.
func (r *Runner) begin(ctx context.Context, status statusRecorder, task domain.Task) (bool, error) {
	outputDir, _ := domain.JobOutputDir(r.deps.Bucket, task.JobID)
	return status.begin(ctx, &task, outputDir)
}

// Run runs the step sequence for the task's command in order and leaves only the artifacts.
//
// It records no status, sends no notice and has no redelivery guard. Those belong to Execute, the
// Cloud Tasks worker's entry point; Run is the core method for running generation synchronously and checking the result.
func (r *Runner) Run(ctx context.Context, task *domain.Task) error {
	if err := task.ValidateSubmission(); err != nil {
		return fmt.Errorf("invalid task: %w", err)
	}
	_, err := r.run(ctx, task)
	return err
}

// run is the body that executes the step sequence. On success it returns the title for the record.
//
// The run-time cap (which keeps an unresponsive image generation from occupying the Cloud Run
// instance) is applied by Lifecycle to Run only, so it is not written here.
// Input validation has also been done by Lifecycle's Validate.
func (r *Runner) run(ctx context.Context, task *domain.Task) (string, error) {
	runCtx := ctx

	outputDir, err := domain.JobOutputDir(r.deps.Bucket, task.JobID)
	if err != nil {
		return "", fmt.Errorf("resolve output dir: %w", err)
	}
	designJobOutputDir, err := domain.DesignJobOutputDir(r.deps.Bucket, task.JobID)
	if err != nil {
		return "", fmt.Errorf("resolve design job output dir: %w", err)
	}

	pc := &Context{
		Task:               task,
		OutputDir:          outputDir,
		DesignJobOutputDir: designJobOutputDir,
		CharacterOutputDir: domain.CharacterAssetDir(r.deps.Bucket),
		Ops:                r.deps.Ops,
		Reader:             r.deps.Reader,
		Writer:             r.deps.Writer,
		Layout:             r.deps.Layout,
	}

	steps, err := r.deps.Planner.Plan(task)
	if err != nil {
		return "", err
	}
	for _, step := range steps {
		if stepErr := step.Execute(runCtx, pc); stepErr != nil {
			r.savePartialResults(runCtx, pc)
			return "", fmt.Errorf("step %s: %w", step.Name(), stepErr)
		}
	}

	// If the script has been generated the title is filled in. It is recorded for the UI to show in listings.
	if pc.Manga != nil {
		return pc.Manga.Title, nil
	}
	return "", nil
}

// recordOutcome records the terminal state and sends the notice along the same path for success and failure
// (Lifecycle's Finish).
//
// Where the cutoff itself is the reason for terminating (PIPELINE_TIMEOUT firing, or the request being
// cancelled because the Cloud Tasks dispatch deadline was exceeded), the caller's ctx has already
// expired. Lifecycle does the detaching, and success and failure both go through here, so code that
// forgets to detach on only one side cannot be written.
func (r *Runner) recordOutcome(ctx context.Context, task *domain.Task, status statusRecorder, title string, cause error) error {
	outcomeCtx := ctx

	if cause != nil {
		status.markFailed(outcomeCtx, task, cause)
		if notifyErr := r.deps.Notifier.NotifyError(outcomeCtx, *task, cause); notifyErr != nil {
			slog.ErrorContext(ctx, "failed to send error notification",
				"job_id", task.JobID, "original_error", cause, "notification_error", notifyErr)
		}
		return cause
	}

	// Record, then notify. Do not tell anyone a job succeeded before it has been recorded.
	status.markSucceeded(outcomeCtx, task, title)
	if notifyErr := r.deps.Notifier.NotifyComplete(outcomeCtx, *task); notifyErr != nil {
		slog.WarnContext(ctx, "success notification failed", "job_id", task.JobID, "error", notifyErr)
	}
	return nil
}

// partialSaveTimeout is the time allowed for saving on failure.
// It runs detached from the caller's context, so it is kept short.
const partialSaveTimeout = 30 * time.Second

// savePartialResults saves what was achieved up to the point a step failed.
//
// go-comic-kit's batch generation records partial success in the state it returns, so without saving
// here, generated images would be left in GCS with no reference from the state.
// A Cloud Tasks retry would also start over, paying the image generation cost twice.
// Once saved, a re-run only needs the panels and pages not yet generated.
//
// Saving uses a context detached from the caller. A PIPELINE_TIMEOUT cutoff is exactly when the
// results should be kept, and that ctx has already expired. A failure of the save itself is only
// a warning and does not overwrite the original failure reason.
func (r *Runner) savePartialResults(ctx context.Context, pc *Context) {
	if pc.Manga == nil {
		return
	}

	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), partialSaveTimeout)
	defer cancel()

	if err := (SaveStateStep{}).Execute(saveCtx, pc); err != nil {
		slog.WarnContext(ctx, "failed to save partial results", "error", err)
		return
	}
	slog.InfoContext(ctx, "saved partial results before reporting failure")
}
