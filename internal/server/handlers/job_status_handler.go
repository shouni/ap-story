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

// recordQueuedStatus はジョブ状態に queued を記録します。
//
// **必ず enqueue の前に呼びます。** worker は受け取ったタスクの前に状態を読み、
// succeeded なら「再配信された完了済みジョブ」と見なして飛ばします。Cloud Tasks の配信は
// 数十ミリ秒で届くので、投入してから書くと、同じ作品への次の操作が1つ前の succeeded を
// 読んで黙って捨てられます（ジョブ状態は job_id 単位で、コマンド別ではありません）。
//
// 記録に失敗しても受付は成功とします。ここで失敗するのは GCS 側の問題で、
// タスクを止める理由にはなりません（状態が追えなくなるだけです）。
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
// ブラウザからのポーリングと M2M クライアント（MCP サーバー）の完了検知の両方が利用します。
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
