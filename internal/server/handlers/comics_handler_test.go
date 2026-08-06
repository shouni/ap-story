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

func TestEnqueueComicSuccess(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	body := `{"source_text": "元文章", "script_mode": "default"}`
	req := httptest.NewRequest(http.MethodPost, "/api/comics", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.EnqueueComic(rec, req)

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
		t.Errorf("SourceText = %q, want 元文章", q.lastTask.SourceText)
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

	req := httptest.NewRequest(http.MethodPost, "/api/comics", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()

	h.EnqueueComic(rec, req)

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

	req := httptest.NewRequest(http.MethodPost, "/api/comics", bytes.NewBufferString(`not json`))
	rec := httptest.NewRecorder()

	h.EnqueueComic(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestEnqueueComicRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	req := httptest.NewRequest(http.MethodGet, "/api/comics", nil)
	rec := httptest.NewRecorder()

	h.EnqueueComic(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestEnqueueComicReturns500OnQueueFailure(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{err: context.DeadlineExceeded}
	h := newTestHandler(t, q)

	req := httptest.NewRequest(http.MethodPost, "/api/comics", bytes.NewBufferString(`{"source_text": "x"}`))
	rec := httptest.NewRecorder()

	h.EnqueueComic(rec, req)

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
