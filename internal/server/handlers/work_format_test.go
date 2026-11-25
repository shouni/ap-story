package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	kitcomic "github.com/shouni/go-comic-kit/comic"

	"github.com/shouni/ap-story/internal/adapters/prompts"
	"github.com/shouni/ap-story/internal/domain"
)

// This file covers the choices one work makes for itself at submission (language, ratio, cast)
// and the copy that carries a finished work into another language.

func postComic(t *testing.T, h *Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.createComicJSON(rec, httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(body)))
	return rec
}

func TestEnqueueComicCarriesLanguageRatioAndCast(t *testing.T) {
	t.Parallel()
	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	rec := postComic(t, h, `{"source_text": "歌詞", "lang": " en ", "aspect_ratio": "9:16", "character_ids": ["metan", " ", ""]}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body: %s", rec.Code, rec.Body.String())
	}
	task := q.lastTask
	if task.Lang != "en" || task.AspectRatio != "9:16" || !slices.Equal(task.CharacterIDs, []string{"metan"}) {
		t.Errorf("task = lang %q, ratio %q, cast %v; want en, 9:16 and [metan] (blank entries dropped)",
			task.Lang, task.AspectRatio, task.CharacterIDs)
	}
}

func TestEnqueueComicRejectsBadWorkChoicesBeforeEnqueue(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"unsupported language":   `{"source_text": "x", "lang": "fr"}`,
		"landscape ratio":        `{"source_text": "x", "aspect_ratio": "16:9"}`,
		"unregistered character": `{"source_text": "x", "character_ids": ["metan", "nobody"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			q := &fakeTaskQueue{}
			rec := postComic(t, newTestHandler(t, q), body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
			}
			if q.called != 0 {
				t.Error("the job was enqueued; the worker would only fail it after accepting it")
			}
		})
	}
}

func TestEnqueueComicChecksTheScriptModesOwnCast(t *testing.T) {
	t.Parallel()
	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)
	// characters.json is replaced without a deploy, so a mode can outlive a character it was written for.
	h.scriptModes = []prompts.ModeInfo{
		{Name: prompts.ModeDefault, Cast: []string{"zundamon", "metan"}},
		{Name: "orphaned", Cast: []string{"zundamon", "retired"}},
	}

	if rec := postComic(t, h, `{"source_text": "x"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("a mode whose cast is registered was refused: %d %s", rec.Code, rec.Body.String())
	}
	if len(q.lastTask.CharacterIDs) != 0 {
		t.Errorf("task cast = %v, want empty: the worker reads the mode's cast, so the two cannot disagree", q.lastTask.CharacterIDs)
	}

	rec := postComic(t, h, `{"source_text": "x", "script_mode": "orphaned"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "retired") {
		t.Errorf("status = %d, body = %s; want 400 naming the missing character", rec.Code, rec.Body.String())
	}
	// Naming a cast of one's own replaces the mode's, so the same mode is usable again.
	if rec := postComic(t, h, `{"source_text": "x", "script_mode": "orphaned", "character_ids": ["metan"]}`); rec.Code != http.StatusAccepted {
		t.Errorf("an explicit registered cast was refused: %d %s", rec.Code, rec.Body.String())
	}
}

func TestComicOptionsListWorkChoicesAndModeFormats(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t, &fakeTaskQueue{})
	h.scriptModes = []prompts.ModeInfo{
		{Name: prompts.ModeDefault, Cast: []string{"zundamon"}},
		{Name: "song_story", MaxChapters: 1, MaxPanels: 6},
	}

	rec := httptest.NewRecorder()
	h.ComicOptions(rec, httptest.NewRequest(http.MethodGet, "/comic-options", nil))
	var resp comicOptionsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !slices.Equal(resp.Langs, domain.SupportedLangs()) || !slices.Equal(resp.AspectRatios, domain.ComicAspectRatios()) {
		t.Errorf("langs = %v, aspect_ratios = %v; want the lists the submission is validated against", resp.Langs, resp.AspectRatios)
	}
	if got := resp.ScriptModes[0].Cast; !slices.Equal(got, []string{"zundamon"}) {
		t.Errorf("default mode cast = %v, want [zundamon]", got)
	}
	if song := resp.ScriptModes[1]; song.MaxChapters != 1 || song.MaxPanels != 6 {
		t.Errorf("song_story format = %+v, want 1 chapter and 6 panels", song)
	}
}

func TestComposeFormOffersLanguageRatioAndCast(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t, &fakeTaskQueue{})
	rec := httptest.NewRecorder()
	h.ComposeForm(rec, httptest.NewRequest(http.MethodGet, "/compose", nil))
	body := rec.Body.String()

	for _, want := range []string{
		`name="lang"`, `<option value="ja" selected>`, `<option value="en">`,
		`name="aspect_ratio"`, `<option value="3:4" selected>`, `<option value="9:16">`,
		`name="character_ids"`, `value="zundamon"`, `value="metan"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the form does not contain %s", want)
		}
	}
	if strings.Contains(body, `value="16:9"`) {
		t.Error("the form offers a landscape comic, which the submission refuses")
	}
}

func TestComposeFormSubmitsTheWorkChoices(t *testing.T) {
	t.Parallel()
	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	form := url.Values{
		"source_text":   {"歌詞"},
		"lang":          {"en"},
		"aspect_ratio":  {"1:1"},
		"character_ids": {"zundamon", "metan"},
	}
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.createComicForm(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body: %s", rec.Code, rec.Body.String())
	}
	task := q.lastTask
	if task.Lang != "en" || task.AspectRatio != "1:1" || !slices.Equal(task.CharacterIDs, []string{"zundamon", "metan"}) {
		t.Errorf("task = lang %q, ratio %q, cast %v", task.Lang, task.AspectRatio, task.CharacterIDs)
	}
	if !task.StopAfterScript {
		t.Error("the form ran past the script")
	}
}

const duplicateSourceID = "c-20261125-000000-aaaaaaaaaaaa"

func duplicateRequest(t *testing.T, jobID, body string) *http.Request {
	t.Helper()
	return httptestRequestWithURLParam(t, http.MethodPost, "/jobs/"+jobID+"/duplicate", body, "jobID", jobID)
}

func handlerWithWork(t *testing.T, q *fakeTaskQueue) *Handler {
	t.Helper()
	return newTestHandlerWithRepo(t, q, &fakeComicRepository{
		states: map[string]*kitcomic.MangaState{duplicateSourceID: {ID: duplicateSourceID, Title: "曲名"}},
	})
}

func TestDuplicateEnqueuesACopyUnderANewJob(t *testing.T) {
	t.Parallel()
	q := &fakeTaskQueue{}
	h := handlerWithWork(t, q)

	rec := httptest.NewRecorder()
	h.JobDuplicate(rec, duplicateRequest(t, duplicateSourceID, `{"lang": "en"}`))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body: %s", rec.Code, rec.Body.String())
	}

	task := q.lastTask
	if task.Command != domain.TaskCommandDuplicateComic || task.SourceJobID != duplicateSourceID || task.Lang != "en" {
		t.Errorf("task = %+v, want duplicate_comic from %s in en", task, duplicateSourceID)
	}
	if task.JobID == duplicateSourceID || domain.ValidateJobID(task.JobID) != nil {
		t.Errorf("copy job ID = %q, want a newly minted one", task.JobID)
	}
	var resp enqueueResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil || resp.JobID != task.JobID {
		t.Errorf("response = %+v (err %v), want the copy's job ID so the caller can open it", resp, err)
	}
	if got := rec.Header().Get("Location"); got != "/jobs/"+task.JobID {
		t.Errorf("Location = %q, want the copy", got)
	}
}

func TestDuplicateAcceptsAnEmptyBodyAsTheDefaultLanguage(t *testing.T) {
	t.Parallel()
	q := &fakeTaskQueue{}
	rec := httptest.NewRecorder()
	handlerWithWork(t, q).JobDuplicate(rec, duplicateRequest(t, duplicateSourceID, ""))
	if rec.Code != http.StatusAccepted || q.lastTask.Lang != "" {
		t.Errorf("status = %d, lang = %q; want 202 with no language", rec.Code, q.lastTask.Lang)
	}
}

func TestDuplicateRefusesBeforeMintingAJob(t *testing.T) {
	t.Parallel()
	const missing = "c-20261125-000000-cccccccccccc"
	for name, tc := range map[string]struct {
		jobID, body string
		running     bool
		want        int
	}{
		"missing source":       {jobID: missing, body: `{"lang": "en"}`, want: http.StatusNotFound},
		"malformed source":     {jobID: "..", body: `{}`, want: http.StatusBadRequest},
		"unsupported language": {jobID: duplicateSourceID, body: `{"lang": "fr"}`, want: http.StatusBadRequest},
		"broken body":          {jobID: duplicateSourceID, body: `{`, want: http.StatusBadRequest},
		// A copy taken mid-generation would hold whichever panels happened to be finished.
		"source still running": {jobID: duplicateSourceID, body: `{"lang": "en"}`, running: true, want: http.StatusConflict},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			q := &fakeTaskQueue{}
			h := handlerWithWork(t, q)
			if tc.running {
				h.jobStatus = &fakeJobStatusStore{saved: []domain.JobStatus{{State: domain.JobStateRunning}}}
			}
			rec := httptest.NewRecorder()
			h.JobDuplicate(rec, duplicateRequest(t, tc.jobID, tc.body))
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d; body: %s", rec.Code, tc.want, rec.Body.String())
			}
			if q.called != 0 {
				t.Error("a copy was enqueued")
			}
		})
	}
}

func TestRegenerateRejectsDuplicateCommand(t *testing.T) {
	t.Parallel()
	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	// Sent to /regenerate, the copy would be written over the work named in the URL.
	rec := httptest.NewRecorder()
	h.JobRegenerate(rec, regenerateRequest(t, `{"command": "duplicate_comic", "source_job_id": "`+duplicateSourceID+`"}`))
	if rec.Code != http.StatusBadRequest || q.called != 0 {
		t.Errorf("status = %d, enqueued = %d; want 400 and nothing enqueued", rec.Code, q.called)
	}
}

func TestDetailPageShowsTheFormatAndOffersTheOtherLanguage(t *testing.T) {
	t.Parallel()
	h := newTestHandler(t, &fakeTaskQueue{})

	japanese := h.buildDetailData("job-ja", &kitcomic.MangaState{AspectRatio: "3:4", Cast: []string{"metan"}})
	if japanese.Lang != "ja" || japanese.OtherLang != "en" || japanese.AspectRatio != "3:4" || !slices.Equal(japanese.Cast, []string{"metan"}) {
		t.Errorf("a work that recorded no language = %+v, want it shown as ja with en on offer", japanese)
	}
	english := h.buildDetailData("job-en", &kitcomic.MangaState{Lang: "en"})
	if english.Lang != "en" || english.OtherLang != "ja" {
		t.Errorf("english work = lang %q, other %q; want en and ja", english.Lang, english.OtherLang)
	}
}
