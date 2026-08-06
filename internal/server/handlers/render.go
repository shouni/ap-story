package handlers

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
)

const titleSuffix = " - AP Comic"

// activeNav はリクエストパスから、ナビゲーションでハイライトすべき項目のキーを返します。
// layout.html の `{{if eq .ActiveNav "..."}}` と対応します。
func activeNav(path string) string {
	switch {
	case strings.HasPrefix(path, "/history"):
		return "works"
	case strings.HasPrefix(path, "/characters"):
		return "characters"
	case strings.HasPrefix(path, "/compose"), strings.HasPrefix(path, "/design-sheets"):
		return "create"
	default:
		return ""
	}
}

// render は HTML テンプレートを描画してレスポンスに書き込みます。
// テンプレート実行エラー時に不完全な body を書き出さないよう、一度バッファへ描画してから
// レスポンスへ書き込みます。CSRF トークンは csrfAutoGenMiddleware がコンテキストへ
// 格納したものを layout.html の hidden input としてページ全体へ公開します。
func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, pageName string, title string, data any) {
	tmpl, ok := h.templates[pageName]
	if !ok {
		slog.Error("テンプレートキャッシュに見つかりません", "page", pageName)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	renderData := struct {
		Title     string
		ActiveNav string
		Data      any
		CSRFToken string
	}{
		Title:     title + titleSuffix,
		ActiveNav: activeNav(r.URL.Path),
		Data:      data,
		CSRFToken: CSRFTokenFromContext(r.Context()),
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout.html", renderData); err != nil {
		slog.Error("テンプレートの描画に失敗しました", "page", pageName, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		slog.Error("レスポンスの書き込みに失敗しました", "error", err)
	}
}
