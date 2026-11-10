package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// browserAccept is the Accept a browser actually sends.
// It does not include application/json, so the representation falls to HTML.
const browserAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

// The same route answers in the representation the caller asks for.
//
// The MCP gateway's HTTP client puts Accept: application/json on every request, so
// machines keep receiving JSON after the /api aliases were removed.
func TestCharactersServesBothAudiences(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})

	t.Run("JSON for machines", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/characters", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		h.Characters(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		var items []characterSummaryResponse
		if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
			t.Fatalf("cannot read the JSON: %v", err)
		}
		if len(items) == 0 {
			t.Error("not a single character was returned")
		}
	})

	t.Run("a page for people", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/characters", nil)
		req.Header.Set("Accept", browserAccept)
		rec := httptest.NewRecorder()
		h.Characters(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("Content-Type"); strings.HasPrefix(got, "application/json") {
			t.Errorf("Content-Type = %q, want HTML", got)
		}
	})

	// The same URL changes its content by Accept, so the cache must be told.
	t.Run("sets Vary: Accept", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/characters", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		h.Characters(rec, req)

		if got := rec.Header().Get("Vary"); got != "Accept" {
			t.Errorf("Vary = %q, want %q", got, "Accept")
		}
	})
}

// An invalid job ID's error is also returned in the requested representation.
// A machine cannot interpret an HTML error page.
func TestComicErrorFollowsTheRequestedFormat(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})

	req := httptest.NewRequest(http.MethodGet, "/jobs/bad", nil)
	req.Header.Set("Accept", "application/json")
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("jobID", "../etc/passwd")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
	rec := httptest.NewRecorder()

	h.Job(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", got)
	}
}
