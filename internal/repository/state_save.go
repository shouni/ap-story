package repository

import (
	"context"
	"fmt"

	kitcomic "github.com/shouni/go-comic-kit/comic"
	"github.com/shouni/go-comic-kit/store"

	"github.com/shouni/ap-story/internal/domain"
)

// SaveState は state を comic_state.json へ書き戻します。
//
// go-comic-kit の store.Save は「常に上書き・常に最新」で、条件付き書き込み
// （GCS の ifGenerationMatch）の口を持ちません。つまり生成ジョブと編集が同時に走ると、
// 後から書いたほうが勝って先の変更が消えます。この競合はここでは防げないので、
// 呼び出し側が実行中ジョブとの同時編集を断る責任を持ちます
// （handlers.UpdateComicScript を参照）。
//
// 履歴一覧のキャッシュは保存後に落とします。タイトルや章の見出しは一覧にも出るため、
// 残したままだと編集が反映されていない一覧を最大10分見せることになります。
func (r *ComicRepository) SaveState(ctx context.Context, jobID string, state *kitcomic.MangaState) error {
	if err := domain.ValidateJobID(jobID); err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("state is required")
	}

	outputDir, err := r.jobOutputDir(jobID)
	if err != nil {
		return err
	}
	if _, err := store.Save(ctx, r.store, state, outputDir); err != nil {
		return fmt.Errorf("state の保存に失敗しました (%s): %w", jobID, err)
	}

	r.deleteCachedHistory(jobID)
	return nil
}
