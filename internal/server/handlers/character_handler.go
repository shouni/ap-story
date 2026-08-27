package handlers

import (
	"log/slog"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
)

// referenceAspectOrder は character_detail.html でのアスペクト比表示順です。
// 3:4 を先頭に置くのは、パネルとページがその比率で生成されるためです（go-comic-kit の
// layout.PanelAspectRatio / PageAspectRatio）。参照画像は生成対象と同じ比率のものが
// 優先して使われるので、漫画生成に効くのは 3:4 のシートです。
var referenceAspectOrder = []string{"3:4", "16:9", "9:16", "1:1"}

// characterListItem は characters.html テンプレートに渡す1キャラクター分のデータです。
type characterListItem struct {
	ID                string
	Name              string
	ThumbnailImageURL string
}

// characterReferenceImage は character_detail.html に渡す1枚のマスター参照画像です。
type characterReferenceImage struct {
	Label    string
	ImageURL string
}

// characterHistoryImage は character_detail.html に渡す1件の生成履歴です。
type characterHistoryImage struct {
	JobID    string
	ImageURL string
}

// characterDetailHistoryLimit は詳細画面に表示する生成履歴の最大件数です。
// これを超える分は /characters/{characterID}/history で確認します。
const characterDetailHistoryLimit = 12

// characterDetailData は character_detail.html テンプレートに渡すデータです。
type characterDetailData struct {
	CharacterID string
	Name        string
	// References はマスター参照画像（既定 + アスペクト比別）です。生成の入力として
	// 使われる「正解」の見た目で、AI生成のデザインシート履歴とは別管理です。
	References []characterReferenceImage
	// History は AI 生成のデザインシート履歴（新しい順、最大 characterDetailHistoryLimit 件）です。
	// 複数キャラクター合成生成のシートはここには表示されません
	// （character/{単体キャラクターID}/ 配下にしか記録されないため）。
	History []characterHistoryImage
	// HistoryTotal は上限適用前の全履歴件数です。History より大きい場合、
	// テンプレートは全件表示ページへのリンクを出します。
	HistoryTotal int
}

// characterHistoryPageData は character_history.html テンプレートに渡すデータです。
type characterHistoryPageData struct {
	CharacterID string
	Name        string
	History     []characterHistoryImage
}

// ServeCharacterHistory は GET /characters/{characterID}/history を処理し、
// AI生成のデザインシート履歴を全件（新しい順）表示します。
func (h *Handler) ServeCharacterHistory(w http.ResponseWriter, r *http.Request) {
	characterID := chi.URLParam(r, "characterID")
	char := h.characters.GetCharacter(characterID)
	if char == nil {
		http.Error(w, "character not found", http.StatusNotFound)
		return
	}

	history, err := h.buildCharacterHistoryImages(r, characterID)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, http.StatusOK, "character_history.html", char.Name+" の生成履歴", characterHistoryPageData{
		CharacterID: characterID,
		Name:        char.Name,
		History:     history,
	})
}

// DeleteCharacterDesign は DELETE /api/characters/{characterID}/images/{jobID} を処理し、
// 指定キャラクターの生成履歴1件（画像と、単体生成ジョブなら state も）を削除します。
func (h *Handler) DeleteCharacterDesign(w http.ResponseWriter, r *http.Request) {
	characterID := chi.URLParam(r, "characterID")
	if h.characters.GetCharacter(characterID) == nil {
		writeErrorJSON(w, http.StatusNotFound, "character not found")
		return
	}
	jobID := chi.URLParam(r, "jobID")

	if err := h.repository.DeleteCharacterDesign(r.Context(), characterID, jobID); err != nil {
		slog.ErrorContext(r.Context(), "failed to delete character design",
			"character_id", characterID, "job_id", jobID, "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, "failed to delete character design")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// buildCharacterReferences はマスター参照画像（既定 + アスペクト比別、表示順ソート済み）を
// テンプレート用データへ変換します。
func (h *Handler) buildCharacterReferences(referenceURL string, referenceURLs map[string]string) []characterReferenceImage {
	var refs []characterReferenceImage
	if url := h.characterReferenceWebPath(referenceURL); url != "" {
		refs = append(refs, characterReferenceImage{Label: "既定", ImageURL: url})
	}
	aspectRatios := make([]string, 0, len(referenceURLs))
	for ratio := range referenceURLs {
		aspectRatios = append(aspectRatios, ratio)
	}
	sort.Slice(aspectRatios, func(i, j int) bool {
		return referenceAspectIndex(aspectRatios[i]) < referenceAspectIndex(aspectRatios[j])
	})
	for _, ratio := range aspectRatios {
		if url := h.characterReferenceWebPath(referenceURLs[ratio]); url != "" {
			refs = append(refs, characterReferenceImage{Label: ratio, ImageURL: url})
		}
	}
	return refs
}

// buildCharacterHistoryImages は生成履歴（新しい順、全件）をテンプレート用データへ変換します。
// 取得失敗時はログを残してエラーを返します（HTTP レスポンスは呼び出し側の責務）。
func (h *Handler) buildCharacterHistoryImages(r *http.Request, characterID string) ([]characterHistoryImage, error) {
	items, err := h.repository.ListCharacterDesignHistory(r.Context(), characterID)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to list character design history",
			"character_id", characterID, "error", err)
		return nil, err
	}
	history := make([]characterHistoryImage, 0, len(items))
	for _, item := range items {
		history = append(history, characterHistoryImage{
			JobID:    item.JobID,
			ImageURL: h.characterImageWebPath(item.ImageURL),
		})
	}
	return history, nil
}

// referenceAspectIndex は referenceAspectOrder 内の位置を返します（未知の比率は末尾）。
func referenceAspectIndex(ratio string) int {
	for i, r := range referenceAspectOrder {
		if r == ratio {
			return i
		}
	}
	return len(referenceAspectOrder)
}
