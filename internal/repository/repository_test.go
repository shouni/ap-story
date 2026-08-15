package repository

import (
	"testing"
	"time"
)

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
