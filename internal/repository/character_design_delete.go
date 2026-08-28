package repository

import (
	"context"
	"errors"
	"fmt"

	kitcomic "github.com/shouni/go-comic-kit/comic"

	"github.com/shouni/go-comic-kit/asset"
	"github.com/shouni/go-comic-kit/store"

	"github.com/shouni/ap-story/internal/domain"
)

// DeleteCharacterDesign は、指定キャラクターの生成履歴1件を GCS から削除します。
// 削除対象は character/{characterID}/{jobID}.ext の画像1枚で、拡張子は列挙から特定します。
// ジョブがデザインシート単体生成だった場合は、対応する state（design-jobs/{jobID}/、
// または旧形式で comics/{jobID}/ に残った章立てなしの state）も孤児として残さないよう
// 併せて削除します。作品ジョブ（章立てあり）の state はそのまま残し、state 内の
// 該当デザインシート記録だけを取り除きます。
func (r *ComicRepository) DeleteCharacterDesign(ctx context.Context, characterID string, jobID string) error {
	if err := domain.ValidateJobID(jobID); err != nil {
		return err
	}

	imagePath, err := r.findCharacterDesignImage(ctx, characterID, jobID)
	if err != nil {
		return err
	}
	if err := r.store.Delete(ctx, imagePath); err != nil {
		return fmt.Errorf("デザインシート画像 %q の削除に失敗しました: %w", imagePath, err)
	}

	return r.cleanupDesignJobState(ctx, jobID, imagePath)
}

// findCharacterDesignImage は character/{characterID}/ 配下から jobID に対応する
// 画像のフルパスを探します。見つからない場合はエラーを返します。
func (r *ComicRepository) findCharacterDesignImage(ctx context.Context, characterID string, jobID string) (string, error) {
	items, err := r.ListCharacterDesignHistory(ctx, characterID)
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.JobID == jobID {
			return item.ImageURL, nil
		}
	}
	return "", fmt.Errorf("キャラクター %q にジョブ %q のデザインシートが見つかりません", characterID, jobID)
}

// cleanupDesignJobState は画像削除後の state の後始末を行います。
// state が存在しない場合は何もしません。デザインシート単体生成ジョブの state は
// ジョブごと削除し、作品ジョブの state からは削除済み画像への参照だけを取り除きます。
func (r *ComicRepository) cleanupDesignJobState(ctx context.Context, jobID string, deletedImagePath string) error {
	// 単体生成ジョブの state（design-jobs/{jobID}/）があればディレクトリごと削除する。
	designJobDir, err := domain.DesignJobOutputDir(r.bucket, jobID)
	if err != nil {
		return err
	}
	designStatePath, err := asset.StatePath(designJobDir)
	if err != nil {
		return err
	}
	if exists, err := r.store.Exists(ctx, designStatePath); err == nil && exists {
		if err := r.deletePrefix(ctx, designJobDir); err != nil {
			return fmt.Errorf("デザインシート単体生成ジョブ %q の state 削除に失敗しました: %w", jobID, err)
		}
		return nil
	}

	state, err := r.GetState(ctx, jobID)
	if err != nil {
		if errors.Is(err, domain.ErrStateNotFound) {
			// state を持たない（または既に削除済みの）画像は画像削除だけで完了。
			return nil
		}
		// 読めなかっただけの場合は成功にしません。state には削除済み画像への参照が
		// 残っており、黙って完了にすると壊れた参照が残ったことに誰も気付けません。
		return fmt.Errorf("ジョブ %q の state を読めないため参照の後始末ができません: %w", jobID, err)
	}

	// 旧形式: 単体生成ジョブの state が comics/ に保存されていた（章立てなしで判別）。
	if len(state.Chapters) == 0 {
		return r.DeleteHistory(ctx, jobID)
	}

	kept := make([]kitcomic.DesignSheetRef, 0, len(state.DesignSheets))
	for _, ds := range state.DesignSheets {
		if ds.ImageURL != deletedImagePath {
			kept = append(kept, ds)
		}
	}
	if len(kept) == len(state.DesignSheets) {
		return nil
	}
	state.DesignSheets = kept

	outputDir, err := r.jobOutputDir(jobID)
	if err != nil {
		return err
	}
	if _, err := store.Save(ctx, r.store, state, outputDir); err != nil {
		return fmt.Errorf("state からのデザインシート参照の削除に失敗しました: %w", err)
	}
	return nil
}
