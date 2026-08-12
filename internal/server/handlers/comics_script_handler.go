package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/shouni/ap-story/internal/domain"
)

// GetComicScript は GET /api/comics/{jobID}/script を処理し、台本のうち
// 編集できる部分（章の見出しと各コマのセリフ）だけを返します。
//
// state 全体は GET /api/comics/{jobID} で取れますが、そちらは生成記録や組み立て済みの
// プロンプトまで含んで100KBを超えます。校正のために通して読むには重いうえ、
// そのまま編集して戻せる形にもなっていません。
func (h *Handler) GetComicScript(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if err := domain.ValidateJobID(jobID); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid job id")
		return
	}

	state, err := h.repository.GetState(r.Context(), jobID)
	if err != nil {
		writeErrorJSON(w, http.StatusNotFound, "comic not found")
		return
	}

	writeJSON(w, http.StatusOK, domain.NewScriptDraft(state))
}

// UpdateComicScript は PUT /api/comics/{jobID}/script を処理し、送られてきた台本の
// セリフを state へ反映します。反映されるのはセリフだけで、コマの構成は動かせません
// （domain.ScriptDraft.ApplyTo を参照）。
//
// 保存してもページ画像は古いままです。セリフはページ合成のときに画像モデルが
// 描き込むので、直した文字を絵に載せるには対象ページを合成し直す必要があります。
// そのページ番号を affected_pages で返すのは、保存が終わりではないことを
// 呼び出し側に伝えるためです。
func (h *Handler) UpdateComicScript(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if err := domain.ValidateJobID(jobID); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid job id")
		return
	}

	var draft domain.ScriptDraft
	if err := json.NewDecoder(r.Body).Decode(&draft); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// 生成ジョブと編集が同時に走ると、state は「常に上書き・常に最新」なので後から
	// 書いたほうが勝ち、負けたほうの変更は痕跡なく消えます。go-comic-kit の store は
	// 条件付き書き込み（GCS の ifGenerationMatch）の口を持たないため、重なりそうな
	// 要求をここで断るのが唯一の防ぎ方です。
	if err := h.rejectWhileJobIsRunning(r, jobID); err != nil {
		writeErrorJSON(w, http.StatusConflict, err.Error())
		return
	}

	state, err := h.repository.GetState(r.Context(), jobID)
	if err != nil {
		writeErrorJSON(w, http.StatusNotFound, "comic not found")
		return
	}

	before := domain.NewScriptDraft(state)
	if err := draft.ApplyTo(state, h.isKnownSpeaker); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, err.Error())
		return
	}

	after := domain.NewScriptDraft(state)
	change := domain.DiffScripts(before, after)

	// 変更が無いなら書き込みません。保存のたびに state を上書きすると、
	// 実行中ジョブとの競合の窓を、何も変えない要求のためにわざわざ開くことになります。
	if change.ChangedLines > 0 {
		if err := h.repository.SaveState(r.Context(), jobID, state); err != nil {
			slog.ErrorContext(r.Context(), "failed to save comic script", "error", err, "job_id", jobID)
			writeErrorJSON(w, http.StatusInternalServerError, "failed to save script")
			return
		}
	}

	writeJSON(w, http.StatusOK, scriptUpdateResponse{
		JobID:        jobID,
		ScriptChange: change,
		Script:       after,
	})
}

// scriptUpdateResponse は台本更新の結果です。
type scriptUpdateResponse struct {
	JobID string `json:"job_id"`
	domain.ScriptChange
	// Script は保存後の台本です。空行の除去などの正規化が入るため、
	// 送った内容とそのまま同じとは限りません。
	Script domain.ScriptDraft `json:"script"`
}

// isKnownSpeaker は、話者IDが登録済みキャラクターかを返します。
func (h *Handler) isKnownSpeaker(id string) bool {
	if h.characters == nil {
		return true
	}
	return h.characters.GetCharacter(id) != nil
}

// rejectWhileJobIsRunning は、対象ジョブが処理中なら編集を断ります。
//
// 状態が記録されていない場合（jobStatus 未設定、または状態記録より前に作られた作品）は
// 素通しします。状態を追えないことを理由に編集まで止めると、記録の無い古い作品が
// 永久に直せなくなるためです。
func (h *Handler) rejectWhileJobIsRunning(r *http.Request, jobID string) error {
	if h.jobStatus == nil {
		return nil
	}
	status, err := h.jobStatus.Get(r.Context(), jobID)
	if err != nil {
		return nil
	}
	if status.State == domain.JobStateQueued || status.State == domain.JobStateRunning {
		return errors.New("生成ジョブの実行中は台本を編集できません（完了してから保存し直してください）")
	}
	return nil
}
