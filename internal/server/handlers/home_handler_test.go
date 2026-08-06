package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shouni/ap-story/internal/domain"
)

func TestHomeRendersHistory(t *testing.T) {
	t.Parallel()

	repo := &fakeComicRepository{
		historyPage: domain.ComicHistoryPage{
			Items: []domain.ComicHistory{
				{JobID: "job-1", Title: "テスト作品", ChapterCount: 3, PanelCount: 12, PageCount: 4},
			},
		},
	}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	h.Home(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "テスト作品") {
		t.Errorf("body did not contain history title, got: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "job-1") {
		t.Errorf("body did not contain job id, got: %s", rec.Body.String())
	}
}

func TestHomeRendersEmptyStateWithoutHistory(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	h.Home(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "作品がまだありません") {
		t.Errorf("body did not contain empty state message, got: %s", rec.Body.String())
	}
}

func TestHomeRejectsNonGetMethod(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()

	h.Home(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
