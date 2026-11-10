package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/shouni/ap-story/internal/domain"

	"github.com/shouni/go-serve-kit/respond"
)

// JobRegenerate handles POST /jobs/{jobID}/regenerate and enqueues a regeneration command
// (regenerate_chapter_script / generate_design_sheet / regenerate_panel / regenerate_page)
// for the given job. The jobID always comes from the URL path, and job_id in the request body
// is ignored (use POST /jobs to enqueue a new job).
func (h *Handler) JobRegenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respond.ErrorJSON(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	jobID := chi.URLParam(r, "jobID")
	if err := domain.ValidateJobID(jobID); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, "invalid job id")
		return
	}

	var task domain.Task
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}
	task.JobID = jobID
	task.CreatedAt = time.Now().UTC()

	if task.Command == domain.TaskCommandComposeComic {
		respond.ErrorJSON(w, r, http.StatusBadRequest, "compose_comic is not a regenerate command; use POST /jobs instead")
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
