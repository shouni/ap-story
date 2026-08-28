package repository

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/shouni/go-comic-kit/asset"
	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-utils/jobid"

	"github.com/shouni/ap-story/internal/domain"
)

// ListCharacterDesignHistory は、指定キャラクター単体のデザインシート生成履歴を
// 新しい順で返します。GCS の character/{characterID}/ 配下を直接列挙するため
// comic_state.json は読みません。ジョブID（例: "c20260718-113045-1a2b3c4d"）は
// 生成時刻を含む形式のため、文字列比較の降順ソートがそのまま新しい順になります。
func (r *ComicRepository) ListCharacterDesignHistory(ctx context.Context, characterID string) ([]domain.CharacterDesignHistoryItem, error) {
	listURI, err := asset.CharacterDesignPrefix(remoteio.BuildURI(remoteio.SchemeGCS, r.bucket, ""), characterID)
	if err != nil {
		return nil, fmt.Errorf("キャラクター画像の一覧パス生成に失敗しました: %w", err)
	}

	var items []domain.CharacterDesignHistoryItem
	for entry, err := range r.store.List(ctx, listURI) {
		if err != nil {
			return nil, fmt.Errorf("キャラクター画像履歴のリスト取得に失敗しました: %w", err)
		}
		jobID := jobIDFromCharacterImagePath(entry.URI, strings.TrimSuffix(listURI, "/"))
		if jobID == "" {
			continue
		}
		items = append(items, domain.CharacterDesignHistoryItem{
			JobID:    jobID,
			ImageURL: entry.URI,
		})
	}

	// ID の辞書順ではなく埋め込まれた生成時刻で並べます（新旧の採番形式が混在するため）。
	sort.Slice(items, func(i, j int) bool {
		ki, kj := jobid.SortKey(items[i].JobID), jobid.SortKey(items[j].JobID)
		if ki != kj {
			return ki > kj
		}
		return items[i].JobID > items[j].JobID
	})
	return items, nil
}

// jobIDFromCharacterImagePath は "character/{characterID}/{jobID}.ext" の GCS フルパスから
// jobID を抽出します。prefix 配下でない、あるいは1階層ちょうどでない場合は空文字を返します。
func jobIDFromCharacterImagePath(gcsPath, prefix string) string {
	if !strings.HasPrefix(gcsPath, prefix) {
		return ""
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(gcsPath, prefix), "/")
	if rel == "" || strings.Contains(rel, "/") {
		return ""
	}
	return strings.TrimSuffix(rel, path.Ext(rel))
}
