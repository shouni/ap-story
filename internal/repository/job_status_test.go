package repository

import (
	"context"
	"testing"

	"github.com/shouni/go-job-kit/jobstatus"

	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
)

// Checking the storage format itself (unrecorded, broken JSON, path traversal, overwrites) lives
// in go-job-kit's jobstatus. This looks only at what is specific to ap-story.

const statusTestJobID = "c20260726-120000-abcd1234"

func newStatusRepo(store *memStore) *jobstatus.Store[domain.JobStatus] {
	return NewJobStatusRepository(config.StorageConfig{GCSBucket: "bucket"}, store)
}

// The status sits under comics/{jobID}/ with the artifacts, so deleting the history (a prefix delete)
// cleans it up automatically. The history list picks up only comic_state.json, so it does not mix in.
func TestSaveWritesInsideJobDirectory(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	repo := newStatusRepo(store)

	err := repo.Save(context.Background(), statusTestJobID, domain.JobStatus{
		JobID:   statusTestJobID,
		Command: "compose_comic",
		State:   domain.JobStateQueued,
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	want := "gs://bucket/comics/" + statusTestJobID + "/status.json"
	if !store.has(want) {
		t.Fatalf("status.json was not written to %q; written: %v", want, keysOf(store))
	}
}

// The ap-story-specific field (OutputDir) makes the round trip.
// It also checks that embedding jobstatus.Status keeps the JSON flat.
func TestSaveAndGetRoundTrip(t *testing.T) {
	t.Parallel()

	repo := newStatusRepo(newMemStore())
	original := domain.JobStatus{
		JobID:     statusTestJobID,
		Command:   "compose_comic",
		State:     domain.JobStateSucceeded,
		Title:     "テスト作品",
		Attempts:  3,
		OutputDir: "gs://bucket/comics/" + statusTestJobID,
	}
	if err := repo.Save(context.Background(), original.JobID, original); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := repo.Get(context.Background(), statusTestJobID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != domain.JobStateSucceeded {
		t.Errorf("State = %q", got.State)
	}
	if got.Title != "テスト作品" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", got.Attempts)
	}
	if got.OutputDir != original.OutputDir {
		t.Errorf("OutputDir = %q", got.OutputDir)
	}
	// It is always stamped on the Save side (so the record exists even if the caller forgets to set it).
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt is not set")
	}
}

// keysOf returns the paths written to memStore.
func keysOf(store *memStore) []string { return store.h.URIs() }
