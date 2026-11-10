package repository

import (
	"context"
	"errors"
	"fmt"

	kitcomic "github.com/shouni/go-comic-kit/comic"

	"github.com/shouni/go-comic-kit/asset"
	"github.com/shouni/go-comic-kit/store"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/domain"
)

// DeleteCharacterDesign deletes one generation record of the given character from GCS.
// The target is the one image character/{characterID}/{jobID}.ext; its extension is found by listing.
// When the job was a standalone design-sheet generation, the matching state (design-jobs/{jobID}/,
// or a chapterless state left in comics/{jobID}/ in the legacy format) is deleted too so that it is
// not left orphaned. The state of a comic job (with chapters) stays, and only the matching
// design-sheet record inside the state is removed.
func (r *ComicRepository) DeleteCharacterDesign(ctx context.Context, characterID string, jobID string) error {
	if err := domain.ValidateJobID(jobID); err != nil {
		return err
	}

	imagePath, err := r.findCharacterDesignImage(ctx, characterID, jobID)
	if err != nil {
		return err
	}
	if err := r.store.Delete(ctx, imagePath); err != nil {
		return fmt.Errorf("failed to delete design-sheet image %q: %w", imagePath, err)
	}

	return r.cleanupDesignJobState(ctx, jobID, imagePath)
}

// findCharacterDesignImage finds the full path of the image for jobID under
// character/{characterID}/. It returns an error when there is none.
func (r *ComicRepository) findCharacterDesignImage(ctx context.Context, characterID string, jobID string) (string, error) {
	items, err := r.ListCharacterDesignHistory(ctx, characterID)
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.JobID == jobID {
			return item.ImageURL, nil
		}
	}
	return "", fmt.Errorf("no design sheet for job %q found for character %q", jobID, characterID)
}

// cleanupDesignJobState cleans up the state after the image is deleted.
// It does nothing when there is no state. The state of a standalone design-sheet job is
// deleted with the job, and from a comic job's state only the references to the deleted image are removed.
func (r *ComicRepository) cleanupDesignJobState(ctx context.Context, jobID string, deletedImagePath string) error {
	// If a standalone job's state (design-jobs/{jobID}/) exists, delete the whole directory.
	designJobDir, err := domain.DesignJobOutputDir(r.bucket, jobID)
	if err != nil {
		return err
	}
	designStatePath, err := asset.StatePath(designJobDir)
	if err != nil {
		return err
	}
	if exists, err := r.store.Exists(ctx, designStatePath); err == nil && exists {
		if _, err := remoteio.DeletePrefix(ctx, r.store, designJobDir); err != nil {
			return fmt.Errorf("failed to delete the state of standalone design-sheet job %q: %w", jobID, err)
		}
		return nil
	}

	state, err := r.GetState(ctx, jobID)
	if err != nil {
		if errors.Is(err, domain.ErrStateNotFound) {
			// An image with no state (or one already deleted) is done once the image is deleted.
			return nil
		}
		// A mere failure to read is not a success. The state still holds a reference to the deleted
		// image, and silently reporting completion would leave a broken reference that nobody notices.
		return fmt.Errorf("cannot clean up references because the state of job %q is unreadable: %w", jobID, err)
	}

	// Legacy format: a standalone job's state was saved in comics/ (identified by having no chapters).
	if len(state.Chapters) == 0 {
		return r.DeleteHistory(ctx, jobID)
	}

	kept := make([]kitcomic.DesignSheetRef, 0, len(state.DesignSheets))
	for _, ds := range state.DesignSheets {
		if ds.ImageURL != deletedImagePath {
			kept = append(kept, ds)
		}
	}
	if len(kept) == len(state.DesignSheets) {
		return nil
	}
	state.DesignSheets = kept

	outputDir, err := r.jobOutputDir(jobID)
	if err != nil {
		return err
	}
	if _, err := store.Save(ctx, r.store, state, outputDir); err != nil {
		return fmt.Errorf("failed to remove the design-sheet reference from the state: %w", err)
	}
	return nil
}
