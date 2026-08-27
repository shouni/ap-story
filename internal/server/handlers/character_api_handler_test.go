package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shouni/ap-story/internal/domain"
)

func TestListCharactersReturnsAllCharacters(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := httptest.NewRequest(http.MethodGet, "/api/characters", nil)
	rec := httptest.NewRecorder()

	req.Header.Set("Accept", "application/json")
	h.Characters(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var items []characterSummaryResponse
	if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %v, want 2 entries", items)
	}
	if items[0].ID != "zundamon" || items[0].ReferenceURL != "gs://test-bucket/character-reference/zundamon/default.png" {
		t.Errorf("items[0] = %+v, unexpected", items[0])
	}
}

func TestGetCharacterDetailReturnsReferencesAndHistory(t *testing.T) {
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
	req := httptestRequestWithURLParam(t, http.MethodGet, "/api/characters/zundamon", "", "characterID", "zundamon")
	rec := httptest.NewRecorder()

	req.Header.Set("Accept", "application/json")
	h.Character(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp characterDetailResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ID != "zundamon" || resp.ReferenceURL != "gs://test-bucket/character-reference/zundamon/default.png" {
		t.Errorf("resp = %+v, unexpected reference", resp)
	}
	if len(resp.History) != 2 || resp.History[0].JobID != "job-2" {
		t.Errorf("resp.History = %+v, want job-2 first", resp.History)
	}
}

func TestGetCharacterDetailReturns404ForUnknownCharacter(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := httptestRequestWithURLParam(t, http.MethodGet, "/api/characters/unknown", "", "characterID", "unknown")
	rec := httptest.NewRecorder()

	req.Header.Set("Accept", "application/json")
	h.Character(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
