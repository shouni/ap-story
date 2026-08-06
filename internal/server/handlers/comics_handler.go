package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/shouni/ap-story/internal/domain"
)

// composeComicRequest は POST /api/comics のリクエストボディです。
// Web フォーム（POST /compose）も同じ入力項目を共有します。
type composeComicRequest struct {
	SourceURL  string `json:"source_url"`
	SourceText string `json:"source_text"`
	ScriptMode string `json:"script_mode"`
	StyleMode  string `json:"style_mode"`
	// StopAfterScript を指定すると台本までで止まります。内容を確認してから
	// 画像生成へ進むための指定で、続きは render_comic で行います。
	StopAfterScript bool `json:"stop_after_script"`
}

// enqueueResponse はジョブ投入系エンドポイントの共通レスポンスです。
type enqueueResponse struct {
	Status string `json:"status"`
	JobID  string `json:"job_id"`
}

// newComposeTask は compose_comic の入力からジョブ ID を採番して Task を構築します。
// JSON API と Web フォームで共有するコアロジックで、レスポンス形式だけが呼び出し側で異なります。
func newComposeTask(req composeComicRequest) (domain.Task, error) {
	jobID, err := domain.NewJobID()
	if err != nil {
		return domain.Task{}, err
	}

	task := domain.Task{
		Command:         domain.TaskCommandComposeComic,
		JobID:           jobID,
		CreatedAt:       time.Now().UTC(),
		SourceURL:       req.SourceURL,
		SourceText:      req.SourceText,
		ScriptMode:      req.ScriptMode,
		StyleMode:       req.StyleMode,
		StopAfterScript: req.StopAfterScript,
	}
	return task, nil
}

// EnqueueComic は POST /api/comics を処理し、compose_comic ジョブを投入します。
// jobID はサーバー側で新規採番し、レスポンスとして返します。
func (h *Handler) EnqueueComic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorJSON(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req composeComicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	task, err := newComposeTask(req)
	if err != nil {
		slog.Error("failed to generate job id", "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := task.ValidateSubmission(); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	h.enqueueAndRespond(w, r, task)
}

// enqueueAndRespond は Task をキューに投入し、成功したら 202 Accepted と jobID を返します。
func (h *Handler) enqueueAndRespond(w http.ResponseWriter, r *http.Request, task domain.Task) {
	if err := h.taskQueue.Enqueue(r.Context(), task); err != nil {
		slog.Error("failed to enqueue task", "job_id", task.JobID, "command", task.Command, "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.recordQueuedStatus(r, task)

	writeJSON(w, http.StatusAccepted, enqueueResponse{Status: "queued", JobID: task.JobID})
}
