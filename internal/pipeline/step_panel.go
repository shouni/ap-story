package pipeline

import (
	"context"
	"fmt"

	"github.com/shouni/go-comic-kit/ports"
)

// AllPanelsStep は、pc.Manga の全パネルを生成します。compose_comic の一括生成で使います。
// 並列数は go-comic-kit の Config.MaxConcurrency（MAX_CONCURRENCY）に従います。
type AllPanelsStep struct {
	// SkipGenerated が true の場合、すでに生成済みのコマを飛ばします（再開用）。
	SkipGenerated bool
}

// Name はステップ名を返します。
func (AllPanelsStep) Name() string { return "panels" }

// Execute は pc.Manga の全パネルを生成します。
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
		// 章の指定があればその章だけを生成する。画像はいちばん高価な工程なので、
		// 確認の単位を台本（章単位）と揃えられるようにしている。
		ChapterID:     pc.Task.ChapterID,
		SkipGenerated: s.SkipGenerated,
	})
	// 一括生成は一部が失敗しても、成功分を記録した state をエラーと一緒に返す。
	// エラー時も受け取っておかないと、生成済みの画像が state から参照されないまま
	// GCS に取り残される（Runner.savePartialResults がこれを保存する）。
	if manga != nil {
		pc.Manga = manga
	}
	if err != nil {
		return fmt.Errorf("panels: %w", err)
	}
	return nil
}

// SinglePanelStep は、Task.PanelID の1パネルのみ生成/再生成します。
// regenerate_panel で使います。Task の Seed/EditPrompt/PromptOverride を反映します。
type SinglePanelStep struct{}

// Name はステップ名を返します。
func (SinglePanelStep) Name() string { return "panel" }

// Execute は Task.PanelID の1パネルのみ生成/再生成します。
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
