package repository

import (
	"testing"
	"time"
)

func TestJobIDFromStatePath(t *testing.T) {
	t.Parallel()

	prefix := "gs://bucket/comics"
	cases := []struct {
		name string
		path string
		want string
	}{
		{"valid state path", "gs://bucket/comics/job-1/comic_state.json", "job-1"},
		{"non-state file ignored", "gs://bucket/comics/job-1/images/panel_1.png", ""},
		{"nested too deep ignored", "gs://bucket/comics/job-1/sub/comic_state.json", ""},
		{"outside prefix ignored", "gs://other-bucket/comics/job-1/comic_state.json", ""},
		{"prefix itself ignored", "gs://bucket/comics/comic_state.json", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := jobIDFromStatePath(tt.path, prefix); got != tt.want {
				t.Errorf("jobIDFromStatePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestFormatTime(t *testing.T) {
	t.Parallel()

	if got := formatTime(time.Time{}); got != "" {
		t.Errorf("formatTime(zero) = %q, want empty", got)
	}

	tm := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if got := formatTime(tm); got != "2026-07-17T12:00:00Z" {
		t.Errorf("formatTime = %q, want RFC3339", got)
	}
}
