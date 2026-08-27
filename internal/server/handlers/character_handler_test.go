package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/shouni/ap-story/internal/domain"
)

func TestServeCharactersRendersRosterWithMasterReferenceThumbnail(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := httptest.NewRequest(http.MethodGet, "/characters", nil)
	rec := httptest.NewRecorder()

	h.Characters(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Zundamon", "/characters/zundamon",
		"/api/characters/reference/zundamon/default.png", // マスター参照画像がサムネイルに使われる
		"Metan", "/characters/metan",
		"/api/characters/reference/metan/default.png",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestServeCharacterDetailRendersReferencesAndHistoryNewestFirst(t *testing.T) {
	t.Parallel()

	repo := &fakeComicRepository{
		characterHistory: map[string][]domain.CharacterDesignHistoryItem{
			"zundamon": {
				{JobID: "job-2", ImageURL: "gs://test-bucket/character/zundamon/job-2.png"},
				{JobID: "job-1", ImageURL: "gs://test-bucket/character/zundamon/job-1.png"},
			},
		},
	}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)
	req := httptestRequestWithURLParam(t, http.MethodGet, "/characters/zundamon", "", "characterID", "zundamon")
	rec := httptest.NewRecorder()

	h.Character(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Zundamon",
		"/api/characters/reference/zundamon/default.png", // マスター参照画像
		"/api/characters/images/zundamon/job-2.png",      // 生成履歴（新しい順）
		"/api/characters/images/zundamon/job-1.png",
		"/design-sheets?character_id=zundamon",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestServeCharacterDetailRendersMultipleReferenceAspectRatios(t *testing.T) {
	t.Parallel()

	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, &fakeComicRepository{})
	req := httptestRequestWithURLParam(t, http.MethodGet, "/characters/metan", "", "characterID", "metan")
	rec := httptest.NewRecorder()

	h.Character(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"/api/characters/reference/metan/default.png",
		"/api/characters/reference/metan/16x9.png",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestServeCharacterDetailRendersEmptyState(t *testing.T) {
	t.Parallel()

	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, &fakeComicRepository{})
	req := httptestRequestWithURLParam(t, http.MethodGet, "/characters/zundamon", "", "characterID", "zundamon")
	rec := httptest.NewRecorder()

	h.Character(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "まだ生成履歴がありません") {
		t.Errorf("body missing empty state message, got: %s", rec.Body.String())
	}
}

func TestServeCharacterDetailReturns404ForUnknownCharacter(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := httptestRequestWithURLParam(t, http.MethodGet, "/characters/unknown", "", "characterID", "unknown")
	rec := httptest.NewRecorder()

	h.Character(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// manyDesignHistoryItems は characterDetailHistoryLimit を超える件数の履歴フィクスチャを返します。
func manyDesignHistoryItems(count int) []domain.CharacterDesignHistoryItem {
	items := make([]domain.CharacterDesignHistoryItem, 0, count)
	for i := count; i >= 1; i-- {
		jobID := fmt.Sprintf("job-%02d", i)
		items = append(items, domain.CharacterDesignHistoryItem{
			JobID:    jobID,
			ImageURL: "gs://test-bucket/character/zundamon/" + jobID + ".png",
		})
	}
	return items
}

func TestServeCharacterDetailCapsHistoryWithLinkToFullList(t *testing.T) {
	t.Parallel()

	repo := &fakeComicRepository{
		characterHistory: map[string][]domain.CharacterDesignHistoryItem{
			"zundamon": manyDesignHistoryItems(characterDetailHistoryLimit + 3),
		},
	}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)
	req := httptestRequestWithURLParam(t, http.MethodGet, "/characters/zundamon", "", "characterID", "zundamon")
	rec := httptest.NewRecorder()

	h.Character(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	// 新しい順の先頭 12 件は表示され、それ以降は表示されない
	if !strings.Contains(body, "job-15.png") || !strings.Contains(body, "job-04.png") {
		t.Errorf("body missing capped history entries")
	}
	if strings.Contains(body, "job-03.png") {
		t.Errorf("body should not contain entries beyond the cap")
	}
	if !strings.Contains(body, "/characters/zundamon/history") {
		t.Errorf("body missing link to full history page")
	}
	if !strings.Contains(body, "15件") {
		t.Errorf("body missing total history count")
	}
}

func TestServeCharacterHistoryRendersAllEntries(t *testing.T) {
	t.Parallel()

	repo := &fakeComicRepository{
		characterHistory: map[string][]domain.CharacterDesignHistoryItem{
			"zundamon": manyDesignHistoryItems(characterDetailHistoryLimit + 3),
		},
	}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)
	req := httptestRequestWithURLParam(t, http.MethodGet, "/characters/zundamon/history", "", "characterID", "zundamon")
	rec := httptest.NewRecorder()

	h.ServeCharacterHistory(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "job-15.png") || !strings.Contains(body, "job-01.png") {
		t.Errorf("full history page should contain all entries")
	}
}

func TestDeleteCharacterDesignDeletesViaRepository(t *testing.T) {
	t.Parallel()

	repo := &fakeComicRepository{}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)
	req := httptest.NewRequest(http.MethodDelete, "/api/characters/zundamon/images/job-1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("characterID", "zundamon")
	rctx.URLParams.Add("jobID", "job-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.DeleteCharacterDesign(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if len(repo.deletedDesigns) != 1 || repo.deletedDesigns[0] != "zundamon/job-1" {
		t.Errorf("deletedDesigns = %v, want [zundamon/job-1]", repo.deletedDesigns)
	}
}

func TestDeleteCharacterDesignReturns404ForUnknownCharacter(t *testing.T) {
	t.Parallel()

	repo := &fakeComicRepository{}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)
	req := httptest.NewRequest(http.MethodDelete, "/api/characters/unknown/images/job-1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("characterID", "unknown")
	rctx.URLParams.Add("jobID", "job-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.DeleteCharacterDesign(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if len(repo.deletedDesigns) != 0 {
		t.Errorf("repository delete should not be called for unknown character")
	}
}
