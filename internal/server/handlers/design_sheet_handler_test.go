package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDesignSheetFormRendersCharacterList(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := httptest.NewRequest(http.MethodGet, "/design-sheets", nil)
	rec := httptest.NewRecorder()

	h.DesignSheetForm(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{"zundamon", "metan", `name="character_ids"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestDesignSheetFormRendersModelOptions(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := httptest.NewRequest(http.MethodGet, "/design-sheets", nil)
	rec := httptest.NewRecorder()

	h.DesignSheetForm(rec, req)

	body := rec.Body.String()
	for _, want := range []string{`name="model_override"`, "quality-model", "standard-model"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func postDesignSheetForm(t *testing.T, h *Handler, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/design-sheets", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.EnqueueDesignSheetForm(rec, req)
	return rec
}

func TestEnqueueDesignSheetFormAcceptsSelection(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	values := url.Values{}
	values.Add("character_ids", "zundamon")
	values.Add("character_ids", "metan")
	values.Set("aspect_ratio", "1:1")
	rec := postDesignSheetForm(t, h, values)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if q.called != 1 {
		t.Fatalf("enqueue called %d times, want 1", q.called)
	}
	if q.lastTask.JobID == "" {
		t.Error("job id was not auto-generated")
	}
	if len(q.lastTask.CharacterIDs) != 2 || q.lastTask.AspectRatio != "1:1" {
		t.Errorf("task = %+v, want 2 character ids and aspect_ratio 1:1", q.lastTask)
	}
}

func TestEnqueueDesignSheetFormAcceptsReferenceOverride(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	values := url.Values{}
	values.Add("character_ids", "zundamon")
	values.Set("reference_url_override", "https://example.com/ref.png")
	values.Set("visual_cues_override", "赤いマフラー, 片方だけ髪留め\n新しい帽子")
	rec := postDesignSheetForm(t, h, values)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if q.lastTask.ReferenceURLOverride != "https://example.com/ref.png" {
		t.Errorf("ReferenceURLOverride = %q, want the submitted URL", q.lastTask.ReferenceURLOverride)
	}
	want := []string{"赤いマフラー", "片方だけ髪留め", "新しい帽子"}
	if len(q.lastTask.VisualCuesOverride) != len(want) {
		t.Fatalf("VisualCuesOverride = %+v, want %+v", q.lastTask.VisualCuesOverride, want)
	}
	for i, w := range want {
		if q.lastTask.VisualCuesOverride[i] != w {
			t.Errorf("VisualCuesOverride[%d] = %q, want %q", i, q.lastTask.VisualCuesOverride[i], w)
		}
	}
}

func TestSplitVisualCuesTrimsAndFiltersEmpty(t *testing.T) {
	t.Parallel()

	got := splitVisualCues(" 赤いマフラー ,, \n 片方だけ髪留め\n\n")
	want := []string{"赤いマフラー", "片方だけ髪留め"}
	if len(got) != len(want) {
		t.Fatalf("got = %+v, want %+v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestEnqueueDesignSheetFormAcceptsModelOverride(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	values := url.Values{}
	values.Add("character_ids", "zundamon")
	values.Set("model_override", "standard-model")
	rec := postDesignSheetForm(t, h, values)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if q.lastTask.ModelOverride != "standard-model" {
		t.Errorf("ModelOverride = %q, want %q", q.lastTask.ModelOverride, "standard-model")
	}
}

func TestEnqueueDesignSheetFormDefaultsModelOverrideToEmpty(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	values := url.Values{}
	values.Add("character_ids", "zundamon")
	rec := postDesignSheetForm(t, h, values)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if q.lastTask.ModelOverride != "" {
		t.Errorf("ModelOverride = %q, want empty (use default)", q.lastTask.ModelOverride)
	}
}

func TestEnqueueDesignSheetFormRejectsNoCharacters(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	rec := postDesignSheetForm(t, h, url.Values{})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if q.called != 0 {
		t.Errorf("enqueue called %d times, want 0", q.called)
	}
}
