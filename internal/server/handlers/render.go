package handlers

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
)

const titleSuffix = " - AP Story"

// activeNav returns, from the request path, the key of the navigation item to highlight.
// It corresponds to `{{if eq .ActiveNav "..."}}` in layout.html.
func activeNav(path string) string {
	switch {
	case strings.HasPrefix(path, "/jobs"):
		return "works"
	case strings.HasPrefix(path, "/characters"):
		return "characters"
	case strings.HasPrefix(path, "/compose"):
		return "create"
	default:
		return ""
	}
}

// render renders an HTML template and writes it to the response.
// So that an incomplete body is never written on a template error, it renders into a buffer first
// and then writes to the response. The CSRF token stored in the context by csrfAutoGenMiddleware
// is exposed to the whole page as a hidden input in layout.html.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, pageName string, title string, data any) {
	tmpl, ok := h.templates[pageName]
	if !ok {
		slog.Error("template not found in the cache", "page", pageName)
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
		slog.Error("failed to render the template", "page", pageName, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		slog.Error("failed to write the response", "error", err)
	}
}
