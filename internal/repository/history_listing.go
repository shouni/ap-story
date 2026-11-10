package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	kitcomic "github.com/shouni/go-comic-kit/comic"
	kitports "github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/go-comic-kit/store"
	"github.com/shouni/go-job-kit/joblist"
	"github.com/shouni/go-job-kit/paging"
	"github.com/shouni/go-utils/jobid"

	"github.com/shouni/ap-story/internal/domain"
)

const comicsPrefix = "comics"

// historyLoadConcurrency is the concurrency for loading one page's worth of states.
const historyLoadConcurrency = 10

// ListHistoryPage fetches the history list with paging.
// /history covers only actual comic works. The state of a standalone design-sheet job is saved
// under design-jobs/, so it does not appear in the comics/ listing, and listing and paging IDs
// completes without reading any state (only the selected page is loaded).
// States with no chapters (standalone jobs left in comics/ in the legacy format, or the fallback
// value of a failed read) are excluded from display just in case.
func (r *ComicRepository) ListHistoryPage(ctx context.Context, page int, perPage int) (domain.ComicHistoryPage, error) {
	jobIDs, err := r.listJobIDs(ctx)
	if err != nil {
		return domain.ComicHistoryPage{}, fmt.Errorf("failed to list the GCS history: %w", err)
	}

	// Sort by the creation time embedded in the ID. The default descending ID order cannot be relied on
	// because the ID format moved from "c{time}-{random}" to jobid.New's "c-{time}-{random}".
	// Lexically "c-" < "c2", so new IDs always sort after legacy ones.
	all, meta, err := paging.LoadPage(ctx, jobIDs, page, perPage, jobid.SortKey, r.loadHistoryOrFallback,
		paging.WithConcurrency(historyLoadConcurrency),
	)
	if err != nil {
		return domain.ComicHistoryPage{}, fmt.Errorf("failed to load the GCS history: %w", err)
	}

	histories := make([]domain.ComicHistory, 0, len(all))
	for _, h := range all {
		if h.ChapterCount == 0 {
			continue
		}
		histories = append(histories, h)
	}

	return domain.ComicHistoryPage{
		Items:    histories,
		PageMeta: paging.AdjustItemCount(meta, len(histories)),
	}, nil
}

// jobIDListCacheKey is the cache key of the job ID list.
// Only one kind is listed (directly under comics/), so a fixed key is enough.
const jobIDListCacheKey = "job-ids"

// listJobIDs collects job IDs from the job directories directly under comics/.
// It scans the whole bucket, so a short-TTL cache sits in front of it.
//
// The scan itself is joblist's job (with a delimiter, so each job comes back as one entry.
// Without it every artifact underneath (the panel and page images in images/) would come back,
// and the caller would have to collapse duplicate job IDs).
//
// Only directory names are picked up, so jobs without comic_state.json (generation that failed
// before saving the state) appear as IDs too. Their state cannot be read and they become the
// fallback value with ChapterCount == 0, so ListHistoryPage's chapter filter drops them from
// display.
func (r *ComicRepository) listJobIDs(ctx context.Context) ([]string, error) {
	return r.jobIDCache.Load(ctx, jobIDListCacheKey, func(ctx context.Context) ([]string, error) {
		return joblist.Collect(ctx, r.store, remoteio.BuildURI(remoteio.SchemeGCS, r.bucket, comicsPrefix))
	})
}

// invalidateJobIDList discards the list cache so that a job deletion shows up immediately.
func (r *ComicRepository) invalidateJobIDList() {
	r.jobIDCache.Invalidate(jobIDListCacheKey)
}

// loadHistoryOrFallback loads one history summary.
//
// Even when reading the state fails, the entry is not dropped from the list; a fallback value with only
// the job ID and title is returned (with ChapterCount == 0, so the /history filter naturally excludes it).
// LoadPage removes from the list any ID for which load returned an error, so anything to keep must be
// returned as a substitute value, not an error.
//
// Context cancellation and timeouts do not fall back. So that a list filled entirely with
// substitute values is not returned as "a normal result", the error goes to LoadPage as is and
// propagates to the caller.
func (r *ComicRepository) loadHistoryOrFallback(ctx context.Context, jobID string) (domain.ComicHistory, error) {
	history, err := r.buildHistory(ctx, jobID)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return domain.ComicHistory{}, ctxErr
		}
		slog.WarnContext(ctx, "failed to load state for history list",
			"job_id", jobID,
			"error", err,
		)
		return domain.ComicHistory{JobID: jobID, Title: jobID}, nil
	}
	return history, nil
}

func (r *ComicRepository) buildHistory(ctx context.Context, jobID string) (domain.ComicHistory, error) {
	if history, ok := r.getCachedHistory(jobID); ok {
		return history, nil
	}

	state, err := r.GetState(ctx, jobID)
	if err != nil {
		return domain.ComicHistory{}, err
	}

	history := historyFromState(jobID, state)
	r.setCachedHistory(jobID, history)
	return history, nil
}

func historyFromState(jobID string, state *kitcomic.MangaState) domain.ComicHistory {
	title := strings.TrimSpace(state.Title)
	if title == "" {
		title = jobID
	}
	return domain.ComicHistory{
		JobID:        jobID,
		Title:        title,
		StyleMode:    state.StyleMode,
		ChapterCount: len(state.Chapters),
		PanelCount:   len(state.Panels),
		PageCount:    len(state.Pages),
		CreatedAt:    formatTime(state.CreatedAt),
		UpdatedAt:    formatTime(state.UpdatedAt),
	}
}

// GetState fetches the MangaState of the given job.
//
// It returns domain.ErrStateNotFound when there is no state yet, and domain.ErrStateUnavailable when
// one should exist but could not be read. The split uses go-comic-kit's sentinel returned by store.Load
// (ports.ErrNotFound), so it adds no round trip to storage.
//
// Both wrap the causing error. Everything used to be collapsed into "not found" with the cause
// discarded, so a permission error and a GCS outage alike showed on screen as 「作品がありません」,
// with no reason in the logs.
func (r *ComicRepository) GetState(ctx context.Context, jobID string) (*kitcomic.MangaState, error) {
	if err := domain.ValidateJobID(jobID); err != nil {
		return nil, err
	}
	statePath, err := r.stateObjectPath(jobID)
	if err != nil {
		return nil, err
	}
	state, err := store.Load(ctx, r.store, statePath)
	if err != nil {
		// Since go-comic-kit v1.6.0, store.Load classifies failures with the ports sentinels.
		// "Not there yet" is ports.ErrNotFound, and the underlying os.ErrNotExist is not wrapped.
		// A broken state (ports.ErrInvalidRequest) goes to the "exists but unreadable" side.
		if errors.Is(err, kitports.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w (job %q): %w", domain.ErrStateNotFound, jobID, err)
		}
		return nil, fmt.Errorf("%w (job %q): %w", domain.ErrStateUnavailable, jobID, err)
	}
	return state, nil
}
