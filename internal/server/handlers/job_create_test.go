package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shouni/ap-story/internal/adapters/prompts"
	"github.com/shouni/ap-story/internal/domain"
)

func TestEnqueueComicSuccess(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	body := `{"source_text": "元文章", "script_mode": "default"}`
	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.createComicJSON(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if q.called != 1 {
		t.Fatalf("Enqueue called %d times, want 1", q.called)
	}
	if q.lastTask.Command != domain.TaskCommandComposeComic {
		t.Errorf("Command = %q, want %q", q.lastTask.Command, domain.TaskCommandComposeComic)
	}
	if q.lastTask.SourceText != "元文章" {
		t.Errorf("SourceText = %q, want %q", q.lastTask.SourceText, "元文章")
	}
	if err := domain.ValidateJobID(q.lastTask.JobID); err != nil {
		t.Errorf("generated JobID %q is invalid: %v", q.lastTask.JobID, err)
	}

	var resp enqueueResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != "queued" || resp.JobID != q.lastTask.JobID {
		t.Errorf("response = %+v, want status=queued job_id=%s", resp, q.lastTask.JobID)
	}
}

func TestEnqueueComicRejectsMissingSource(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()

	h.createComicJSON(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if q.called != 0 {
		t.Error("Enqueue was called despite invalid submission")
	}
}

func TestEnqueueComicRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(`not json`))
	rec := httptest.NewRecorder()

	h.createComicJSON(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestEnqueueComicReturns500OnQueueFailure(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{err: context.DeadlineExceeded}
	h := newTestHandler(t, q)

	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(`{"source_text": "x"}`))
	rec := httptest.NewRecorder()

	h.createComicJSON(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestNewHandlerRequiresTaskQueue(t *testing.T) {
	t.Parallel()

	if _, err := NewHandler(HandlerOptions{Repository: &fakeComicRepository{}}); err == nil {
		t.Error("NewHandler without TaskQueue succeeded, want error")
	}
}

func TestNewHandlerRequiresRepository(t *testing.T) {
	t.Parallel()

	if _, err := NewHandler(HandlerOptions{TaskQueue: &fakeTaskQueue{}}); err == nil {
		t.Error("NewHandler without Repository succeeded, want error")
	}
}

func TestNewHandlerRequiresSigner(t *testing.T) {
	t.Parallel()

	opts := HandlerOptions{TaskQueue: &fakeTaskQueue{}, Repository: &fakeComicRepository{}, Bucket: "b"}
	if _, err := NewHandler(opts); err == nil {
		t.Error("NewHandler without Signer succeeded, want error")
	}
}

func TestNewHandlerRequiresBucket(t *testing.T) {
	t.Parallel()

	opts := HandlerOptions{TaskQueue: &fakeTaskQueue{}, Repository: &fakeComicRepository{}, Signer: &fakeSigner{}}
	if _, err := NewHandler(opts); err == nil {
		t.Error("NewHandler without Bucket succeeded, want error")
	}
}

// The chosen model and art style are carried to the Task. The image model uses the existing ModelOverride
// (it is the same field as for design sheets, so the Task does not get a second image model).
func TestEnqueueComicCarriesModelAndModeChoices(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	body := `{"source_text": "元文章", "script_mode": "alt", "style_mode": "watercolor",
		"text_model": "gemini-alt", "image_model": "image-alt"}`
	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.createComicJSON(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	task := q.lastTask
	if task.ScriptMode != "alt" || task.StyleMode != "watercolor" {
		t.Errorf("modes = %q/%q, want alt/watercolor", task.ScriptMode, task.StyleMode)
	}
	if task.TextModel != "gemini-alt" || task.ModelOverride != "image-alt" {
		t.Errorf("models = %q/%q, want gemini-alt/image-alt", task.TextModel, task.ModelOverride)
	}
}

// A choice outside the allowlist is rejected before enqueue. A browser is restricted by the <select>, but
// the JSON API can send any string.
func TestEnqueueComicRejectsUnknownChoices(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"script mode": `{"source_text": "x", "script_mode": "no-such-mode"}`,
		"style mode":  `{"source_text": "x", "style_mode": "no-such-style"}`,
		"text model":  `{"source_text": "x", "text_model": "no-such-model"}`,
		"image model": `{"source_text": "x", "image_model": "no-such-model"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			q := &fakeTaskQueue{}
			h := newTestHandler(t, q)

			req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(body))
			rec := httptest.NewRecorder()

			h.createComicJSON(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if q.called != 0 {
				t.Errorf("Enqueue called %d times, want 0", q.called)
			}
		})
	}
}

// A JSON call with no model (MCP and the like) also yields a Task filled with the default model.
// Falling to the worker's configuration would leave that work alone without its provenance in the state.
func TestEnqueueComicFillsInDefaultModels(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(`{"source_text": "元文章"}`))
	rec := httptest.NewRecorder()

	h.createComicJSON(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if q.lastTask.TextModel != "gemini-model" {
		t.Errorf("TextModel = %q, want gemini-model (the head of GEMINI_MODELS)", q.lastTask.TextModel)
	}
	if q.lastTask.ModelOverride != "image-model" {
		t.Errorf("ModelOverride = %q, want image-model (the head of IMAGE_MODELS)", q.lastTask.ModelOverride)
	}
}

// There is an endpoint for looking up the options. A browser can choose by looking at the <select>, but a
// JSON API caller has no way to know what can be specified, and sending blind gives a 400.
func TestComicOptionsListsChoices(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})

	rec := httptest.NewRecorder()
	h.ComicOptions(rec, httptest.NewRequest(http.MethodGet, "/comic-options", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp comicOptionsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.ScriptModes) == 0 || len(resp.StyleModes) == 0 {
		t.Fatalf("modes = %+v/%+v, want both listed", resp.ScriptModes, resp.StyleModes)
	}
	// The head is the default model, in the same order as the form.
	if len(resp.TextModels) == 0 || resp.TextModels[0] != "gemini-model" {
		t.Errorf("text models = %v, want gemini-model first", resp.TextModels)
	}
	if len(resp.ImageModels) == 0 || resp.ImageModels[0] != "image-model" {
		t.Errorf("image models = %v, want image-model first", resp.ImageModels)
	}
	// A name alone is not enough to choose; the same values as the allowlist at enqueue are returned as names.
	if resp.StyleModes[0].Name != prompts.ModeDefault {
		t.Errorf("style modes[0] = %q, want %q", resp.StyleModes[0].Name, prompts.ModeDefault)
	}
}

// The job status is written before the enqueue.
// The worker reads the status before the delivered task and skips it as a redelivery if it is succeeded.
// Cloud Tasks delivers within tens of milliseconds, so in the reverse order the next operation on the same
// work would read the previous succeeded and be silently dropped (this actually happened).
func TestEnqueueRecordsQueuedStatusBeforeEnqueueing(t *testing.T) {
	t.Parallel()

	var order []string
	status := &fakeJobStatusStore{onSave: func() { order = append(order, "status") }}
	q := &fakeTaskQueue{onEnqueue: func() { order = append(order, "enqueue") }}
	h := newTestHandlerWithJobStatus(t, q, status)

	req := httptest.NewRequest(http.MethodPost, "/jobs", bytes.NewBufferString(`{"source_text": "元文章"}`))
	rec := httptest.NewRecorder()
	h.createComicJSON(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if len(order) != 2 || order[0] != "status" || order[1] != "enqueue" {
		t.Errorf("order = %v, want [status enqueue]", order)
	}
}
