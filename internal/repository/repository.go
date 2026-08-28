// Package repository は、GCS 上の comic_state.json 群から履歴一覧・詳細取得・削除を行います。
package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shouni/go-comic-kit/asset"
	"github.com/shouni/go-job-kit/cache"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
)

const defaultHistoryCacheTTL = 10 * time.Minute

// ComicRepository は GCS 上の comic_state.json を読み書きし、履歴一覧・詳細・削除を提供します。
type ComicRepository struct {
	bucket string
	// store は完全な URI を受け取るルートのストアです。成果物のディレクトリは
	// domain が組み立てて go-comic-kit へも渡るため、ここでスコープは絞りません。
	store        remoteio.Store
	historyCache *cache.TTL[domain.ComicHistory]
	// jobIDCache は一覧走査で得たジョブ ID を短時間キャッシュします。
	// 履歴サマリ本体のキャッシュ（historyCache）と違い、これが無いと履歴画面を開くたびに
	// comics/ 配下全体の List が走ります。
	jobIDCache *cache.IDList
}

var _ domain.ComicRepository = (*ComicRepository)(nil)

// NewHistoryCache は ComicRepository に注入する履歴サマリ用キャッシュを生成します。
// 期限切れエントリの回収は生成と同時に始まります。停止は Close で行ってください。
func NewHistoryCache() *cache.TTL[domain.ComicHistory] {
	return cache.NewTTL[domain.ComicHistory](defaultHistoryCacheTTL)
}

// NewComicRepository は ComicRepository を構築します。
func NewComicRepository(
	storage config.StorageConfig,
	store remoteio.Store,
	historyCache *cache.TTL[domain.ComicHistory],
) *ComicRepository {
	return &ComicRepository{
		bucket:       storage.GCSBucket,
		store:        store,
		historyCache: historyCache,
		jobIDCache:   cache.NewIDList(cache.DefaultIDListTTL),
	}
}

// jobOutputDir は指定ジョブの成果物を格納する GCS URI を返します。
func (r *ComicRepository) jobOutputDir(jobID string) (string, error) {
	return domain.JobOutputDir(r.bucket, jobID)
}

// stateObjectPath は指定ジョブの comic_state.json の GCS URI を返します。
func (r *ComicRepository) stateObjectPath(jobID string) (string, error) {
	outputDir, err := r.jobOutputDir(jobID)
	if err != nil {
		return "", err
	}
	return asset.StatePath(outputDir)
}

// キャッシュキーのジョブ ID 正規化は cache.TTL 側で行われます。
func (r *ComicRepository) getCachedHistory(jobID string) (domain.ComicHistory, bool) {
	return r.historyCache.Get(jobID)
}

func (r *ComicRepository) setCachedHistory(jobID string, history domain.ComicHistory) {
	r.historyCache.Set(jobID, history)
}

func (r *ComicRepository) deleteCachedHistory(jobID string) {
	r.historyCache.Delete(jobID)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// deletePrefix はプレフィックス配下のオブジェクトをすべて削除します。
//
// remoteio の Delete は単一オブジェクトを消すもので、プレフィックスを渡しても
// 「その名前のオブジェクトは無い」として黙って成功します。ディレクトリという実体が
// 無いストレージでは、消す側が一覧して 1 つずつ消すしかありません。
// 以前はプレフィックスをそのまま Delete へ渡しており、本番では何も消えていませんでした。
func (r *ComicRepository) deletePrefix(ctx context.Context, prefix string) error {
	var uris []string
	for entry, err := range r.store.List(ctx, prefix) {
		if err != nil {
			return fmt.Errorf("削除対象の一覧取得に失敗しました (%s): %w", prefix, err)
		}
		uris = append(uris, entry.URI)
	}

	var errs []error
	for _, uri := range uris {
		if err := r.store.Delete(ctx, uri); err != nil {
			errs = append(errs, fmt.Errorf("%s の削除に失敗しました: %w", uri, err))
		}
	}
	return errors.Join(errs...)
}
