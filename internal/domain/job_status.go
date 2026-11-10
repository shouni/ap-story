package domain

import (
	"path"
	"time"

	"github.com/shouni/go-job-kit/jobstatus"
)

// JobState is a state in a job's lifecycle.
// It is go-job-kit's jobstatus.State.
type JobState = jobstatus.State

const (
	// JobStateQueued means enqueued onto Cloud Tasks, with no worker processing it yet.
	JobStateQueued = jobstatus.StateQueued
	// JobStateRunning means a worker is processing it.
	JobStateRunning = jobstatus.StateRunning
	// JobStateSucceeded means everything up to saving the artifacts has completed.
	JobStateSucceeded = jobstatus.StateSucceeded
	// JobStateFailed means processing failed. It can be a target of Cloud Tasks retries.
	JobStateFailed = jobstatus.StateFailed
)

// ErrJobStatusNotFound means the job status has not been recorded yet.
// "No status" is a normal state, not an anomaly (an enqueue not yet recorded, or a job made
// before this feature), so it is a separate error the caller can tell apart from a storage failure.
//
// Like JobState it points straight at go-job-kit's value. Aliasing only the state constants in
// domain while keeping the errors in repository split the references for one jobstatus surface:
// "states through domain, errors through the concrete package".
var ErrJobStatusNotFound = jobstatus.ErrNotFound

// jobStatusFile is the object name that records job progress.
const jobStatusFile = "status.json"

// JobStatus is a job's progress.
//
// Whether a generation succeeded used to survive only in Slack notices, and failed jobs vanished from the UI entirely.
// With this record, both the UI and M2M clients can follow a job's state after enqueueing.
// It is also the basis of the re-run guard against Cloud Tasks' at-least-once delivery.
//
// The common fields (JobID, State, Attempts and so on) and IsTerminal belong to jobstatus.Status.
// It is embedded, so the JSON stays flat and existing status.json files read and write unchanged.
type JobStatus struct {
	jobstatus.Status
	// OutputDir is the GCS URI that holds the artifacts.
	OutputDir string `json:"output_dir,omitempty"`
}

// NewQueuedJobStatus builds the job status right after enqueueing.
func NewQueuedJobStatus(task Task, now time.Time) JobStatus {
	return JobStatus{
		JobID:     task.JobID,
		Command:   string(task.Command),
		State:     JobStateQueued,
		QueuedAt:  now,
		UpdatedAt: now,
	}
}

// JobStatusPath returns the relative object path of the job progress JSON.
// It sits under comics/{jobID}/ with the artifacts, so deleting the history (a prefix delete)
// cleans it up automatically. The history list picks up only comic_state.json, so it does not appear there.
func JobStatusPath(jobID string) (string, error) {
	prefix, err := JobObjectPrefix(jobID)
	if err != nil {
		return "", err
	}
	return path.Join(prefix, jobStatusFile), nil
}
