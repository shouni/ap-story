package pipeline

import (
	"context"
	"fmt"

	"github.com/shouni/go-comic-kit/ports"
)

// DesignSheetStep は、Task で指定されたキャラクターのデザインシートを生成します。
// pc.Manga が nil の場合（単発生成）は新しい MangaState を作成します。
type DesignSheetStep struct{}

// Name はステップ名を返します。
func (DesignSheetStep) Name() string { return "design_sheet" }

// Execute はデザインシートを生成し、pc.Manga を更新します。
func (DesignSheetStep) Execute(ctx context.Context, pc *Context) error {
	req := ports.DesignSheetRequest{
		CharacterIDs: pc.Task.CharacterIDs,
		JobID:        pc.Task.JobID,
		// キャラクターは特定の作品に依存しない共有アセットのため、comics/{jobID}/ ではなく
		// バケットルート（character/ 配下）に保存する。
		OutputDir:   pc.CharacterOutputDir,
		AspectRatio: pc.Task.AspectRatio,
		Layout:      pc.Task.Layout,
		Model:       pc.Task.ModelOverride,
		StyleMode:   pc.Task.StyleMode,
		// Seed はキット側も *int64 なので、nil のまま渡せば「解決はキットに任せる」意味になる。
		Seed: pc.Task.Seed,
	}
	if pc.Task.ReferenceURLOverride != "" || len(pc.Task.VisualCuesOverride) > 0 {
		req.Override = ports.DesignOverride{
			ReferenceURL: pc.Task.ReferenceURLOverride,
			VisualCues:   pc.Task.VisualCuesOverride,
		}
	}

	// state なし（単発生成）の場合、GenerateDesignSheet が Version/CreatedAt 込みの
	// 新しい MangaState を作るが、ID（ジョブとの紐付け）だけは呼び出し側の責務。
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
