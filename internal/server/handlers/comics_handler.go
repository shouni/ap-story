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
	// TextModel / ImageModel は台本生成・画像生成に使うモデルです。
	// 空文字なら worker 側の設定（GEMINI_MODELS / IMAGE_MODELS の先頭）を使います。
	TextModel  string `json:"text_model"`
	ImageModel string `json:"image_model"`
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
		TextModel:       req.TextModel,
		ModelOverride:   req.ImageModel,
		StopAfterScript: req.StopAfterScript,
	}
	return task, nil
}

// validateComposeChoices は、モデルとプロンプトモードの選択が許可リストに収まるかを
// 確かめます。ブラウザは <select> の選択肢に縛られますが、JSON API は任意の文字列を
// 送れるため、投入前にここで弾きます。
//
// モードの実体（テンプレートの有無）は worker 側でも検証されますが、そちらは
// 画像生成のあとの章台本まで進んでから落ちることがあるので、投入時点で返します。
func (h *Handler) validateComposeChoices(task domain.Task) error {
	if err := validateAllowed("台本モード", task.ScriptMode, modeNames(h.scriptModes)); err != nil {
		return err
	}
	if err := validateAllowed("スタイルモード", task.StyleMode, modeNames(h.styleModes)); err != nil {
		return err
	}
	if err := validateAllowed("テキストモデル", task.TextModel, h.geminiModels); err != nil {
		return err
	}
	return h.validateModelOverride(task.ModelOverride)
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

	if err := h.validateComposeChoices(task); err != nil {
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
