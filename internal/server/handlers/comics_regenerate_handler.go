package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/shouni/ap-story/internal/domain"

	"github.com/shouni/gcp-kit/negotiate"
)

// RegenerateComic は POST /api/comics/{jobID}/regenerate を処理し、指定ジョブに対する
// 再生成コマンド（regenerate_chapter_script / generate_design_sheet / regenerate_panel /
// regenerate_page）を投入します。jobID は必ず URL パスから取得し、リクエストボディの
// job_id は無視します（新規ジョブの投入は POST /api/comics を使ってください）。
func (h *Handler) RegenerateComic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		negotiate.ErrorJSON(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	jobID := chi.URLParam(r, "jobID")
	if err := domain.ValidateJobID(jobID); err != nil {
		negotiate.ErrorJSON(w, r, http.StatusBadRequest, "invalid job id")
		return
	}

	var task domain.Task
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		negotiate.ErrorJSON(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}
	task.JobID = jobID
	task.CreatedAt = time.Now().UTC()

	if task.Command == domain.TaskCommandComposeComic {
		negotiate.ErrorJSON(w, r, http.StatusBadRequest, "compose_comic is not a regenerate command; use POST /api/comics instead")
		return
	}

	if err := task.ValidateSubmission(); err != nil {
		negotiate.ErrorJSON(w, r, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.validateChoices(task); err != nil {
		negotiate.ErrorJSON(w, r, http.StatusBadRequest, err.Error())
		return
	}

	h.enqueueAndRespond(w, r, task)
}
