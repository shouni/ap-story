package repository

import (
	"context"
	"fmt"

	kitcomic "github.com/shouni/go-comic-kit/comic"
	"github.com/shouni/go-comic-kit/store"

	"github.com/shouni/ap-story/internal/domain"
)

// SaveState writes the state back to comic_state.json.
//
// go-comic-kit's store.Save is "always overwrite, always latest" and has no entry for conditional writes
// (GCS's ifGenerationMatch). So when a generation job and an edit run at the same time,
// the later write wins and the earlier change is lost. This race cannot be prevented here, so
// the caller is responsible for refusing edits concurrent with a running job
// (see handlers.UpdateComicScript).
//
// The history list cache is dropped after saving. Titles and chapter headings also appear in the list,
// so keeping it would show a list without the edit for up to 10 minutes.
func (r *ComicRepository) SaveState(ctx context.Context, jobID string, state *kitcomic.MangaState) error {
	if err := domain.ValidateJobID(jobID); err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("state is required")
	}

	outputDir, err := r.jobOutputDir(jobID)
	if err != nil {
		return err
	}
	if _, err := store.Save(ctx, r.store, state, outputDir); err != nil {
		return fmt.Errorf("failed to save the state (%s): %w", jobID, err)
	}

	r.deleteCachedHistory(jobID)
	return nil
}
