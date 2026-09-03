package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/shouni/go-serve-kit/respond"

	"github.com/shouni/ap-story/internal/domain"
)

// このファイルは、人と機械が同じものを見る経路をまとめています。
//
// 表現だけが違うのに実装を 2 つ持つと、片方だけ直したときに画面と機械可読な結果が
// 食い違います。取得と検証は 1 度だけ行い、最後に Accept を見て HTML か JSON かを
// 決めます。片方の読者にしか無いもの（入力フォーム、再生成の指示、画像の転送など）は
// 別のリソースなので、ここには置きません。

// JobList は作品（compose_comic のジョブ）の一覧を返します（GET /jobs?page=）。
func (h *Handler) JobList(w http.ResponseWriter, r *http.Request) {
	page := parseHistoryPage(r)
	historyPage, err := h.repository.ListHistoryPage(r.Context(), page, defaultHistoryPageSize)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to list comic history", "error", err)
		respond.Error(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	if respond.WantsJSON(w, r) {
		respond.JSON(w, r, http.StatusOK, historyPage)
		return
	}
	h.render(w, r, http.StatusOK, "history.html", "作品履歴", historyPage)
}

// jobDocument は GET /jobs/{jobID} の JSON 応答です。
//
// 進行状況（JobStatus）を土台に、state が読めるジョブでは作品の状態を同じ文書に載せます。
// 投入直後から削除するまで同じ URL を同じ形で読めるので、呼び出し側は URL を切り替えずに
// 済みます。state を入れ子にするのは、state が job_id や title を自分でも持ち、平らに
// 並べると encoding/json が両方のキーを落とすためです。
type jobDocument struct {
	domain.JobStatus
	Comic any `json:"comic,omitempty"`
}

// Job はジョブ 1 件を返します（GET /jobs/{jobID}）。
//
// state（comic_state.json）が読めれば作品の詳細、まだ無ければ進行状況です。画面は
// 「処理中または未存在」の案内を出し、機械には記録があれば状態を、無ければ 404 を返します。
// デザインシートのジョブは state を持たないので、常に進行状況になります（成果物は
// /characters/{characterID} 側に並びます）。
func (h *Handler) Job(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if err := domain.ValidateJobID(jobID); err != nil {
		respond.Error(w, r, http.StatusBadRequest, "invalid job id")
		return
	}

	status := h.loadJobStatus(r, jobID)
	state, err := h.repository.GetState(r.Context(), jobID)

	if respond.WantsJSON(w, r) {
		switch {
		case err == nil:
			respond.JSON(w, r, http.StatusOK, jobDocument{JobStatus: completedStatus(jobID, status), Comic: state})
		case status != nil && errors.Is(err, domain.ErrStateNotFound):
			respond.JSON(w, r, http.StatusOK, *status)
		default:
			writeStateError(w, r, jobID, err)
		}
		return
	}

	if err != nil {
		if !errors.Is(err, domain.ErrStateNotFound) {
			slog.ErrorContext(r.Context(), "failed to load comic state", "job_id", jobID, "error", err)
			http.Error(w, "作品の読み込みに失敗しました。時間をおいて開き直してください。", http.StatusBadGateway)
			return
		}
		code := http.StatusNotFound
		if status != nil {
			// 記録があるなら「無い」のではなく「まだ」なので、案内は成功応答で返します。
			code = http.StatusOK
		}
		h.render(w, r, code, "history_pending.html", "処理中または未存在", historyPendingData{JobID: jobID})
		return
	}
	h.render(w, r, http.StatusOK, "history_detail.html", state.Title, h.buildDetailData(jobID, state))
}

// loadJobStatus は記録された進行状況を返します。無い（状態機能より前のジョブ）か読めなければ
// nil です。読めなかった場合をエラーにしないのは、作品の詳細は state から描けるからです。
func (h *Handler) loadJobStatus(r *http.Request, jobID string) *domain.JobStatus {
	if h.jobStatus == nil {
		return nil
	}
	status, err := h.jobStatus.Get(r.Context(), jobID)
	if err != nil {
		if !errors.Is(err, domain.ErrJobStatusNotFound) {
			slog.WarnContext(r.Context(), "failed to load job status", "job_id", jobID, "error", err)
		}
		return nil
	}
	return &status
}

// completedStatus は、state が読めたジョブの状態を返します。記録が無いジョブでも、
// state が読めた時点で少なくとも台本までは出来ています。
func completedStatus(jobID string, status *domain.JobStatus) domain.JobStatus {
	if status != nil {
		return *status
	}
	var completed domain.JobStatus
	completed.JobID = jobID
	completed.State = domain.JobStateSucceeded
	return completed
}

// Characters は、characters.json の全キャラクターを返します。
//
// 画面はサムネイルの URL を Web の経路へ書き換えますが、機械には定義そのままの
// 参照 URL を渡します。書き換えた URL は画面の画像エンドポイントに紐づくため、
// 機械が受け取っても素材の在り処にはなりません。
func (h *Handler) Characters(w http.ResponseWriter, r *http.Request) {
	if respond.WantsJSON(w, r) {
		items := make([]characterSummaryResponse, 0, h.characters.Len())
		for _, c := range h.characters.All() {
			items = append(items, characterSummaryResponse{
				ID:           c.ID,
				Name:         c.Name,
				ReferenceURL: c.ReferenceURL,
			})
		}
		respond.JSON(w, r, http.StatusOK, items)
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
		respond.Error(w, r, http.StatusNotFound, "character not found")
		return
	}

	if respond.WantsJSON(w, r) {
		history, err := h.repository.ListCharacterDesignHistory(r.Context(), characterID)
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to list character design history", "character_id", characterID, "error", err)
			respond.Error(w, r, http.StatusInternalServerError, "internal server error")
			return
		}
		respond.JSON(w, r, http.StatusOK, characterDetailResponse{
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
