package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/shouni/gcp-kit/negotiate"

	"github.com/shouni/ap-story/internal/domain"
)

// このファイルは、人と機械が同じものを見る経路をまとめています。
//
// 表現だけが違うのに実装を 2 つ持つと、片方だけ直したときに画面と機械可読な結果が
// 食い違います。取得と検証は 1 度だけ行い、最後に Accept を見て HTML か JSON かを
// 決めます。片方の読者にしか無いもの（入力フォーム、再生成の指示、画像の転送など）は
// 別のリソースなので、ここには置きません。

// Comics は履歴一覧を返します。?page= を受けます。
func (h *Handler) Comics(w http.ResponseWriter, r *http.Request) {
	page := parseHistoryPage(r)
	historyPage, err := h.repository.ListHistoryPage(r.Context(), page, defaultHistoryPageSize)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to list comic history", "error", err)
		negotiate.Error(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	if negotiate.WantsJSON(w, r) {
		negotiate.JSON(w, r, http.StatusOK, historyPage)
		return
	}
	h.render(w, r, http.StatusOK, "history.html", "作品履歴", historyPage)
}

// Comic は 1 作品の状態を返します。
//
// 未生成のジョブに対して、画面は「処理中または未存在」の案内を出し、機械には
// 404 を返します。案内の HTML を機械に渡しても解釈できません。
func (h *Handler) Comic(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if err := domain.ValidateJobID(jobID); err != nil {
		negotiate.Error(w, r, http.StatusBadRequest, "invalid job id")
		return
	}

	state, err := h.repository.GetState(r.Context(), jobID)
	if err != nil {
		if negotiate.WantsJSON(w, r) {
			writeStateError(w, r, jobID, err)
			return
		}
		if !errors.Is(err, domain.ErrStateNotFound) {
			slog.ErrorContext(r.Context(), "failed to load comic state", "job_id", jobID, "error", err)
			http.Error(w, "作品の読み込みに失敗しました。時間をおいて開き直してください。", http.StatusBadGateway)
			return
		}
		h.render(w, r, http.StatusNotFound, "history_pending.html", "処理中または未存在", historyPendingData{JobID: jobID})
		return
	}

	if negotiate.WantsJSON(w, r) {
		negotiate.JSON(w, r, http.StatusOK, state)
		return
	}
	h.render(w, r, http.StatusOK, "history_detail.html", state.Title, h.buildDetailData(jobID, state))
}

// Characters は、characters.json の全キャラクターを返します。
//
// 画面はサムネイルの URL を Web の経路へ書き換えますが、機械には定義そのままの
// 参照 URL を渡します。書き換えた URL は画面の画像エンドポイントに紐づくため、
// 機械が受け取っても素材の在り処にはなりません。
func (h *Handler) Characters(w http.ResponseWriter, r *http.Request) {
	if negotiate.WantsJSON(w, r) {
		items := make([]characterSummaryResponse, 0, h.characters.Len())
		for _, c := range h.characters.All() {
			items = append(items, characterSummaryResponse{
				ID:           c.ID,
				Name:         c.Name,
				ReferenceURL: c.ReferenceURL,
			})
		}
		negotiate.JSON(w, r, http.StatusOK, items)
		return
	}

	items := make([]characterListItem, 0, h.characters.Len())
	for _, c := range h.characters.All() {
		items = append(items, characterListItem{
			ID:                c.ID,
			Name:              c.Name,
			ThumbnailImageURL: h.characterReferenceWebPath(c.ReferenceURL),
		})
	}
	h.render(w, r, http.StatusOK, "characters.html", "Characters", items)
}

// Character は、マスター参照画像とデザインシート履歴を返します。
//
// 画面は履歴を characterDetailHistoryLimit 件で切りますが、機械には全件返します。
// 画面の上限は一覧の見やすさのためで、呼び出し側が探したい 1 枚を落とす理由には
// なりません。
func (h *Handler) Character(w http.ResponseWriter, r *http.Request) {
	characterID := chi.URLParam(r, "characterID")
	char := h.characters.GetCharacter(characterID)
	if char == nil {
		negotiate.Error(w, r, http.StatusNotFound, "character not found")
		return
	}

	if negotiate.WantsJSON(w, r) {
		history, err := h.repository.ListCharacterDesignHistory(r.Context(), characterID)
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to list character design history", "character_id", characterID, "error", err)
			negotiate.Error(w, r, http.StatusInternalServerError, "internal server error")
			return
		}
		negotiate.JSON(w, r, http.StatusOK, characterDetailResponse{
			ID:            char.ID,
			Name:          char.Name,
			ReferenceURL:  char.ReferenceURL,
			ReferenceURLs: char.ReferenceURLs,
			History:       history,
		})
		return
	}

	data := characterDetailData{
		CharacterID: characterID,
		Name:        char.Name,
		References:  h.buildCharacterReferences(char.ReferenceURL, char.ReferenceURLs),
	}

	history, err := h.buildCharacterHistoryImages(r, characterID)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	data.HistoryTotal = len(history)
	if len(history) > characterDetailHistoryLimit {
		history = history[:characterDetailHistoryLimit]
	}
	data.History = history

	h.render(w, r, http.StatusOK, "character_detail.html", char.Name, data)
}
