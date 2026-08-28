package handlers

import (
	"log/slog"
	"net/http"

	"github.com/shouni/ap-story/internal/domain"

	"github.com/shouni/gcp-kit/negotiate"
)

// homeHistoryLimit は Home 画面に表示する履歴件数です。
const homeHistoryLimit = 5

// homeData は home.html テンプレートに渡すデータです。
type homeData struct {
	History []domain.ComicHistory
}

// Home は GET / を処理し、直近の履歴を数件表示します。未認証アクセスは
// protectedAccessMiddleware が /auth/login へリダイレクトするため、ここに到達するのは
// 認証済みのリクエストのみです。
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		negotiate.Error(w, r, http.StatusMethodNotAllowed, "method not allowed")
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
