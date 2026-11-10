package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/shouni/go-comic-kit/asset"
	"github.com/shouni/go-comic-kit/store"
)

// LoadStateStep loads the existing state of Task.JobID and sets it on pc.Manga.
// It fails when no state exists (a new job).
type LoadStateStep struct{}

// Name returns the step name.
func (LoadStateStep) Name() string { return "load_state" }

// Execute loads the existing state and sets it on pc.Manga.
func (LoadStateStep) Execute(ctx context.Context, pc *Context) error {
	statePath, err := stateObjectPath(pc)
	if err != nil {
		return err
	}
	manga, err := store.Load(ctx, pc.Reader, statePath)
	if err != nil {
		return fmt.Errorf("load_state: %w", err)
	}
	pc.Manga = manga
	return nil
}

// LoadStateStepOptional is like LoadStateStep, but when no state exists it does not fail and
// leaves pc.Manga nil (for one-off generate_design_sheet runs).
// When the comic state (comics/{jobID}) is not found, the job is taken to be a standalone design-sheet
// job, and the destination from then on (pc.OutputDir) switches to design-jobs/{jobID}.
// Standalone states are kept out of comics/ so that the history list of works is complete
// from a listing of comics/ alone.
type LoadStateStepOptional struct{}

// Name returns the step name.
func (LoadStateStepOptional) Name() string { return "load_state_optional" }

// Execute tries to load the existing state and, if there is none, switches the destination to design-jobs/.
func (LoadStateStepOptional) Execute(ctx context.Context, pc *Context) error {
	statePath, err := stateObjectPath(pc)
	if err != nil {
		return err
	}
	// Only ErrNotFound means "missing". Treating a read failure as "missing" too would overwrite
	// the existing record with a newly generated state (LoadIfExists makes that call).
	manga, err := store.LoadIfExists(ctx, pc.Reader, statePath)
	if err != nil {
		return fmt.Errorf("failed to read the existing state: %w", err)
	}
	if manga != nil {
		pc.Manga = manga
		return nil
	}

	// No comic state = a standalone job. Loading from design-jobs/ is tried too, so that a re-run
	// of the same job ID (updating an existing standalone state) works.
	pc.OutputDir = pc.DesignJobOutputDir
	designStatePath, err := stateObjectPath(pc)
	if err != nil {
		return err
	}
	manga, err = store.LoadIfExists(ctx, pc.Reader, designStatePath)
	if err != nil {
		return fmt.Errorf("failed to read the existing state: %w", err)
	}
	if manga != nil {
		pc.Manga = manga
	}
	return nil // no state yet is the normal path (one gets created)
}

// LoadStateIfExistsStep loads the existing state into pc.Manga if there is one.
// If not, it does nothing (a new job is the normal path).
//
// Placed at the head of compose_comic, it lets a job that Cloud Tasks redelivers inherit the saved
// script and generated images. Without it OutlineStep would replace the state with a fresh one,
// defeating the point of saving at each stage.
type LoadStateIfExistsStep struct{}

// Name returns the step name.
func (LoadStateIfExistsStep) Name() string { return "load_state_if_exists" }

// Execute loads the existing state if there is one.
func (LoadStateIfExistsStep) Execute(ctx context.Context, pc *Context) error {
	statePath, err := stateObjectPath(pc)
	if err != nil {
		return err
	}
	// Not created yet is the normal path (it is about to be). But only ErrNotFound means "missing";
	// treating a read failure as "missing" too would overwrite the saved script and images with a
	// fresh state on every redelivery.
	manga, err := store.LoadIfExists(ctx, pc.Reader, statePath)
	if err != nil {
		return fmt.Errorf("failed to read the existing state: %w", err)
	}
	if manga == nil {
		return nil
	}
	pc.Manga = manga
	slog.InfoContext(ctx, "resuming from existing state",
		"chapters", len(manga.Chapters), "panels", len(manga.Panels))
	return nil
}

// SaveStateStep saves pc.Manga as the state.
type SaveStateStep struct{}

// Name returns the step name.
func (SaveStateStep) Name() string { return "save_state" }

// Execute saves pc.Manga as the state.
func (SaveStateStep) Execute(ctx context.Context, pc *Context) error {
	if pc.Manga == nil {
		return fmt.Errorf("save_state: manga state is nil")
	}
	if _, err := store.Save(ctx, pc.Writer, pc.Manga, pc.OutputDir); err != nil {
		return fmt.Errorf("save_state: %w", err)
	}
	return nil
}

func stateObjectPath(pc *Context) (string, error) {
	if pc.OutputDir == "" {
		return "", fmt.Errorf("output dir is not set")
	}
	return asset.StatePath(pc.OutputDir)
}
