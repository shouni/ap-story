package pipeline

import (
	"context"

	"github.com/shouni/go-job-kit/jobstatus"

	"github.com/shouni/ap-story/internal/domain"
)

// statusRecorder records job status.
//
// The re-run guard, carrying over from the previous record and swallowing record failures all live
// in jobstatus.Recorder; all this provides is assembling the ap-story-specific state.
// With no destination configured the pipeline still works; only the recording is skipped.
type statusRecorder struct {
	recorder *jobstatus.Recorder[domain.JobStatus]
}

// newStatusRecorder builds a statusRecorder backed by a domain.JobStatusStore.
// When store is nil, nothing is recorded.
//
// domain.JobStatusStore has the same signature as jobstatus.StatusStore, so it can be passed as is.
func newStatusRecorder(store domain.JobStatusStore) statusRecorder {
	if store == nil {
		return statusRecorder{recorder: jobstatus.NewRecorder[domain.JobStatus](nil)}
	}
	return statusRecorder{recorder: jobstatus.NewRecorder[domain.JobStatus](store)}
}

// begin returns true when the job has already completed; otherwise it records the start of processing
// and returns false. The check and the record use a single read of the previous record, so no other
// delivery can slip in between. The attempt count advances by one here.
//
// When outputDir is empty (the destination could not be resolved), it writes no running, as before,
// and only checks. This keeps a wrong destination out of the record.
//
// When the state cannot be read it returns an error and records nothing. The caller should return the
// error as is and leave it to Cloud Tasks redelivery. Falling to "not completed" here would rebuild a
// completed job and incur the very cost this guard exists to prevent; falling to "completed" would ACK
// an unfinished job along with its task, and it would never run again.
func (s statusRecorder) begin(ctx context.Context, task *domain.Task, outputDir string) (bool, error) {
	if task == nil {
		return false, nil
	}
	if outputDir == "" {
		return s.recorder.AlreadySucceeded(ctx, task.JobID)
	}
	return s.recorder.Begin(ctx, task.JobID, s.newStatus(task, domain.JobStateRunning),
		s.carryOutputDir(func(next, _ *domain.JobStatus) {
			next.OutputDir = outputDir
			next.Attempts++
		}))
}

// markSucceeded records success.
func (s statusRecorder) markSucceeded(ctx context.Context, task *domain.Task, title string) {
	s.record(ctx, task, domain.JobStateSucceeded, func(next, _ *domain.JobStatus) {
		if title != "" {
			next.Title = title
		}
	})
}

// markFailed records failure and its reason.
func (s statusRecorder) markFailed(ctx context.Context, task *domain.Task, cause error) {
	if cause == nil {
		return
	}
	s.record(ctx, task, domain.JobStateFailed, func(next, _ *domain.JobStatus) {
		next.Error = cause.Error()
	})
}

// record assembles and saves the state for this record.
//
// Attempts, QueuedAt and Title are carried over from the previous record by the Recorder. OutputDir is
// an ap-story-specific field that jobstatus.Status lacks, so it is carried over here.
// apply is called after the carry-over, so it can overwrite with this run's values, as markRunning does.
func (s statusRecorder) record(
	ctx context.Context,
	task *domain.Task,
	state domain.JobState,
	apply func(next, prev *domain.JobStatus),
) {
	if task == nil {
		return
	}
	s.recorder.Record(ctx, task.JobID, s.newStatus(task, state), s.carryOutputDir(apply))
}

// newStatus assembles the state for this record.
func (s statusRecorder) newStatus(task *domain.Task, state domain.JobState) domain.JobStatus {
	return domain.JobStatus{
		JobID:   task.JobID,
		Command: string(task.Command),
		State:   state,
	}
}

// carryOutputDir returns a function that carries over the previous OutputDir and then calls apply.
// OutputDir is an ap-story-specific field that jobstatus.Status lacks, so it is carried over here.
func (s statusRecorder) carryOutputDir(apply func(next, prev *domain.JobStatus)) func(next, prev *domain.JobStatus) {
	return func(next, prev *domain.JobStatus) {
		if prev != nil {
			next.OutputDir = prev.OutputDir
		}
		if apply != nil {
			apply(next, prev)
		}
	}
}
