package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/shouni/ap-story/internal/domain"

	"github.com/shouni/go-serve-kit/respond"
)

// defaultHistoryPageSize is the default page size of the history list.
const defaultHistoryPageSize = 20

// JobDelete handles DELETE /jobs/{jobID} and deletes the job's full set of artifacts.
func (h *Handler) JobDelete(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if err := domain.ValidateJobID(jobID); err != nil {
		respond.Error(w, r, http.StatusBadRequest, "invalid job id")
		return
	}

	if err := h.repository.DeleteHistory(r.Context(), jobID); err != nil {
		respond.ServerError(w, r, http.StatusInternalServerError, err, "failed to delete comic history", "job_id", jobID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseHistoryPage(r *http.Request) int {
	page, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("page")))
	if err != nil || page < 1 {
		return 1
	}
	return page
}
