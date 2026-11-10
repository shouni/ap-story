package pipeline

import (
	"context"
	"fmt"

	"github.com/shouni/go-comic-kit/ports"
)

// AllPanelsStep generates every panel of pc.Manga. It is used by compose_comic's batch generation.
// Concurrency follows go-comic-kit's Config.MaxConcurrency (MAX_CONCURRENCY).
type AllPanelsStep struct {
	// When SkipGenerated is true, panels already generated are skipped (for resuming).
	SkipGenerated bool
}

// Name returns the step name.
func (AllPanelsStep) Name() string { return "panels" }

// Execute generates every panel of pc.Manga.
func (s AllPanelsStep) Execute(ctx context.Context, pc *Context) error {
	if pc.Manga == nil {
		return fmt.Errorf("panels: manga state is nil")
	}

	manga, err := pc.Ops.Panel.GenerateAllPanels(ctx, pc.Manga, ports.BatchOptions{
		Seed:        pc.Task.Seed,
		Model:       pc.imageModel(),
		AspectRatio: pc.Layout.AspectRatio,
		ImageSize:   pc.Layout.PanelImageSize,
		StyleMode:   pc.styleMode(),
		OutputDir:   pc.OutputDir,
		// With a chapter given, generate only that chapter. Images are the most expensive stage, so
		// the unit of checking can match the script's (per chapter).
		ChapterID:     pc.Task.ChapterID,
		SkipGenerated: s.SkipGenerated,
	})
	// Even when part of a batch fails, it returns the state recording the successes along with the error.
	// Unless it is received on error too, generated images are left in GCS with no reference from
	// the state (Runner.savePartialResults saves it).
	if manga != nil {
		pc.Manga = manga
	}
	if err != nil {
		return fmt.Errorf("panels: %w", err)
	}
	return nil
}

// SinglePanelStep generates or regenerates only the panel in Task.PanelID.
// It is used by regenerate_panel and applies the Task's Seed/EditPrompt/PromptOverride.
type SinglePanelStep struct{}

// Name returns the step name.
func (SinglePanelStep) Name() string { return "panel" }

// Execute generates or regenerates only the panel in Task.PanelID.
func (SinglePanelStep) Execute(ctx context.Context, pc *Context) error {
	opts := ports.GenerateOptions{
		Seed:           pc.Task.Seed,
		EditPrompt:     pc.Task.EditPrompt,
		PromptOverride: pc.Task.PromptOverride,
		Model:          pc.imageModel(),
		AspectRatio:    pc.Layout.AspectRatio,
		ImageSize:      pc.Layout.PanelImageSize,
		StyleMode:      pc.styleMode(),
		OutputDir:      pc.OutputDir,
	}
	manga, err := pc.Ops.Panel.GeneratePanel(ctx, pc.Manga, pc.Task.PanelID, opts)
	if err != nil {
		return fmt.Errorf("panel: panel %q: %w", pc.Task.PanelID, err)
	}
	pc.Manga = manga
	return nil
}
