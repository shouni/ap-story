package repository

import (
	"context"
	"fmt"
)

// DeleteHistory は指定ジョブの成果物一式（state・デザインシート・画像）を GCS から削除します。
func (r *ComicRepository) DeleteHistory(ctx context.Context, jobID string) error {
	outputDir, err := r.jobOutputDir(jobID)
	if err != nil {
		return err
	}
	if err := r.writer.Delete(ctx, outputDir); err != nil {
		return fmt.Errorf("comic %qの削除に失敗しました: %w", jobID, err)
	}
	r.deleteCachedHistory(jobID)
	// ジョブ ID 一覧も破棄します。これが無いと、削除したジョブが TTL のあいだ一覧に
	// 残り続けます（サマリ本体は消えているため、フォールバック値の行として現れます）。
	r.invalidateJobIDList()
	return nil
}
