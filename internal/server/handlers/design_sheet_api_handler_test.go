package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shouni/ap-story/internal/domain"
)

func TestEnqueueDesignSheetSuccess(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	body := `{"character_ids": ["zundamon"], "aspect_ratio": "1:1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/design-sheets", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.EnqueueDesignSheet(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if q.called != 1 {
		t.Fatalf("Enqueue called %d times, want 1", q.called)
	}
	if q.lastTask.Command != domain.TaskCommandGenerateDesignSheet {
		t.Errorf("Command = %q, want %q", q.lastTask.Command, domain.TaskCommandGenerateDesignSheet)
	}
	if len(q.lastTask.CharacterIDs) != 1 || q.lastTask.CharacterIDs[0] != "zundamon" {
		t.Errorf("CharacterIDs = %v, want [zundamon]", q.lastTask.CharacterIDs)
	}
	if q.lastTask.AspectRatio != "1:1" {
		t.Errorf("AspectRatio = %q, want 1:1", q.lastTask.AspectRatio)
	}

	var resp enqueueResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != "queued" || resp.JobID != q.lastTask.JobID {
		t.Errorf("response = %+v, want status=queued job_id=%s", resp, q.lastTask.JobID)
	}
}

func TestEnqueueDesignSheetRejectsMissingCharacterIDs(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	req := httptest.NewRequest(http.MethodPost, "/api/design-sheets", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()

	h.EnqueueDesignSheet(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if q.called != 0 {
		t.Error("Enqueue was called despite invalid submission")
	}
}

func TestEnqueueDesignSheetRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := httptest.NewRequest(http.MethodPost, "/api/design-sheets", bytes.NewBufferString(`not json`))
	rec := httptest.NewRecorder()

	h.EnqueueDesignSheet(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestEnqueueDesignSheetRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := httptest.NewRequest(http.MethodGet, "/api/design-sheets", nil)
	rec := httptest.NewRecorder()

	h.EnqueueDesignSheet(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestEnqueueDesignSheetReturns500OnQueueFailure(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{err: context.DeadlineExceeded}
	h := newTestHandler(t, q)

	req := httptest.NewRequest(http.MethodPost, "/api/design-sheets", bytes.NewBufferString(`{"character_ids": ["zundamon"]}`))
	rec := httptest.NewRecorder()

	h.EnqueueDesignSheet(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
