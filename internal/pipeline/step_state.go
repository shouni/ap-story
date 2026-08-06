package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/shouni/go-comic-kit/asset"
	"github.com/shouni/go-comic-kit/store"
)

// LoadStateStep は、Task.JobID の既存 state をロードして pc.Manga にセットします。
// state が存在しない（新規ジョブ）場合はエラーになります。
type LoadStateStep struct{}

// Name はステップ名を返します。
func (LoadStateStep) Name() string { return "load_state" }

// Execute は既存 state をロードして pc.Manga にセットします。
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

// LoadStateStepOptional は LoadStateStep と同様ですが、state が存在しない場合は
// エラーにせず pc.Manga を nil のままにします（generate_design_sheet の単発生成向け）。
// 作品 state（comics/{jobID}）が見つからない場合はデザインシート単体生成ジョブと
// 判断し、以降の保存先（pc.OutputDir）を design-jobs/{jobID} へ切り替えます。
// 作品の履歴一覧が comics/ の列挙だけで完結するよう、単体生成の state は
// comics/ に混ぜません。
type LoadStateStepOptional struct{}

// Name はステップ名を返します。
func (LoadStateStepOptional) Name() string { return "load_state_optional" }

// Execute は既存 state のロードを試み、無ければ保存先を design-jobs/ へ切り替えます。
func (LoadStateStepOptional) Execute(ctx context.Context, pc *Context) error {
	statePath, err := stateObjectPath(pc)
	if err != nil {
		return err
	}
	if manga, err := store.Load(ctx, pc.Reader, statePath); err == nil {
		pc.Manga = manga
		return nil
	}

	// 作品 state がない = 単体生成ジョブ。同じジョブ ID の再実行（既存の単体生成
	// state の更新）にも対応するため、design-jobs/ 側のロードも試みる。
	pc.OutputDir = pc.DesignJobOutputDir
	designStatePath, err := stateObjectPath(pc)
	if err != nil {
		return err
	}
	if manga, err := store.Load(ctx, pc.Reader, designStatePath); err == nil {
		pc.Manga = manga
	}
	return nil // state 未作成は正常系（新規に作られる）
}

// LoadStateIfExistsStep は、既存 state があれば pc.Manga に読み込みます。
// 無ければ何もしません（新規ジョブは正常系）。
//
// compose_comic の先頭に置くことで、Cloud Tasks の再配信で同じジョブがやり直された
// ときに、保存済みの台本と生成済み画像を引き継げます。これが無いと OutlineStep が
// state を新品に差し替えてしまい、工程ごとに保存した意味が失われます。
type LoadStateIfExistsStep struct{}

// Name はステップ名を返します。
func (LoadStateIfExistsStep) Name() string { return "load_state_if_exists" }

// Execute は既存 state があれば読み込みます。
func (LoadStateIfExistsStep) Execute(ctx context.Context, pc *Context) error {
	statePath, err := stateObjectPath(pc)
	if err != nil {
		return err
	}
	manga, err := store.Load(ctx, pc.Reader, statePath)
	if err != nil {
		return nil // 未作成は正常系（これから作られる）
	}
	pc.Manga = manga
	slog.InfoContext(ctx, "resuming from existing state",
		"chapters", len(manga.Chapters), "panels", len(manga.Panels))
	return nil
}

// SaveStateStep は、pc.Manga を state として保存します。
type SaveStateStep struct{}

// Name はステップ名を返します。
func (SaveStateStep) Name() string { return "save_state" }

// Execute は pc.Manga を state として保存します。
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
