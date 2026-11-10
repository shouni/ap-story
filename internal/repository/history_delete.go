package repository

import (
	"context"
	"fmt"

	"github.com/shouni/go-remote-io/remoteio"
)

// DeleteHistory deletes the job's full set of artifacts (state, design sheets, images) from GCS.
func (r *ComicRepository) DeleteHistory(ctx context.Context, jobID string) error {
	outputDir, err := r.jobOutputDir(jobID)
	if err != nil {
		return err
	}
	if _, err := remoteio.DeletePrefix(ctx, r.store, outputDir); err != nil {
		return fmt.Errorf("failed to delete comic %q: %w", jobID, err)
	}
	r.deleteCachedHistory(jobID)
	// The job ID list is discarded too. Without this, a deleted job would stay in the list for the TTL
	// (its summary is gone, so it would appear as a row of fallback values).
	r.invalidateJobIDList()
	return nil
}
