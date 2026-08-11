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
	TextModel  string
	ImageModel string
	// ScriptModes / StyleModes は台本モード・スタイルモードの選択肢
	// （assets/prompts 配下のテンプレート）です。
	ScriptModes []selectOption
	StyleModes  []selectOption
	// TextModels / ImageModels はモデルの選択肢です（先頭が既定）。
	// 空なら選択欄そのものを出しません。
	TextModels   []selectOption
	ImageModels  []selectOption
	ErrorMessage string
}

// buildComposeFormData は入力値を反映したフォーム表示用データを構築します。
// 初回表示（ゼロ値の req）とエラー再表示で同じ組み立てを通します。
func (h *Handler) buildComposeFormData(req composeComicRequest) composeFormData {
	return composeFormData{
		SourceURL:   req.SourceURL,
		SourceText:  req.SourceText,
		ScriptMode:  req.ScriptMode,
		StyleMode:   req.StyleMode,
		TextModel:   req.TextModel,
		ImageModel:  req.ImageModel,
		ScriptModes: modeOptions(h.scriptModes, req.ScriptMode),
		StyleModes:  modeOptions(h.styleModes, req.StyleMode),
		TextModels:  modelOptions(h.geminiModels, req.TextModel),
		ImageModels: modelOptions(h.imageModels, req.ImageModel),
	}
}

// ComposeForm は GET /compose を処理し、漫画生成フォームを表示します。
func (h *Handler) ComposeForm(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "compose.html", "漫画を生成", h.buildComposeFormData(composeComicRequest{}))
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
		SourceURL:  strings.TrimSpace(r.PostFormValue("source_url")),
		SourceText: strings.TrimSpace(r.PostFormValue("source_text")),
		ScriptMode: strings.TrimSpace(r.PostFormValue("script_mode")),
		StyleMode:  strings.TrimSpace(r.PostFormValue("style_mode")),
		TextModel:  strings.TrimSpace(r.PostFormValue("text_model")),
		ImageModel: strings.TrimSpace(r.PostFormValue("image_model")),
		// このフォームは台本までしか作りません。押した時点では章立てが未実行で、
		// 何コマになるか誰も知らないためです。金額の分からない支払いを承認させない。
		// 画像はコマ数が見えている作品詳細から始めます（JSON API は最後まで走れます）。
		StopAfterScript: true,
	}
	formData := h.buildComposeFormData(req)

	task, err := h.newComposeTask(req)
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

	if err := h.validateChoices(task); err != nil {
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
