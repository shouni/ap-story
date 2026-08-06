package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	kitports "github.com/shouni/go-comic-kit/ports"

	"github.com/shouni/ap-story/internal/domain"
)

func TestListComicsSuccess(t *testing.T) {
	t.Parallel()

	repo := &fakeComicRepository{
		historyPage: domain.ComicHistoryPage{
			Items:    []domain.ComicHistory{{JobID: "job-1", Title: "夜明けのデプロイ"}},
			PageMeta: domain.PageMeta{Page: 1, PerPage: 20, Total: 1},
		},
	}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)

	req := httptest.NewRequest(http.MethodGet, "/api/comics", nil)
	rec := httptest.NewRecorder()
	h.ListComics(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got domain.ComicHistoryPage
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].JobID != "job-1" {
		t.Errorf("Items = %+v, want job-1", got.Items)
	}
}

func TestListComicsReturns500OnRepositoryError(t *testing.T) {
	t.Parallel()

	repo := &fakeComicRepository{listErr: context.DeadlineExceeded}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)

	req := httptest.NewRequest(http.MethodGet, "/api/comics", nil)
	rec := httptest.NewRecorder()
	h.ListComics(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestGetComicSuccess(t *testing.T) {
	t.Parallel()

	state := &kitports.MangaState{ID: "job-1", Title: "夜明けのデプロイ", CreatedAt: time.Now()}
	repo := &fakeComicRepository{states: map[string]*kitports.MangaState{"job-1": state}}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)

	req := httptestRequestWithURLParam(t, http.MethodGet, "/api/comics/job-1", "", "jobID", "job-1")
	rec := httptest.NewRecorder()
	h.GetComic(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got kitports.MangaState
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got.ID != "job-1" {
		t.Errorf("ID = %q, want job-1", got.ID)
	}
}

func TestGetComicRejectsInvalidJobID(t *testing.T) {
	t.Parallel()

	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, &fakeComicRepository{})
	req := httptestRequestWithURLParam(t, http.MethodGet, "/api/comics/..%2Fescape", "", "jobID", "../escape")
	rec := httptest.NewRecorder()
	h.GetComic(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetComicReturns404WhenNotFound(t *testing.T) {
	t.Parallel()

	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, &fakeComicRepository{})
	req := httptestRequestWithURLParam(t, http.MethodGet, "/api/comics/job-missing", "", "jobID", "job-missing")
	rec := httptest.NewRecorder()
	h.GetComic(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDeleteComicSuccess(t *testing.T) {
	t.Parallel()

	repo := &fakeComicRepository{}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)

	req := httptestRequestWithURLParam(t, http.MethodDelete, "/api/comics/job-1", "", "jobID", "job-1")
	rec := httptest.NewRecorder()
	h.DeleteComic(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != "job-1" {
		t.Errorf("deleted = %v, want [job-1]", repo.deleted)
	}
}

func TestDeleteComicRejectsInvalidJobID(t *testing.T) {
	t.Parallel()

	repo := &fakeComicRepository{}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)

	req := httptestRequestWithURLParam(t, http.MethodDelete, "/api/comics/bad", "", "jobID", "../escape")
	rec := httptest.NewRecorder()
	h.DeleteComic(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if len(repo.deleted) != 0 {
		t.Error("DeleteHistory was called despite invalid job id")
	}
}
