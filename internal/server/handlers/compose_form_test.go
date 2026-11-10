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
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.createComicForm(rec, req)
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

// This form only makes the script. When it is pressed, the outline has not run yet and
// nobody knows how many panels there will be. Images start from the work's detail page.
func TestEnqueueComicFormAlwaysStopsAfterScript(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	// A crafted attempt to run through to the images (a field not in the form) must have no effect.
	rec := postComposeForm(t, h, url.Values{
		"source_text":       {"元文章"},
		"stop_after_script": {""},
	})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if !q.lastTask.StopAfterScript {
		t.Error("a form submission is set to run through to image generation")
	}
}

func TestComposeFormHasNoWholeRunControl(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := httptest.NewRequest(http.MethodGet, "/compose", nil)
	rec := httptest.NewRecorder()

	h.ComposeForm(rec, req)

	if strings.Contains(rec.Body.String(), `name="stop_after_script"`) {
		t.Error("a control choosing between script-only and through-images remains (this form is script-only)")
	}
}

// The form shows pickers for the script mode, style mode, text model and image model.
// If the options disappeared, operation would carry on without anyone noticing that only the defaults can be generated.
func TestComposeFormRendersChoiceSelects(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})

	req := httptest.NewRequest(http.MethodGet, "/compose", nil)
	rec := httptest.NewRecorder()
	h.ComposeForm(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`name="script_mode"`, `name="style_mode"`, `name="text_model"`,
		`value="watercolor"`, `value="gemini-alt"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("compose form is missing %q", want)
		}
	}
	// The image model is not chosen here. At the script stage neither the panel count nor the look is known,
	// so it is chosen on the work's detail page (the page that starts image generation).
	if strings.Contains(body, `name="image_model"`) {
		t.Error("an image model picker remains on the script-generation form")
	}
}

// The submitted choices are carried from the form input to the task.
func TestEnqueueComicFormCarriesChoices(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	rec := postComposeForm(t, h, url.Values{
		"source_text": {"元文章"},
		"script_mode": {"alt"},
		"style_mode":  {"watercolor"},
		"text_model":  {"gemini-alt"},
	})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	task := q.lastTask
	if task.ScriptMode != "alt" || task.StyleMode != "watercolor" {
		t.Errorf("modes = %q/%q, want alt/watercolor", task.ScriptMode, task.StyleMode)
	}
	if task.TextModel != "gemini-alt" {
		t.Errorf("text model = %q, want gemini-alt", task.TextModel)
	}
}

// The model pickers have no empty "use the default" option. A browser always sends a concrete name,
// so that the state's record shows which model made the work.
func TestComposeFormAlwaysSubmitsAModel(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})

	req := httptest.NewRequest(http.MethodGet, "/compose", nil)
	rec := httptest.NewRecorder()
	h.ComposeForm(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, `<option value=""`) {
		t.Errorf("an empty option remains: %s", body)
	}
	// When nothing is chosen, the head (= the default model) is shown selected.
	for _, want := range []string{
		`<option value="gemini-model" selected>既定: gemini-model</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the default model is not shown selected: want %q", want)
		}
	}
}
