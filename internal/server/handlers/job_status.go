package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/shouni/ap-story/internal/domain"
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
