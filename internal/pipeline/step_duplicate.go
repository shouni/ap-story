package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"time"

	"github.com/shouni/go-comic-kit/asset"
	"github.com/shouni/go-comic-kit/comic"
	"github.com/shouni/go-comic-kit/store"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/domain"
)

// Copier duplicates one stored object. remoteio.Store satisfies it as is.
type Copier interface {
	Copy(ctx context.Context, src, dst string, opts ...remoteio.WriteOption) error
}

// DuplicateStateStep copies the work in Task.SourceJobID into this job: the script and the
// panel images, but not the pages.
//
// It is how a second-language edition is made. Panels are drawn without text, so one set of
// panels serves every language; pages carry the lettering, so they are left out and composed
// again from the copy's script. The copy records Task.Lang, which is what makes that composition
// letter in the new language.
//
// The panel images are copied rather than referenced. A copy pointing at the source's images
// would lose them when the source work is deleted, with nothing on the copy saying why.
type DuplicateStateStep struct{}

// Name returns the step name.
func (DuplicateStateStep) Name() string { return "duplicate" }

// Execute copies the source work into this job. When this job already has a state (a re-run
// from redelivery), it does nothing: the copy may have been edited since.
func (DuplicateStateStep) Execute(ctx context.Context, pc *Context) error {
	if pc.Manga != nil {
		slog.InfoContext(ctx, "the copy already exists, skipping", "panels", len(pc.Manga.Panels))
		return nil
	}
	if pc.Copier == nil {
		return fmt.Errorf("duplicate: no copier is configured")
	}

	sourceDir, err := domain.JobOutputDir(pc.Bucket, pc.Task.SourceJobID)
	if err != nil {
		return fmt.Errorf("duplicate: resolve source dir: %w", err)
	}
	statePath, err := asset.StatePath(sourceDir)
	if err != nil {
		return fmt.Errorf("duplicate: %w", err)
	}
	manga, err := store.Load(ctx, pc.Reader, statePath)
	if err != nil {
		return fmt.Errorf("duplicate: load source %q: %w", pc.Task.SourceJobID, err)
	}

	for i := range manga.Panels {
		if err := copyPanelImage(ctx, pc, &manga.Panels[i]); err != nil {
			return err
		}
	}

	now := time.Now().UTC()
	manga.ID = pc.Task.JobID
	manga.Lang = pc.Task.Lang
	// Pages are the only artifacts that hold lettering. Dropping them leaves render_comic
	// exactly the work that remains: skip every panel, compose every page.
	manga.Pages = nil
	manga.CreatedAt = now
	manga.UpdatedAt = now
	pc.Manga = manga

	slog.InfoContext(ctx, "duplicated the work",
		"source_job_id", pc.Task.SourceJobID, "panels", len(manga.Panels), "lang", manga.Lang)
	return nil
}

// copyPanelImage copies one panel's image under this job and points the panel's record at the copy.
// A panel not drawn yet has nothing to copy and stays that way.
func copyPanelImage(ctx context.Context, pc *Context, panel *comic.Panel) error {
	if panel.Generation == nil || panel.Generation.ImageURL == "" {
		return nil
	}
	src := panel.Generation.ImageURL
	dst, err := asset.PanelImagePath(pc.OutputDir, panel.ID, path.Ext(src))
	if err != nil {
		return fmt.Errorf("duplicate: panel %q: %w", panel.ID, err)
	}
	if err := pc.Copier.Copy(ctx, src, dst); err != nil {
		return fmt.Errorf("duplicate: copy panel %q: %w", panel.ID, err)
	}
	// The record is cloned first: the panel came from the loaded state by value, but its
	// Generation is a pointer.
	record := *panel.Generation
	record.ImageURL = dst
	panel.Generation = &record
	return nil
}
