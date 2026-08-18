package handlers

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/shouni/ap-story/internal/domain"
)

// characterSummaryResponse は GET /api/characters の1キャラクター分のレスポンスです。
// 画像 URL は state 同様 gs:// URI のまま返します（署名 URL 変換は
// /api/characters/reference/* リダイレクトエンドポイントの責務です）。
type characterSummaryResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ReferenceURL string `json:"reference_url,omitempty"`
}

// ListCharacters は GET /api/characters を処理し、characters.json の全キャラクターを返します。
func (h *Handler) ListCharacters(w http.ResponseWriter, _ *http.Request) {
	items := make([]characterSummaryResponse, 0, h.characters.Len())
	for _, c := range h.characters.All() {
		items = append(items, characterSummaryResponse{
			ID:           c.ID,
			Name:         c.Name,
			ReferenceURL: c.ReferenceURL,
		})
	}
	writeJSON(w, http.StatusOK, items)
}

// characterDetailResponse は GET /api/characters/{characterID} のレスポンスです。
type characterDetailResponse struct {
	ID            string                              `json:"id"`
	Name          string                              `json:"name"`
	ReferenceURL  string                              `json:"reference_url,omitempty"`
	ReferenceURLs map[string]string                   `json:"reference_urls,omitempty"`
	History       []domain.CharacterDesignHistoryItem `json:"history"`
}

// GetCharacterDetail は GET /api/characters/{characterID} を処理し、マスター参照画像と
// 生成履歴（新しい順、全件）を返します。
func (h *Handler) GetCharacterDetail(w http.ResponseWriter, r *http.Request) {
	characterID := chi.URLParam(r, "characterID")
	char := h.characters.GetCharacter(characterID)
	if char == nil {
		writeErrorJSON(w, http.StatusNotFound, "character not found")
		return
	}

	history, err := h.repository.ListCharacterDesignHistory(r.Context(), characterID)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to list character design history", "character_id", characterID, "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, characterDetailResponse{
		ID:            char.ID,
		Name:          char.Name,
		ReferenceURL:  char.ReferenceURL,
		ReferenceURLs: char.ReferenceURLs,
		History:       history,
	})
}
