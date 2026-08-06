package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func characterImageRequest(t *testing.T, wildcard string) *http.Request {
	t.Helper()
	req := httptestRequestWithURLParam(t, http.MethodGet, "/api/characters/images/"+wildcard, "", "unused", "")
	chi.RouteContext(req.Context()).URLParams.Add("*", wildcard)
	return req
}

func TestRedirectCharacterImageSuccess(t *testing.T) {
	t.Parallel()

	signer := &fakeSigner{}
	h := newTestHandlerFull(t, &fakeTaskQueue{}, &fakeComicRepository{}, signer)

	req := characterImageRequest(t, "zundamon/job-1.png")
	rec := httptest.NewRecorder()
	h.RedirectCharacterImage(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusFound, rec.Body.String())
	}
	wantURI := "gs://test-bucket/character/zundamon/job-1.png"
	if signer.lastObjectURI != wantURI {
		t.Errorf("signed object URI = %q, want %q", signer.lastObjectURI, wantURI)
	}
}

func TestRedirectCharacterImageRejectsPathTraversal(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := characterImageRequest(t, "../../../etc/passwd")
	rec := httptest.NewRecorder()
	h.RedirectCharacterImage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRedirectCharacterImageRejectsEmptyPath(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := characterImageRequest(t, "")
	rec := httptest.NewRecorder()
	h.RedirectCharacterImage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestResolveCharacterAssetPathBuildsExpectedPath(t *testing.T) {
	t.Parallel()

	got, err := resolveCharacterAssetPath(characterImagePrefix, "zundamon/job-1.png")
	if err != nil {
		t.Fatalf("resolveCharacterAssetPath failed: %v", err)
	}
	if got != "character/zundamon/job-1.png" {
		t.Errorf("path = %q, want character/zundamon/job-1.png", got)
	}
}

func TestResolveCharacterAssetPathBuildsReferencePath(t *testing.T) {
	t.Parallel()

	got, err := resolveCharacterAssetPath(characterReferenceImagePrefix, "zundamon/default.jpg")
	if err != nil {
		t.Fatalf("resolveCharacterAssetPath failed: %v", err)
	}
	if got != "character-reference/zundamon/default.jpg" {
		t.Errorf("path = %q, want character-reference/zundamon/default.jpg", got)
	}
}

func TestCharacterImageWebPathRejectsForeignURI(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	if got := h.characterImageWebPath("gs://other-bucket/character/zundamon/job-1.png"); got != "" {
		t.Errorf("foreign bucket URI = %q, want empty", got)
	}
	if got := h.characterImageWebPath("gs://test-bucket/comics/job-1/character/design_zundamon.png"); got != "" {
		t.Errorf("legacy job-scoped URI = %q, want empty", got)
	}
}
