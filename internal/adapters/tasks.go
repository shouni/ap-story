package adapters

import (
	"context"
	"fmt"

	"github.com/shouni/gcp-kit/tasks"

	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
)

// TaskEnqueuer wraps gcp-kit's Cloud Tasks Enqueuer and implements domain.TaskQueue, enqueueing
// tasks with a deterministic name derived from Task.TaskName().
// A duplicate enqueue of the same name (from Cloud Tasks' at-least-once delivery or a caller's retry)
// is treated as success via ALREADY_EXISTS, so only one task is actually created.
type TaskEnqueuer struct {
	enqueuer *tasks.Enqueuer[domain.Task]
}

var _ domain.TaskQueue = (*TaskEnqueuer)(nil)

// NewTaskEnqueuer initialises the Cloud Tasks enqueuer.
// The instance holds a gRPC connection internally, so reuse it as a singleton
// and Close it when the application exits.
func NewTaskEnqueuer(ctx context.Context, cfg *config.Config) (*TaskEnqueuer, error) {

	taskCfg := tasks.Config{
		ProjectID:  cfg.GCP.ProjectID,
		LocationID: cfg.GCP.LocationID,
		QueueID:    cfg.Tasks.QueueID,
		WorkerURL:  cfg.Tasks.WorkerURL,
		WorkerPath: domain.WorkerTaskPath,
		// The caller SA put on the task. Cloud Tasks mints and attaches the token; this
		// process signs nothing. It is not the allowlist the receiving side accepts
		// (Tasks.AllowedServiceAccounts); do not confuse the two.
		ServiceAccountEmail: cfg.TaskCallerServiceAccount(),
		Audience:            cfg.Tasks.TaskAudienceURL,
		// The effective cap on the worker's run time. Unset, Cloud Tasks' default of 10 minutes applies,
		// and the request is cut there however long the worker's Cloud Run timeout is.
		// compose_comic takes at least 14 minutes with the defaults, so the default is not enough.
		// PIPELINE_TIMEOUT is set shorter than this (25m in production) so that the app can give up
		// on its own first. For how the three relate, see "1. Configuration" in the README.
		DispatchDeadline: cfg.Tasks.DispatchDeadline,
	}
	enqueuer, err := tasks.NewEnqueuer[domain.Task](ctx, taskCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create cloud tasks enqueuer: %w", err)
	}
	return &TaskEnqueuer{enqueuer: enqueuer}, nil
}

// Enqueue enqueues the task onto Cloud Tasks under a deterministic name derived from task.TaskName().
func (e *TaskEnqueuer) Enqueue(ctx context.Context, task domain.Task) error {
	return e.enqueuer.EnqueueWithName(ctx, task.TaskName(), task)
}

// Close releases the resources the enqueuer holds (the gRPC connection).
func (e *TaskEnqueuer) Close() error {
	return e.enqueuer.Close()
}
