package handlers

import (
	"log/slog"
	"net/http"
	"strings"
)

// composeFormData は compose.html テンプレートに渡すデータです。
// エラー時は入力値を保持したままフォームを再表示します。
type composeFormData struct {
	SourceURL  string
	SourceText string
	ScriptMode string
	StyleMode  string
	// StopAfterScript はチェックボックスの状態です（エラーで再表示するときに保つため）。
	StopAfterScript bool
	ErrorMessage    string
}

// ComposeForm は GET /compose を処理し、漫画生成フォームを表示します。
func (h *Handler) ComposeForm(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "compose.html", "漫画を生成", composeFormData{})
}

// EnqueueComicForm は POST /compose を処理し、フォーム入力から compose_comic ジョブを
// 投入します。タスク構築・検証は JSON API（EnqueueComic）と共有し、レスポンスだけが
// HTML（受付画面 or エラー付きフォーム再表示）になります。
func (h *Handler) EnqueueComicForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	req := composeComicRequest{
		SourceURL:       strings.TrimSpace(r.PostFormValue("source_url")),
		SourceText:      strings.TrimSpace(r.PostFormValue("source_text")),
		ScriptMode:      strings.TrimSpace(r.PostFormValue("script_mode")),
		StyleMode:       strings.TrimSpace(r.PostFormValue("style_mode")),
		StopAfterScript: r.PostFormValue("stop_after_script") != "",
	}
	formData := composeFormData{
		SourceURL:       req.SourceURL,
		SourceText:      req.SourceText,
		ScriptMode:      req.ScriptMode,
		StyleMode:       req.StyleMode,
		StopAfterScript: req.StopAfterScript,
	}

	task, err := newComposeTask(req)
	if err != nil {
		slog.Error("failed to generate job id", "error", err)
		formData.ErrorMessage = "ジョブIDの採番に失敗しました。時間をおいて再度お試しください。"
		h.render(w, r, http.StatusInternalServerError, "compose.html", "漫画を生成", formData)
		return
	}

	if err := task.ValidateSubmission(); err != nil {
		formData.ErrorMessage = err.Error()
		h.render(w, r, http.StatusBadRequest, "compose.html", "漫画を生成", formData)
		return
	}

	if err := h.taskQueue.Enqueue(r.Context(), task); err != nil {
		slog.Error("failed to enqueue task", "job_id", task.JobID, "command", task.Command, "error", err)
		formData.ErrorMessage = "ジョブの投入に失敗しました。時間をおいて再度お試しください。"
		h.render(w, r, http.StatusInternalServerError, "compose.html", "漫画を生成", formData)
		return
	}
	h.recordQueuedStatus(r, task)

	h.render(w, r, http.StatusAccepted, "accepted.html", "受付完了", newComposeAcceptedData(task.JobID))
}
