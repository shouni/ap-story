package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	kitcomic "github.com/shouni/go-comic-kit/comic"

	"github.com/shouni/ap-story/internal/domain"
)

const scriptJobID = "c-20260811-185837-3ed89ff00505"

func scriptTestState() *kitcomic.MangaState {
	return &kitcomic.MangaState{
		ID:    scriptJobID,
		Title: "作品",
		Chapters: []kitcomic.Chapter{
			{ID: "ch01", Title: "第1話", Summary: "あらすじ"},
		},
		Panels: []kitcomic.Panel{
			{
				ID: "ch01-p01", ChapterID: "ch01", Page: 1,
				Dialogues:  []kitcomic.DialogueLine{{SpeakerID: "zundamon", Text: "元のセリフなのだ", Kind: "speech"}},
				Generation: &kitcomic.GenerationRecord{ImageURL: "gs://b/p01.png", UsedSeed: 7},
			},
			{
				ID: "ch01-p02", ChapterID: "ch01", Page: 2,
				Dialogues: []kitcomic.DialogueLine{{SpeakerID: "metan", Text: "そうよ", Kind: "speech"}},
			},
		},
	}
}

// scriptRequest builds a request carrying chi's URL parameters.
func scriptRequest(t *testing.T, method, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, "/jobs/"+scriptJobID+"/script", strings.NewReader(body))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("jobID", scriptJobID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

// runningJobStatus is the state "a generation job is running".
func runningJobStatus() domain.JobStatus {
	status := domain.NewQueuedJobStatus(domain.Task{
		JobID:   scriptJobID,
		Command: domain.TaskCommandRegeneratePanel,
	}, time.Now().UTC())
	status.State = domain.JobStateRunning
	return status
}

func newScriptHandler(t *testing.T) (*Handler, *fakeComicRepository) {
	t.Helper()
	repo := &fakeComicRepository{states: map[string]*kitcomic.MangaState{scriptJobID: scriptTestState()}}
	return newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo), repo
}

func TestGetComicScriptReturnsOnlyTheEditablePart(t *testing.T) {
	t.Parallel()

	h, _ := newScriptHandler(t)
	rec := httptest.NewRecorder()
	h.JobScript(rec, scriptRequest(t, http.MethodGet, ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var draft domain.ScriptDraft
	if err := json.Unmarshal(rec.Body.Bytes(), &draft); err != nil {
		t.Fatalf("the response cannot be read as a script: %v", err)
	}
	if len(draft.Panels) != 2 || draft.Panels[0].Dialogues[0].Text != "元のセリフなのだ" {
		t.Errorf("the script was not retrieved: %+v", draft)
	}
	// Generation records are not editing input, so they do not appear in the script
	if strings.Contains(rec.Body.String(), "used_seed") {
		t.Errorf("generation records are mixed into the script: %s", rec.Body.String())
	}
}

func TestUpdateComicScriptSavesTheEditedLines(t *testing.T) {
	t.Parallel()

	h, repo := newScriptHandler(t)
	body := `{"panels":[
		{"panel_id":"ch01-p01","dialogues":[{"speaker_id":"zundamon","text":"直したのだ","kind":"speech"}]},
		{"panel_id":"ch01-p02","dialogues":[{"speaker_id":"metan","text":"そうよ","kind":"speech"}]}
	]}`

	rec := httptest.NewRecorder()
	h.JobScriptUpdate(rec, scriptRequest(t, http.MethodPut, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if len(repo.saved) != 1 {
		t.Fatalf("save count = %d, want 1", len(repo.saved))
	}
	if got := repo.saved[0].Panels[0].Dialogues[0].Text; got != "直したのだ" {
		t.Errorf("saved dialogue = %q", got)
	}
	// It is the very mapping to the panel images, so saving the script must not erase it
	if repo.saved[0].Panels[0].Generation == nil {
		t.Error("the generation record was lost on save")
	}
}

func TestUpdateComicScriptReportsThePagesThatMustBeRecomposed(t *testing.T) {
	t.Parallel()

	// The image model draws the dialogue in at page composition, so saving alone leaves the art as it was.
	// Unless it returns which pages to recompose, saving would be mistaken for the end of the job.
	h, _ := newScriptHandler(t)
	body := `{"panels":[
		{"panel_id":"ch01-p01","dialogues":[{"speaker_id":"zundamon","text":"元のセリフなのだ","kind":"speech"}]},
		{"panel_id":"ch01-p02","dialogues":[{"speaker_id":"metan","text":"直したわ","kind":"speech"}]}
	]}`

	rec := httptest.NewRecorder()
	h.JobScriptUpdate(rec, scriptRequest(t, http.MethodPut, body))

	var got struct {
		ChangedLines  int   `json:"changed_lines"`
		AffectedPages []int `json:"affected_pages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("cannot read the response: %v", err)
	}
	if got.ChangedLines != 1 {
		t.Errorf("changed_lines = %d, want 1", got.ChangedLines)
	}
	// Only the panel on page 2 was changed
	if len(got.AffectedPages) != 1 || got.AffectedPages[0] != 2 {
		t.Errorf("affected_pages = %v, want [2]", got.AffectedPages)
	}
}

func TestUpdateComicScriptDoesNotWriteWhenNothingChanged(t *testing.T) {
	t.Parallel()

	// Overwriting the state for a request that changes nothing would open the window for
	// a race with a running job for no reason.
	h, repo := newScriptHandler(t)
	body := `{"panels":[
		{"panel_id":"ch01-p01","dialogues":[{"speaker_id":"zundamon","text":"元のセリフなのだ","kind":"speech"}]},
		{"panel_id":"ch01-p02","dialogues":[{"speaker_id":"metan","text":"そうよ","kind":"speech"}]}
	]}`

	rec := httptest.NewRecorder()
	h.JobScriptUpdate(rec, scriptRequest(t, http.MethodPut, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(repo.saved) != 0 {
		t.Errorf("saved although nothing changed (%d times)", len(repo.saved))
	}
}

func TestUpdateComicScriptRejectsChangesToThePanelLineup(t *testing.T) {
	t.Parallel()

	h, repo := newScriptHandler(t)
	// A request that drops one panel
	body := `{"panels":[
		{"panel_id":"ch01-p01","dialogues":[{"speaker_id":"zundamon","text":"直したのだ","kind":"speech"}]}
	]}`

	rec := httptest.NewRecorder()
	h.JobScriptUpdate(rec, scriptRequest(t, http.MethodPut, body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	if len(repo.saved) != 0 {
		t.Error("saved although it was rejected")
	}
}

func TestUpdateComicScriptRefusesWhileAJobIsRunning(t *testing.T) {
	t.Parallel()

	// The state is "always overwrite, always latest", so overlapping with a generation job erases the edit without a trace.
	repo := &fakeComicRepository{states: map[string]*kitcomic.MangaState{scriptJobID: scriptTestState()}}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)
	status := &fakeJobStatusStore{}
	if err := status.Save(t.Context(), scriptJobID, runningJobStatus()); err != nil {
		t.Fatalf("failed to record the status: %v", err)
	}
	h.jobStatus = status

	body := `{"panels":[
		{"panel_id":"ch01-p01","dialogues":[{"speaker_id":"zundamon","text":"直したのだ","kind":"speech"}]},
		{"panel_id":"ch01-p02","dialogues":[{"speaker_id":"metan","text":"そうよ","kind":"speech"}]}
	]}`
	rec := httptest.NewRecorder()
	h.JobScriptUpdate(rec, scriptRequest(t, http.MethodPut, body))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body: %s)", rec.Code, rec.Body.String())
	}
	if len(repo.saved) != 0 {
		t.Error("saved although a job is running")
	}
}

func TestUpdateComicScriptAllowsEditingWhenNoStatusWasEverRecorded(t *testing.T) {
	t.Parallel()

	// Stopping edits because the status cannot be followed would leave works made before status recording
	// unfixable forever.
	repo := &fakeComicRepository{states: map[string]*kitcomic.MangaState{scriptJobID: scriptTestState()}}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)
	h.jobStatus = &fakeJobStatusStore{} // nothing recorded = Get returns an error

	body := `{"panels":[
		{"panel_id":"ch01-p01","dialogues":[{"speaker_id":"zundamon","text":"直したのだ","kind":"speech"}]},
		{"panel_id":"ch01-p02","dialogues":[{"speaker_id":"metan","text":"そうよ","kind":"speech"}]}
	]}`
	rec := httptest.NewRecorder()
	h.JobScriptUpdate(rec, scriptRequest(t, http.MethodPut, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateComicScriptRejectsAnUnknownSpeaker(t *testing.T) {
	t.Parallel()

	h, _ := newScriptHandler(t)
	body := `{"panels":[
		{"panel_id":"ch01-p01","dialogues":[{"speaker_id":"dareka","text":"誰なのだ","kind":"speech"}]},
		{"panel_id":"ch01-p02","dialogues":[{"speaker_id":"metan","text":"そうよ","kind":"speech"}]}
	]}`

	rec := httptest.NewRecorder()
	h.JobScriptUpdate(rec, scriptRequest(t, http.MethodPut, body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}
