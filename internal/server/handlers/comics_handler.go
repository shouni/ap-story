package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/shouni/ap-story/internal/domain"

	"github.com/shouni/go-serve-kit/respond"
)

// composeComicRequest は POST /api/comics のリクエストボディです。
// Web フォーム（POST /compose）も同じ入力項目を共有します。
type composeComicRequest struct {
	SourceURL  string `json:"source_url"`
	SourceText string `json:"source_text"`
	ScriptMode string `json:"script_mode"`
	StyleMode  string `json:"style_mode"`
	// TextModel / ImageModel は台本生成・画像生成に使うモデルです。
	// 省略すると既定モデル（GEMINI_MODELS / IMAGE_MODELS の先頭）で埋めます。
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
//
// モデル未指定は既定モデル（一覧の先頭）で埋めます。worker 側の設定へ落とさないのは、
// アスペクト比と同じ理由です（DefaultDesignSheetAspectRatio 参照）。埋めておけば
// どのモデルで作られた作品かが state に必ず残り、章の作り直しや後からの画像生成も
// 同じモデルを引き継げます。空のまま通すと、その作品だけ出自が辿れなくなります。
func (h *Handler) newComposeTask(req composeComicRequest) (domain.Task, error) {
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
		TextModel:       firstIfEmpty(req.TextModel, h.geminiModels),
		ModelOverride:   firstIfEmpty(req.ImageModel, h.imageModels),
		StopAfterScript: req.StopAfterScript,
	}
	return task, nil
}

// validateChoices は、Task に載ったモデル・プロンプトモードの選択が許可リストに
// 収まるかを確かめます。ブラウザは <select> の選択肢に縛られますが、JSON API は
// 任意の文字列を送れるため、投入前にここで弾きます。
//
// コマンドごとに使う項目が違うので、空の項目は「指定なし」として素通しします。
// 投入系すべて（compose / design-sheet / regenerate）がこの1つを通ります。分けると、
// 今度コマンドを足したときにどれか1経路だけ検証漏れになります（実際そうなっていました）。
//
// モードの実体（テンプレートの有無）やモデル名の空は worker 側でも検証されますが、
// そちらは Cloud Tasks を1往復してから落ちるので、送る前にここで返します。
func (h *Handler) validateChoices(task domain.Task) error {
	for _, choice := range []struct {
		kind    string
		value   string
		allowed []string
	}{
		{"台本モード", task.ScriptMode, modeNames(h.scriptModes)},
		{"スタイルモード", task.StyleMode, modeNames(h.styleModes)},
		{"テキストモデル", task.TextModel, h.geminiModels},
		{"画像モデル", task.ModelOverride, h.imageModels},
	} {
		if err := validateAllowed(choice.kind, choice.value, choice.allowed); err != nil {
			return err
		}
	}
	return nil
}

// EnqueueComic は POST /api/comics を処理し、compose_comic ジョブを投入します。
// jobID はサーバー側で新規採番し、レスポンスとして返します。
func (h *Handler) EnqueueComic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respond.ErrorJSON(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req composeComicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}

	task, err := h.newComposeTask(req)
	if err != nil {
		slog.Error("failed to generate job id", "error", err)
		respond.ErrorJSON(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := task.ValidateSubmission(); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.validateChoices(task); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, err.Error())
		return
	}

	h.enqueueAndRespond(w, r, task)
}

// enqueueAndRespond は Task をキューに投入し、成功したら 202 Accepted と jobID を返します。
func (h *Handler) enqueueAndRespond(w http.ResponseWriter, r *http.Request, task domain.Task) {
	// 状態を先に書く。worker は配信されたタスクより先に状態を読むので、順序を逆にすると
	// 1つ前の succeeded を読んで投入が黙って捨てられます（recordQueuedStatus 参照）。
	h.recordQueuedStatus(r, task)
	if err := h.taskQueue.Enqueue(r.Context(), task); err != nil {
		slog.Error("failed to enqueue task", "job_id", task.JobID, "command", task.Command, "error", err)
		respond.ErrorJSON(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	respond.JSON(w, r, http.StatusAccepted, enqueueResponse{Status: "queued", JobID: task.JobID})
}
