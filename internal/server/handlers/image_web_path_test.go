package handlers

import "testing"

func TestComicImageWebPathRejectsForeignURI(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	if got := h.comicImageWebPath("job-1", "gs://other-bucket/comics/job-1/images/x.png"); got != "" {
		t.Errorf("foreign bucket URI = %q, want empty", got)
	}
	if got := h.comicImageWebPath("job-1", "gs://test-bucket/comics/job-2/images/x.png"); got != "" {
		t.Errorf("foreign job URI = %q, want empty", got)
	}
}
