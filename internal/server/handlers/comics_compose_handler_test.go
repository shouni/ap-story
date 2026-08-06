package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/shouni/ap-story/internal/domain"
)

func TestComposeFormRenders(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := httptest.NewRequest(http.MethodGet, "/compose", nil)
	rec := httptest.NewRecorder()

	h.ComposeForm(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `name="source_text"`) {
		t.Errorf("body missing source_text field")
	}
}

func postComposeForm(t *testing.T, h *Handler, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/compose", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.EnqueueComicForm(rec, req)
	return rec
}

func TestEnqueueComicFormAcceptsValidSubmission(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	rec := postComposeForm(t, h, url.Values{"source_text": {"元文章"}, "script_mode": {"default"}})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if q.called != 1 {
		t.Errorf("enqueue called %d times, want 1", q.called)
	}
	if q.lastTask.Command != domain.TaskCommandComposeComic || q.lastTask.SourceText != "元文章" {
		t.Errorf("task = %+v, want compose_comic with source text", q.lastTask)
	}
	if !strings.Contains(rec.Body.String(), q.lastTask.JobID) {
		t.Errorf("accepted page does not contain job id %q", q.lastTask.JobID)
	}
}

func TestEnqueueComicFormRejectsMissingSource(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	rec := postComposeForm(t, h, url.Values{})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if q.called != 0 {
		t.Errorf("enqueue called %d times, want 0", q.called)
	}
	if !strings.Contains(rec.Body.String(), "source_url") {
		t.Errorf("error page should mention missing source input, got: %s", rec.Body.String())
	}
}

func TestEnqueueComicFormPassesStopAfterScript(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	rec := postComposeForm(t, h, url.Values{
		"source_text":       {"元文章"},
		"stop_after_script": {"1"},
	})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if !q.lastTask.StopAfterScript {
		t.Error("チェックボックスを入れたのに StopAfterScript が false")
	}
}

func TestEnqueueComicFormWithoutStopAfterScript(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	rec := postComposeForm(t, h, url.Values{"source_text": {"元文章"}})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if q.lastTask.StopAfterScript {
		t.Error("チェックボックス未指定なのに StopAfterScript が true")
	}
}

func TestComposeFormRendersStopAfterScriptCheckbox(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := httptest.NewRequest(http.MethodGet, "/compose", nil)
	rec := httptest.NewRecorder()

	h.ComposeForm(rec, req)

	if !strings.Contains(rec.Body.String(), `name="stop_after_script"`) {
		t.Error("台本ゲートのチェックボックスがフォームに無い")
	}
}

// scriptOnlyState は stop_after_script 直後の state（台本のみ、画像なし）を返します。
