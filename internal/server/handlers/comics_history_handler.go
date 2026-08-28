package handlers

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/shouni/ap-story/internal/domain"

	"github.com/shouni/gcp-kit/negotiate"
)

// defaultHistoryPageSize は履歴一覧のデフォルトページサイズです。
const defaultHistoryPageSize = 20

// DeleteComic は DELETE /api/comics/{jobID} を処理し、指定ジョブの成果物一式を削除します。
func (h *Handler) DeleteComic(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if err := domain.ValidateJobID(jobID); err != nil {
		negotiate.Error(w, r, http.StatusBadRequest, "invalid job id")
		return
	}

	if err := h.repository.DeleteHistory(r.Context(), jobID); err != nil {
		slog.ErrorContext(r.Context(), "failed to delete comic history", "job_id", jobID, "error", err)
		negotiate.Error(w, r, http.StatusInternalServerError, "internal server error")
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
