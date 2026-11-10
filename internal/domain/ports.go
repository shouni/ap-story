package domain

import (
	"context"
	"errors"

	kitcomic "github.com/shouni/go-comic-kit/comic"
)

// ErrStateNotFound means the job has no state yet.
//
// It is a normal state before generation starts, or for a job that failed before saving its state.
// Handlers should map it to 404 (or to the "in progress" page).
var ErrStateNotFound = errors.New("comic state not found")

// ErrStateUnavailable means a state that "should exist could not be read".
//
// It is kept apart from ErrStateNotFound because the two call for opposite decisions.
// Treating missing permissions or a GCS outage as "missing" would show every work as
// "not there yet" for the whole outage and leave no clue to the cause.
// Handlers should map it to 502 (or 500) and log the cause.
var ErrStateUnavailable = errors.New("comic state unavailable")

// Pipeline is the execution surface that processes one Task to the end.
//
// The implementation is internal/pipeline's Runner, which only the worker face has. It stays nil
// on the web face, so the assembling side must declare the variable with this type (declaring it
// with the concrete pointer type and assigning makes the interface non-nil even when nil goes in).
type Pipeline interface {
	Execute(ctx context.Context, task Task) error
}

// TaskQueue abstracts the asynchronous queue.
type TaskQueue interface {
	Enqueue(ctx context.Context, task Task) error
}

// ComicRepository abstracts reading and deleting the history of works (jobs) and reading the
// generation history of shared character assets (design sheets).
// The details (GetState) return go-comic-kit's MangaState as is. ap-story has no domain type
// of its own corresponding to MusicRecipe, because the state itself is a complete representation.
type ComicRepository interface {
	ListHistoryPage(ctx context.Context, page int, perPage int) (ComicHistoryPage, error)
	GetState(ctx context.Context, jobID string) (*kitcomic.MangaState, error)
	// SaveState writes the whole state back. It is the entry for touching up the script; the
	// generation pipeline (worker) uses go-comic-kit's store directly and does not go through here.
	SaveState(ctx context.Context, jobID string, state *kitcomic.MangaState) error
	DeleteHistory(ctx context.Context, jobID string) error
	// ListCharacterDesignHistory returns the design-sheet generation history of one character,
	// newest first. Sheets generated for several characters together are excluded
	// (they are recorded only under character/{single character ID}/).
	ListCharacterDesignHistory(ctx context.Context, characterID string) ([]CharacterDesignHistoryItem, error)
	// DeleteCharacterDesign deletes one generation record of a character (character/{characterID}/{jobID}.ext).
	// When the job was a standalone design-sheet generation (no chapters), the matching
	// state (comics/{jobID}/) is deleted too. The state of a comic job is never deleted.
	DeleteCharacterDesign(ctx context.Context, characterID string, jobID string) error
}

// Notifier abstracts job completion and failure notices (Slack and so on).
// When notices are disabled, such as with no webhook configured, an implementation may skip
// silently without returning an error (a notice must never affect whether the job itself succeeds).
type Notifier interface {
	NotifyComplete(ctx context.Context, task Task) error
	NotifyError(ctx context.Context, task Task, cause error) error
}

// JobStatusStore persists job progress (queued/running/succeeded/failed).
// The web process writes the state at enqueue and the worker process writes the outcome.
//
// The Save / Get signatures match go-job-kit's jobstatus.StatusStore.
// That makes *jobstatus.Store[JobStatus] the implementation as is, with no adapter needed to
// pass it to jobstatus.Recorder (the job ID used to be part of the status, so the three
// services each kept an adapter that only reshaped it).
type JobStatusStore interface {
	// Save saves the job status.
	Save(ctx context.Context, jobID string, status JobStatus) error
	// Get reads the job status. It returns an error when nothing has been recorded.
	Get(ctx context.Context, jobID string) (JobStatus, error)
}
