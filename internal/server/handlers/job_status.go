package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/shouni/ap-story/internal/domain"
)

// recordQueuedStatus records queued as the job status.
//
// **Always call it before the enqueue.** The worker reads the status before the task it received and,
// if it is succeeded, skips it as "a completed job delivered again". Cloud Tasks delivers within
// tens of milliseconds, so writing after enqueueing would let the next operation on the same work read the
// previous succeeded and be silently dropped (the job status is per job_id, not per command).
//
// A failure to record still counts as accepted. A failure here is a GCS problem and
// no reason to stop the task (the status just cannot be followed).
func (h *Handler) recordQueuedStatus(r *http.Request, task domain.Task) {
	if h.jobStatus == nil {
		return
	}

	status := domain.NewQueuedJobStatus(task, time.Now().UTC())
	if err := h.jobStatus.Save(r.Context(), task.JobID, status); err != nil {
		slog.WarnContext(r.Context(), "failed to record queued job status",
			"job_id", task.JobID, "error", err)
	}
}
