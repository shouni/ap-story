package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/shouni/ap-story/internal/domain"
	"github.com/shouni/ap-story/internal/repository"
)

// recordQueuedStatus は投入直後のジョブ状態を記録します。
// これがあることで、ワーカーが動き出す前でも投入済みジョブを追跡できます。
// 記録に失敗してもタスク自体は既にキューへ入っているため、警告ログに留めて受付は成功とします。
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

// JobStatus は、ジョブの進行状況（queued/running/succeeded/failed）を JSON で返します。
// ブラウザからのポーリングと M2M クライアント（ap-mcp）の完了検知の両方が利用します。
func (h *Handler) JobStatus(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimSpace(chi.URLParam(r, "jobID"))
	if err := domain.ValidateJobID(jobID); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "Invalid JobID format")
		return
	}
	if h.jobStatus == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "job status tracking is not configured")
		return
	}

	status, err := h.jobStatus.Get(r.Context(), jobID)
	if err != nil {
		// 状態が無いのは異常ではなく「この機能より前に作られたジョブ」でも起こるため、
		// 404 で明確に区別できるようにします。
		if errors.Is(err, repository.ErrJobStatusNotFound) {
			writeErrorJSON(w, http.StatusNotFound, "job status not found")
			return
		}
		slog.ErrorContext(r.Context(), "failed to load job status", "job_id", jobID, "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, "failed to load job status")
		return
	}

	writeJSON(w, http.StatusOK, status)
}
