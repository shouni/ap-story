package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func imageRequest(t *testing.T, jobID, wildcard string) *http.Request {
	t.Helper()
	req := httptestRequestWithURLParam(t, http.MethodGet, "/jobs/"+jobID+"/images/"+wildcard, "", "jobID", jobID)
	chi.RouteContext(req.Context()).URLParams.Add("*", wildcard)
	return req
}

func TestRedirectComicImageSuccess(t *testing.T) {
	t.Parallel()

	signer := &fakeSigner{}
	h := newTestHandlerFull(t, &fakeTaskQueue{}, &fakeComicRepository{}, signer)

	req := imageRequest(t, "job-1", "images/panel_ch01-p01.png")
	rec := httptest.NewRecorder()
	h.JobImage(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusFound, rec.Body.String())
	}
	wantURI := "gs://test-bucket/comics/job-1/images/panel_ch01-p01.png"
	if signer.lastObjectURI != wantURI {
		t.Errorf("signed object URI = %q, want %q", signer.lastObjectURI, wantURI)
	}
	if loc := rec.Header().Get("Location"); loc == "" {
		t.Error("Location header not set")
	}
}

func TestRedirectComicImageRejectsInvalidJobID(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := imageRequest(t, "../escape", "images/panel_1.png")
	rec := httptest.NewRecorder()
	h.JobImage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRedirectComicImageRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := imageRequest(t, "job-1", "../../../etc/passwd")
	rec := httptest.NewRecorder()
	h.JobImage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRedirectComicImageRejectsEmptyPath(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := imageRequest(t, "job-1", "")
	rec := httptest.NewRecorder()
	h.JobImage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRedirectComicImageReturns500OnSignerFailure(t *testing.T) {
	t.Parallel()

	signer := &fakeSigner{err: errImagePathRequired}
	h := newTestHandlerFull(t, &fakeTaskQueue{}, &fakeComicRepository{}, signer)

	req := imageRequest(t, "job-1", "images/panel_1.png")
	rec := httptest.NewRecorder()
	h.JobImage(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestResolveComicImagePathRejectsTraversal(t *testing.T) {
	t.Parallel()

	if _, err := resolveComicImagePath("job-1", "../escape"); err == nil {
		t.Error("resolveComicImagePath with traversal succeeded, want error")
	}
}

func TestResolveComicImagePathBuildsExpectedPath(t *testing.T) {
	t.Parallel()

	got, err := resolveComicImagePath("job-1", "images/panel_1.png")
	if err != nil {
		t.Fatalf("resolveComicImagePath failed: %v", err)
	}
	if got != "comics/job-1/images/panel_1.png" {
		t.Errorf("path = %q, want comics/job-1/images/panel_1.png", got)
	}
}
