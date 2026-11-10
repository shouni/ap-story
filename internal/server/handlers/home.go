package handlers

import (
	"log/slog"
	"net/http"

	"github.com/shouni/ap-story/internal/domain"

	"github.com/shouni/go-serve-kit/respond"
)

// homeHistoryLimit is the number of history items shown on the Home page.
const homeHistoryLimit = 5

// homeData is the data passed to the home.html template.
type homeData struct {
	History []domain.ComicHistory
}

// Home handles GET / and shows a few recent history items. Unauthenticated access is
// redirected to /auth/login by protectedAccessMiddleware, so only authenticated requests
// reach here.
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respond.Error(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	page, err := h.repository.ListHistoryPage(r.Context(), 1, homeHistoryLimit)
	if err != nil {
		slog.Error("failed to load history for home page", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, http.StatusOK, "home.html", "Home", homeData{History: page.Items})
}
