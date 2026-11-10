package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	kitcomic "github.com/shouni/go-comic-kit/comic"

	"github.com/go-chi/chi/v5"
	characterkit "github.com/shouni/go-character-kit/character"

	"github.com/shouni/ap-story/internal/adapters/prompts"
	"github.com/shouni/ap-story/internal/domain"
)

// testCharacters is a test fixture with a fixed list of characters.
func testCharacters(t *testing.T) *characterkit.Characters {
	t.Helper()
	chars, err := characterkit.NewCharacters([]characterkit.Character{
		{
			ID: "zundamon", Name: "Zundamon",
			ReferenceURL: "gs://test-bucket/character-reference/zundamon/default.png",
			VisualCues:   []string{"green hair"},
		},
		{
			ID: "metan", Name: "Metan",
			ReferenceURL: "gs://test-bucket/character-reference/metan/default.png",
			ReferenceURLs: map[string]string{
				"16:9": "gs://test-bucket/character-reference/metan/16x9.png",
			},
			VisualCues: []string{"purple hair"},
		},
	})
	if err != nil {
		t.Fatalf("testCharacters failed: %v", err)
	}
	return chars
}

// httptestRequestWithURLParam builds a test request with URL parameters, readable by chi.URLParam,
// embedded in the context. When body is empty the request has an empty body.
func httptestRequestWithURLParam(t *testing.T, method, target, body, key, value string) *http.Request {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

type fakeTaskQueue struct {
	lastTask domain.Task
	called   int
	err      error
	// onEnqueue is used to check call order (relative to recording the job status).
	onEnqueue func()
}

func (f *fakeTaskQueue) Enqueue(_ context.Context, task domain.Task) error {
	f.called++
	f.lastTask = task
	if f.onEnqueue != nil {
		f.onEnqueue()
	}
	return f.err
}

// fakeJobStatusStore observes the job status recorded at enqueue.
type fakeJobStatusStore struct {
	saved  []domain.JobStatus
	onSave func()
}

func (f *fakeJobStatusStore) Save(_ context.Context, _ string, status domain.JobStatus) error {
	f.saved = append(f.saved, status)
	if f.onSave != nil {
		f.onSave()
	}
	return nil
}

func (f *fakeJobStatusStore) Get(_ context.Context, _ string) (domain.JobStatus, error) {
	if len(f.saved) == 0 {
		return domain.JobStatus{}, fmt.Errorf("not recorded")
	}
	return f.saved[len(f.saved)-1], nil
}

func newTestHandlerWithJobStatus(t *testing.T, q *fakeTaskQueue, status domain.JobStatusStore) *Handler {
	t.Helper()
	h := newTestHandler(t, q)
	h.jobStatus = status
	return h
}

type fakeComicRepository struct {
	historyPage         domain.ComicHistoryPage
	listErr             error
	states              map[string]*kitcomic.MangaState
	getErr              error
	deleted             []string
	deleteErr           error
	characterHistory    map[string][]domain.CharacterDesignHistoryItem
	characterHistoryErr error
	deletedDesigns      []string
	deleteDesignErr     error
	saved               []*kitcomic.MangaState
	saveErr             error
}

func (f *fakeComicRepository) ListCharacterDesignHistory(_ context.Context, characterID string) ([]domain.CharacterDesignHistoryItem, error) {
	if f.characterHistoryErr != nil {
		return nil, f.characterHistoryErr
	}
	return f.characterHistory[characterID], nil
}

func (f *fakeComicRepository) DeleteCharacterDesign(_ context.Context, characterID string, jobID string) error {
	if f.deleteDesignErr != nil {
		return f.deleteDesignErr
	}
	f.deletedDesigns = append(f.deletedDesigns, characterID+"/"+jobID)
	return nil
}

func (f *fakeComicRepository) ListHistoryPage(_ context.Context, _ int, _ int) (domain.ComicHistoryPage, error) {
	return f.historyPage, f.listErr
}

func (f *fakeComicRepository) GetState(_ context.Context, jobID string) (*kitcomic.MangaState, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	state, ok := f.states[jobID]
	if !ok {
		// The real GetState returns a missing state as ErrStateNotFound. A bare error would let
		// the checks of handlers that treat "not there yet" and "unreadable" separately slip through.
		return nil, fmt.Errorf("%w (job %q)", domain.ErrStateNotFound, jobID)
	}
	return state, nil
}

func (f *fakeComicRepository) SaveState(_ context.Context, jobID string, state *kitcomic.MangaState) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, state)
	if f.states == nil {
		f.states = map[string]*kitcomic.MangaState{}
	}
	f.states[jobID] = state
	return nil
}

func (f *fakeComicRepository) DeleteHistory(_ context.Context, jobID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, jobID)
	return nil
}

// fakeSigner is a test fake satisfying Signer.
type fakeSigner struct {
	lastObjectURI string
	lastExpires   time.Duration
	signedURL     string
	err           error
}

func (f *fakeSigner) SignURL(_ context.Context, objectURI, _ string, expires time.Duration) (string, error) {
	f.lastObjectURI = objectURI
	f.lastExpires = expires
	if f.err != nil {
		return "", f.err
	}
	if f.signedURL != "" {
		return f.signedURL, nil
	}
	return "https://signed.example.com/" + objectURI, nil
}

func newTestHandler(t *testing.T, q *fakeTaskQueue) *Handler {
	t.Helper()
	return newTestHandlerWithRepo(t, q, &fakeComicRepository{})
}

func newTestHandlerWithRepo(t *testing.T, q *fakeTaskQueue, repo *fakeComicRepository) *Handler {
	t.Helper()
	return newTestHandlerFull(t, q, repo, &fakeSigner{})
}

func newTestHandlerFull(t *testing.T, q *fakeTaskQueue, repo *fakeComicRepository, signer *fakeSigner) *Handler {
	t.Helper()
	h, err := NewHandler(HandlerOptions{
		TaskQueue:    q,
		Repository:   repo,
		Signer:       signer,
		Bucket:       "test-bucket",
		Characters:   testCharacters(t),
		ImageModels:  []string{"image-model", "image-alt"},
		GeminiModels: []string{"gemini-model", "gemini-alt"},
		// In production these are template names under assets/prompts. Here the only purpose is
		// validating the options, so it has one default mode and one extra mode.
		ScriptModes: []prompts.ModeInfo{{Name: prompts.ModeDefault}, {Name: "alt"}},
		StyleModes:  []prompts.ModeInfo{{Name: prompts.ModeDefault}, {Name: "watercolor"}},
	})
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}
	return h
}
