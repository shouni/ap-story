package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shouni/ap-story/internal/domain"
	"github.com/shouni/go-job-kit/jobstatus"
)

// fakeJobStatusStore is an in-memory job status store for tests.
type fakeJobStatusStore struct {
	mu       sync.Mutex
	statuses map[string]domain.JobStatus
	saved    []domain.JobState
}

func newFakeJobStatusStore() *fakeJobStatusStore {
	return &fakeJobStatusStore{statuses: map[string]domain.JobStatus{}}
}

// It honours the context, as the real GCS client does. Ignoring it here would let the tests
// pass even when the terminal record of a cut-off job is written with an already-cancelled
// context, missing the bug where the state sticks at running.
func (s *fakeJobStatusStore) Save(ctx context.Context, _ string, status domain.JobStatus) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses[status.JobID] = status
	s.saved = append(s.saved, status.State)
	return nil
}

func (s *fakeJobStatusStore) Get(ctx context.Context, jobID string) (domain.JobStatus, error) {
	if err := ctx.Err(); err != nil {
		return domain.JobStatus{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	status, ok := s.statuses[jobID]
	if !ok {
		// An unrecorded job returns a wrapped ErrNotFound (the Store contract). Returning a bare error
		// would be classified as "could not read" and the re-run guard would return an error.
		return domain.JobStatus{}, fmt.Errorf("%w: not recorded", jobstatus.ErrNotFound)
	}
	return status, nil
}

func (s *fakeJobStatusStore) states() []domain.JobState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.JobState(nil), s.saved...)
}

func equalStates(t *testing.T, got []domain.JobState, want ...domain.JobState) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("states = %v, want %v", got, want)
		}
	}
}

func TestExecuteRecordsRunningThenSucceeded(t *testing.T) {
	store := newFakeJobStatusStore()
	runner := newStatusTestRunner(t, store)

	if err := runner.Execute(context.Background(), statusTestTask()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	equalStates(t, store.states(), domain.JobStateRunning, domain.JobStateSucceeded)
	final := store.statuses[statusTestJobID]
	if final.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", final.Attempts)
	}
	if final.OutputDir == "" {
		t.Error("OutputDir was not recorded")
	}
}

func TestExecuteRecordsFailureWithReason(t *testing.T) {
	store := newFakeJobStatusStore()
	runner := newStatusTestRunner(t, store, failingStep{})

	if err := runner.Execute(context.Background(), statusTestTask()); err == nil {
		t.Fatal("Execute() error = nil, want error")
	}

	equalStates(t, store.states(), domain.JobStateRunning, domain.JobStateFailed)
	if store.statuses[statusTestJobID].Error == "" {
		t.Error("the failure reason was not recorded")
	}
}

// A Cloud Tasks redelivery does not redo the generation of a completed job.
func TestExecuteSkipsAlreadySucceededJob(t *testing.T) {
	store := newFakeJobStatusStore()
	counter := &countingStep{}
	runner := newStatusTestRunner(t, store, counter)

	task := statusTestTask()
	if err := runner.Execute(context.Background(), task); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if err := runner.Execute(context.Background(), task); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if counter.calls != 1 {
		t.Fatalf("step calls = %d, want 1 (a completed job was run again)", counter.calls)
	}
}

// A failed job can be redone on redelivery (the guard is not too strict).
func TestExecuteRetriesFailedJob(t *testing.T) {
	store := newFakeJobStatusStore()

	failing := newStatusTestRunner(t, store, failingStep{})
	if err := failing.Execute(context.Background(), statusTestTask()); err == nil {
		t.Fatal("Execute() error = nil, want error")
	}

	ok := newStatusTestRunner(t, store)
	if err := ok.Execute(context.Background(), statusTestTask()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	final := store.statuses[statusTestJobID]
	if final.State != domain.JobStateSucceeded {
		t.Fatalf("State = %q, want succeeded", final.State)
	}
	if final.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2 (the attempt count was not carried over)", final.Attempts)
	}
}

// A job whose generation completes almost exactly when the deadline (PIPELINE_TIMEOUT) fires is still
// recorded as succeeded and notified.
//
// The deadline must cover only the step execution. Overriding the whole ctx with the deadline makes
// the success record of a job completing around the deadline fail with DeadlineExceeded, and the state
// sticks at running while the images are in GCS. story-queue has max_attempts = 1, so
// no retry comes either, and it can only be fixed by hand.
func TestExecuteRecordsSuccessCompletedAtDeadline(t *testing.T) {
	store := newFakeJobStatusStore()
	notifier := &fakeNotifier{}
	runner := newOutcomeTestRunner(t, store, notifier, 50*time.Millisecond, deadlineStep{})

	if err := runner.Execute(context.Background(), statusTestTask()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	equalStates(t, store.states(), domain.JobStateRunning, domain.JobStateSucceeded)
	if len(notifier.completed) != 1 {
		t.Errorf("completed = %v, want one notification (a completion at the deadline was not notified)", notifier.completed)
	}
}

// A job whose generation completes almost exactly when the caller disconnects (the Cloud Tasks dispatch
// deadline is exceeded) is still recorded as succeeded and notified. Unlike PIPELINE_TIMEOUT, this
// happens even when the pipeline sets no cap.
func TestExecuteRecordsSuccessWhenCallerCancelsAtCompletion(t *testing.T) {
	store := newFakeJobStatusStore()
	notifier := &fakeNotifier{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := newOutcomeTestRunner(t, store, notifier, 0, cancelStep{cancel: cancel})

	if err := runner.Execute(ctx, statusTestTask()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	equalStates(t, store.states(), domain.JobStateRunning, domain.JobStateSucceeded)
	if len(notifier.completed) != 1 {
		t.Errorf("completed = %v, want one notification (a completion at disconnect was not notified)", notifier.completed)
	}
}

// A job rejected by input validation still goes through running to failed.
// If the record were only failed, it could not be told apart from a job that was enqueued but never ran.
func TestExecuteRecordsRunningBeforeValidation(t *testing.T) {
	store := newFakeJobStatusStore()
	runner := newOutcomeTestRunner(t, store, nil, 0)

	// Missing both SourceURL and SourceText fails ValidateSubmission.
	invalid := domain.Task{Command: domain.TaskCommandComposeComic, JobID: statusTestJobID}
	if err := runner.Execute(context.Background(), invalid); err == nil {
		t.Fatal("Execute() error = nil, want error")
	}

	equalStates(t, store.states(), domain.JobStateRunning, domain.JobStateFailed)
}

// The pipeline works normally without a configured store.
func TestExecuteWorksWithoutStatusStore(t *testing.T) {
	counter := &countingStep{}
	runner := newStatusTestRunner(t, nil, counter)

	if err := runner.Execute(context.Background(), statusTestTask()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if counter.calls != 1 {
		t.Errorf("step calls = %d, want 1", counter.calls)
	}
}

// statusTestJobID is the fixed job ID used by the job status tests.
const statusTestJobID = "c20260726-120000-abcd1234"

func statusTestTask() domain.Task {
	return domain.Task{
		Command:    domain.TaskCommandComposeComic,
		JobID:      statusTestJobID,
		SourceText: "元文章",
	}
}

// staticPlanner is a test Planner that returns the given step sequence as is.
type staticPlanner struct{ steps []Step }

func (p staticPlanner) Plan(*domain.Task) ([]Step, error) { return p.steps, nil }

// newStatusTestRunner returns a Runner with only the step sequence and the job status store replaced.
// Passing nil as store gives a configuration with status recording disabled.
func newStatusTestRunner(t *testing.T, jobStatus domain.JobStatusStore, steps ...Step) *Runner {
	t.Helper()

	store := newMemStore()
	deps := Dependencies{
		Ops:     completeOps(),
		Reader:  store,
		Writer:  store,
		Bucket:  "bucket",
		Planner: staticPlanner{steps: steps},
	}
	if jobStatus != nil {
		deps.JobStatus = jobStatus
	}
	r, err := New(deps)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return r
}

// newOutcomeTestRunner returns a Runner that also replaces the Notifier and Timeout, for checking terminal records.
func newOutcomeTestRunner(
	t *testing.T,
	jobStatus domain.JobStatusStore,
	notifier domain.Notifier,
	timeout time.Duration,
	steps ...Step,
) *Runner {
	t.Helper()

	store := newMemStore()
	r, err := New(Dependencies{
		Ops:       completeOps(),
		Reader:    store,
		Writer:    store,
		Bucket:    "bucket",
		Planner:   staticPlanner{steps: steps},
		JobStatus: jobStatus,
		Notifier:  notifier,
		Timeout:   timeout,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return r
}

// deadlineStep waits until the context's deadline and then succeeds.
// It reproduces generation completing almost exactly when PIPELINE_TIMEOUT fires.
type deadlineStep struct{}

func (deadlineStep) Name() string { return "deadline" }

func (deadlineStep) Execute(ctx context.Context, _ *Context) error {
	<-ctx.Done()
	return nil
}

// cancelStep calls cancel just before returning success. It reproduces generation completing at the
// same time as the caller's context is cut (the Cloud Tasks dispatch deadline is exceeded).
type cancelStep struct{ cancel context.CancelFunc }

func (cancelStep) Name() string { return "cancel" }

func (s cancelStep) Execute(context.Context, *Context) error {
	s.cancel()
	return nil
}

type countingStep struct{ calls int }

func (countingStep) Name() string { return "counting" }

func (s *countingStep) Execute(context.Context, *Context) error {
	s.calls++
	return nil
}

type failingStep struct{}

func (failingStep) Name() string                            { return "failing" }
func (failingStep) Execute(context.Context, *Context) error { return errors.New("boom") }
