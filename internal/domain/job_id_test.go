package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/shouni/go-utils/jobid"
)

// legacyJobID is an ID in the format minted up to v1.5.0 (2026-07-17 11:30:45 UTC).
// Past jobs stay in GCS artifact paths in this format.
const legacyJobID = "c20260717-113045-1a2b3c4d"

func TestNewJobIDFormat(t *testing.T) {
	t.Parallel()

	id, err := NewJobID()
	if err != nil {
		t.Fatalf("NewJobID failed: %v", err)
	}
	if err := ValidateJobID(id); err != nil {
		t.Errorf("NewJobID produced invalid ID %q: %v", id, err)
	}
	if !strings.HasPrefix(id, "c") {
		t.Errorf("id = %q, want prefix 'c'", id)
	}

	other, err := NewJobID()
	if err != nil {
		t.Fatalf("NewJobID failed: %v", err)
	}
	if id == other {
		t.Errorf("two NewJobID calls returned the same ID %q", id)
	}
}

// TestNewJobIDCarriesCreatedAt checks that the creation time can be recovered from a minted ID.
// The history list's dates and ordering depend on this recovery, so changing the ID format
// would make jobid.CreatedAt unreadable and the list's dates would silently go blank.
func TestNewJobIDCarriesCreatedAt(t *testing.T) {
	t.Parallel()

	before := time.Now().UTC().Truncate(time.Second)
	id, err := NewJobID()
	if err != nil {
		t.Fatalf("NewJobID failed: %v", err)
	}
	after := time.Now().UTC()

	createdAt, err := jobid.CreatedAt(id)
	if err != nil {
		t.Fatalf("jobid.CreatedAt(%q) failed: %v", id, err)
	}
	if createdAt.Before(before) || createdAt.After(after) {
		t.Errorf("jobid.CreatedAt(%q) = %v, want between %v and %v", id, createdAt, before, after)
	}
}

// TestNewJobIDSortsAgainstLegacyIDs checks that IDs minted in the new format sort by
// creation time, descending, even mixed with legacy IDs.
//
// Since minting moved to jobid.New, old and new separate in lexical order ("c-" < "c2", so
// new IDs always sort after legacy ones). History ordering depends on paging.LoadPage's sortKey (jobid.SortKey),
// and reverting to the default descending ID order would reorder the list silently, with no error.
func TestNewJobIDSortsAgainstLegacyIDs(t *testing.T) {
	t.Parallel()

	id, err := NewJobID()
	if err != nil {
		t.Fatalf("NewJobID failed: %v", err)
	}

	key, legacyKey := jobid.SortKey(id), jobid.SortKey(legacyJobID)
	if key == "" {
		t.Fatalf("jobid.SortKey(%q) is empty; the creation time cannot be recovered from the ID format", id)
	}
	if legacyKey == "" {
		t.Fatalf("jobid.SortKey(%q) is empty; legacy IDs can no longer be read", legacyJobID)
	}
	// A newly minted ID always has the later time.
	if key <= legacyKey {
		t.Errorf("SortKey(%q) = %q, want > SortKey(%q) = %q", id, key, legacyJobID, legacyKey)
	}
}

func TestValidateJobID(t *testing.T) {
	t.Parallel()

	// Underscores became allowed with the move to go-utils/jobid
	// (to match the rule that came from the sister projects). They are safe in both URLs and GCS paths.
	valid := []string{"c20260717-113045-1a2b3c4d", "abc", "ABC-123", "a_b"}
	for _, id := range valid {
		if err := ValidateJobID(id); err != nil {
			t.Errorf("ValidateJobID(%q) = %v, want nil", id, err)
		}
	}

	// Leading hyphens or underscores and more than 128 characters are newly rejected by the unified rule.
	invalid := []string{"", "a/b", "../etc", "a b", "日本語", "a.b",
		"-leading", "_leading", strings.Repeat("a", 129)}
	for _, id := range invalid {
		if err := ValidateJobID(id); err == nil {
			t.Errorf("ValidateJobID(%q) = nil, want error", id)
		}
	}
}

func TestSanitizeJobID(t *testing.T) {
	t.Parallel()

	// Path-like values are normalised by Base
	got, err := SanitizeJobID("comics/c123-abc")
	if err != nil || got != "c123-abc" {
		t.Errorf("SanitizeJobID = %q, %v; want c123-abc", got, err)
	}

	// Traversal is rejected by the validation after normalisation
	for _, id := range []string{"../..", "a/../..", "/", "."} {
		if _, err := SanitizeJobID(id); err == nil {
			t.Errorf("SanitizeJobID(%q) = nil error, want rejection", id)
		}
	}
}

func TestJobOutputDir(t *testing.T) {
	t.Parallel()

	got, err := JobOutputDir("my-bucket", "c123-abc")
	if err != nil || got != "gs://my-bucket/comics/c123-abc" {
		t.Errorf("JobOutputDir = %q, %v; want gs://my-bucket/comics/c123-abc", got, err)
	}

	if _, err := JobOutputDir("my-bucket", "../.."); err == nil {
		t.Error("JobOutputDir(../..) succeeded, want error")
	}
}

func TestJobObjectPrefix(t *testing.T) {
	t.Parallel()

	got, err := JobObjectPrefix("c123-abc")
	if err != nil || got != "comics/c123-abc" {
		t.Errorf("JobObjectPrefix = %q, %v; want comics/c123-abc", got, err)
	}

	// Path-like values are neutralised by Base (no traversal remains in the result)
	got, err = JobObjectPrefix("../escape")
	if err != nil || got != "comics/escape" {
		t.Errorf("JobObjectPrefix(../escape) = %q, %v; want comics/escape (sanitized)", got, err)
	}

	// Values that are invalid even after neutralising are rejected
	if _, err := JobObjectPrefix("../.."); err == nil {
		t.Error("JobObjectPrefix(../..) succeeded, want error")
	}
}
