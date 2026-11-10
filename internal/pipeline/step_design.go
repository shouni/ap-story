package pipeline

import (
	"context"
	"fmt"

	"github.com/shouni/go-comic-kit/ports"
)

// DesignSheetStep generates design sheets for the characters named in the Task.
// When pc.Manga is nil (a one-off generation) it creates a new MangaState.
type DesignSheetStep struct{}

// Name returns the step name.
func (DesignSheetStep) Name() string { return "design_sheet" }

// Execute generates the design sheet and updates pc.Manga.
func (DesignSheetStep) Execute(ctx context.Context, pc *Context) error {
	req := ports.DesignSheetRequest{
		CharacterIDs: pc.Task.CharacterIDs,
		JobID:        pc.Task.JobID,
		// Characters are shared assets that belong to no particular work, so they are saved at the bucket
		// root (under character/) rather than in comics/{jobID}/.
		OutputDir:   pc.CharacterOutputDir,
		AspectRatio: pc.Task.AspectRatio,
		Layout:      pc.Task.Layout,
		Model:       pc.Task.ModelOverride,
		StyleMode:   pc.Task.StyleMode,
		// Seed is *int64 on the kit side too, so passing nil means "leave the resolution to the kit".
		Seed: pc.Task.Seed,
	}
	if pc.Task.ReferenceURLOverride != "" || len(pc.Task.VisualCuesOverride) > 0 {
		req.Override = ports.DesignOverride{
			ReferenceURL: pc.Task.ReferenceURLOverride,
			VisualCues:   pc.Task.VisualCuesOverride,
		}
	}

	// With no state (a one-off generation), GenerateDesignSheet creates a new MangaState including
	// Version/CreatedAt, but the ID (the link to the job) is the caller's responsibility.
	manga, err := pc.Ops.DesignSheet.GenerateDesignSheet(ctx, pc.Manga, req)
	if err != nil {
		return fmt.Errorf("design_sheet: %w", err)
	}
	if manga.ID == "" {
		manga.ID = pc.Task.JobID
	}
	pc.Manga = manga
	return nil
}
