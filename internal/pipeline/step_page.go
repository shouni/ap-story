package pipeline

import (
	"context"
	"fmt"

	"github.com/shouni/go-comic-kit/ports"
)

// AllPagesStep composes every page of pc.Manga. It is used by compose_comic's batch generation.
// Concurrency follows go-comic-kit's Config.MaxConcurrency (MAX_CONCURRENCY).
type AllPagesStep struct {
	// When SkipGenerated is true, pages already composed are skipped (for resuming).
	SkipGenerated bool
}

// Name returns the step name.
func (AllPagesStep) Name() string { return "pages" }

// Execute composes every page of pc.Manga.
func (s AllPagesStep) Execute(ctx context.Context, pc *Context) error {
	if pc.Manga == nil {
		return fmt.Errorf("pages: manga state is nil")
	}

	manga, err := pc.Ops.Page.ComposeAllPages(ctx, pc.Manga, ports.BatchOptions{
		Seed:          pc.Task.Seed,
		Model:         pc.imageModel(),
		AspectRatio:   pc.Layout.AspectRatio,
		ImageSize:     pc.Layout.PageImageSize,
		StyleMode:     pc.styleMode(),
		OutputDir:     pc.OutputDir,
		ChapterID:     pc.Task.ChapterID,
		SkipGenerated: s.SkipGenerated,
	})
	// As with panels, the state recording the successes is received even on error (see AllPanelsStep).
	if manga != nil {
		pc.Manga = manga
	}
	if err != nil {
		return fmt.Errorf("pages: %w", err)
	}
	return nil
}

// SinglePageStep composes or recomposes only the page in Task.Page.
// It is used by regenerate_page and applies the Task's Seed/EditPrompt/PromptOverride.
type SinglePageStep struct{}

// Name returns the step name.
func (SinglePageStep) Name() string { return "page" }

// Execute composes or recomposes only the page in Task.Page.
func (SinglePageStep) Execute(ctx context.Context, pc *Context) error {
	opts := ports.GenerateOptions{
		Seed:           pc.Task.Seed,
		EditPrompt:     pc.Task.EditPrompt,
		PromptOverride: pc.Task.PromptOverride,
		Model:          pc.imageModel(),
		AspectRatio:    pc.Layout.AspectRatio,
		ImageSize:      pc.Layout.PageImageSize,
		StyleMode:      pc.styleMode(),
		OutputDir:      pc.OutputDir,
	}
	manga, err := pc.Ops.Page.ComposePage(ctx, pc.Manga, pc.Task.Page, opts)
	if err != nil {
		return fmt.Errorf("page: page %d: %w", pc.Task.Page, err)
	}
	pc.Manga = manga
	return nil
}
