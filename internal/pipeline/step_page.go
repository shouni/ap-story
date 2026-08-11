package pipeline

import (
	"context"
	"fmt"

	"github.com/shouni/go-comic-kit/ports"
)

// AllPagesStep は、pc.Manga の全ページを合成します。compose_comic の一括生成で使います。
// 並列数は go-comic-kit の Config.MaxConcurrency（MAX_CONCURRENCY）に従います。
type AllPagesStep struct {
	// SkipGenerated が true の場合、すでに合成済みのページを飛ばします（再開用）。
	SkipGenerated bool
}

// Name はステップ名を返します。
func (AllPagesStep) Name() string { return "pages" }

// Execute は pc.Manga の全ページを合成します。
func (s AllPagesStep) Execute(ctx context.Context, pc *Context) error {
	if pc.Manga == nil {
		return fmt.Errorf("pages: manga state is nil")
	}

	manga, err := pc.Ops.Page.ComposeAllPages(ctx, pc.Manga, ports.BatchOptions{
		Seed:        pc.Task.Seed,
		Model:       pc.imageModel(),
		AspectRatio: pc.Layout.AspectRatio,
		ImageSize:   pc.Layout.PageImageSize,
		StyleMode:   pc.styleMode(),
		OutputDir:   pc.OutputDir,
		ChapterID:     pc.Task.ChapterID,
		SkipGenerated: s.SkipGenerated,
	})
	// パネルと同じく、エラー時も成功分を記録した state を受け取る（AllPanelsStep 参照）。
	if manga != nil {
		pc.Manga = manga
	}
	if err != nil {
		return fmt.Errorf("pages: %w", err)
	}
	return nil
}

// SinglePageStep は、Task.Page の1ページのみ合成/再合成します。
// regenerate_page で使います。Task の Seed/EditPrompt/PromptOverride を反映します。
type SinglePageStep struct{}

// Name はステップ名を返します。
func (SinglePageStep) Name() string { return "page" }

// Execute は Task.Page の1ページのみ合成/再合成します。
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
