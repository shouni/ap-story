package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/shouni/go-serve-kit/respond"
)

// designSheetAPIRequest は POST /api/design-sheets のリクエストボディです。
// Web フォーム（POST /design-sheets）も同じ入力項目をコアロジック（newDesignSheetTask）で共有します。
type designSheetAPIRequest struct {
	CharacterIDs         []string `json:"character_ids"`
	AspectRatio          string   `json:"aspect_ratio,omitempty"`
	Layout               string   `json:"layout,omitempty"`
	StyleMode            string   `json:"style_mode,omitempty"`
	ModelOverride        string   `json:"model_override,omitempty"`
	ReferenceURLOverride string   `json:"reference_url_override,omitempty"`
	VisualCuesOverride   []string `json:"visual_cues_override,omitempty"`
	// Seed は省略可。未指定かつキャラクターを1人だけ指定した場合、characters.json の
	// そのキャラクターの登録 seed へ自動フォールバックします（EnqueueDesignSheet 参照）。
	Seed *int64 `json:"seed,omitempty"`
}

// EnqueueDesignSheet は POST /api/design-sheets を処理し、generate_design_sheet ジョブを
// 投入します。job_id はサーバー側で新規採番し、レスポンスとして返します。
func (h *Handler) EnqueueDesignSheet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respond.ErrorJSON(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req designSheetAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}

	params := designSheetTaskParams(req)
	params.Seed = h.resolveDesignSheetSeed(params.CharacterIDs, params.Seed)

	task, err := h.newDesignSheetTask(params)
	if err != nil {
		slog.Error("failed to generate job id", "error", err)
		respond.ErrorJSON(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := task.ValidateSubmission(); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// モデル名と画風は許可リストで縛る。ブラウザは <select> に縛られるが、
	// この JSON 経路は任意の文字列を送れるため、ここが唯一の関門になる。
	if err := h.validateChoices(task); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, err.Error())
		return
	}

	h.enqueueAndRespond(w, r, task)
}
