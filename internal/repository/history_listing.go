package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	kitcomic "github.com/shouni/go-comic-kit/comic"
	kitports "github.com/shouni/go-comic-kit/ports"

	"github.com/shouni/go-comic-kit/store"
	"github.com/shouni/go-job-kit/joblist"
	"github.com/shouni/go-job-kit/paging"
	"github.com/shouni/go-utils/jobid"

	"github.com/shouni/ap-story/internal/domain"
)

const comicsPrefix = "comics"

// historyLoadConcurrency は 1 ページ分の state を読み込むときの同時実行数です。
const historyLoadConcurrency = 10

// ListHistoryPage は履歴一覧をページングして取得します。
// /history は実際の漫画作品だけを対象とします。デザインシート単体生成ジョブの state は
// design-jobs/ 配下に保存されるため comics/ の列挙には現れず、ID の列挙・ページングは
// state を読まずに完結します（読み込むのは選択されたページの分だけ）。
// 章立てを持たない state（旧形式で comics/ に残った単体生成ジョブや読み込み失敗の
// フォールバック値）は念のため表示から除外します。
func (r *ComicRepository) ListHistoryPage(ctx context.Context, page int, perPage int) (domain.ComicHistoryPage, error) {
	jobIDs, err := r.listJobIDs(ctx)
	if err != nil {
		return domain.ComicHistoryPage{}, fmt.Errorf("GCS履歴のリスト取得に失敗しました: %w", err)
	}

	// ID に埋め込まれた生成時刻で並べます。既定の ID 降順に頼れないのは、採番形式を
	// "c{時刻}-{乱数}" から jobid.New の "c-{時刻}-{乱数}" へ移行したためです。
	// 辞書順では "c-" < "c2" となり、新形式の ID が旧形式より常に後ろに回ります。
	all, meta, err := paging.LoadPage(ctx, jobIDs, page, perPage, jobid.SortKey, r.loadHistoryOrFallback,
		paging.WithConcurrency(historyLoadConcurrency),
	)
	if err != nil {
		return domain.ComicHistoryPage{}, fmt.Errorf("GCS履歴の読み込みに失敗しました: %w", err)
	}

	histories := make([]domain.ComicHistory, 0, len(all))
	for _, h := range all {
		if h.ChapterCount == 0 {
			continue
		}
		histories = append(histories, h)
	}

	return domain.ComicHistoryPage{
		Items:    histories,
		PageMeta: paging.AdjustItemCount(meta, len(histories)),
	}, nil
}

// jobIDListCacheKey はジョブ ID 一覧のキャッシュキーです。
// 一覧対象は comics/ 直下の 1 種類だけなので固定キーで足ります。
const jobIDListCacheKey = "job-ids"

// listJobIDs は comics/ 直下のジョブディレクトリからジョブ ID を集めます。
// バケット全体の走査になるため、短い TTL のキャッシュを挟みます。
//
// 走査そのものは joblist が担います（区切り文字を指定して、ジョブ 1 件を 1 エントリで
// 受け取る形。指定しないと配下の成果物（images/ のパネル・ページ画像）が全件返り、
// 呼び出し側でジョブ ID の重複を潰すことになります）。
//
// 拾えるのはディレクトリ名だけなので、comic_state.json を持たないジョブ
// （生成が state の保存前に落ちたもの）も ID としては現れます。それらは state を
// 読めず ChapterCount == 0 のフォールバック値になるため、ListHistoryPage の
// 章立てフィルタで表示から外れます。
func (r *ComicRepository) listJobIDs(ctx context.Context) ([]string, error) {
	return r.jobIDCache.Load(ctx, jobIDListCacheKey, func(ctx context.Context) ([]string, error) {
		return joblist.Collect(ctx, r.reader, fmt.Sprintf("gs://%s/%s", r.bucket, comicsPrefix))
	})
}

// invalidateJobIDList は一覧キャッシュを破棄し、ジョブの削除を即座に反映させます。
func (r *ComicRepository) invalidateJobIDList() {
	r.jobIDCache.Invalidate(jobIDListCacheKey)
}

// loadHistoryOrFallback は 1 件分の履歴サマリを読み込みます。
//
// state の読み込みに失敗しても一覧からは落とさず、ジョブID・タイトルのみのフォールバック値を
// 返します（ChapterCount == 0 になるため、/history のフィルタで自然に除外されます）。
// LoadPage は load がエラーを返した ID を一覧から取り除くので、残したいものは
// エラーではなく代替値として返す必要があります。
//
// コンテキストのキャンセル・タイムアウトはフォールバックしません。ページ全体が
// 代替値で埋まった一覧を「正常な結果」として返さないよう、エラーのまま LoadPage へ渡し、
// 呼び出し元まで伝播させます。
func (r *ComicRepository) loadHistoryOrFallback(ctx context.Context, jobID string) (domain.ComicHistory, error) {
	history, err := r.buildHistory(ctx, jobID)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return domain.ComicHistory{}, ctxErr
		}
		slog.WarnContext(ctx, "failed to load state for history list",
			"job_id", jobID,
			"error", err,
		)
		return domain.ComicHistory{JobID: jobID, Title: jobID}, nil
	}
	return history, nil
}

func (r *ComicRepository) buildHistory(ctx context.Context, jobID string) (domain.ComicHistory, error) {
	if history, ok := r.getCachedHistory(jobID); ok {
		return history, nil
	}

	state, err := r.GetState(ctx, jobID)
	if err != nil {
		return domain.ComicHistory{}, err
	}

	history := historyFromState(jobID, state)
	r.setCachedHistory(jobID, history)
	return history, nil
}

func historyFromState(jobID string, state *kitcomic.MangaState) domain.ComicHistory {
	title := strings.TrimSpace(state.Title)
	if title == "" {
		title = jobID
	}
	return domain.ComicHistory{
		JobID:        jobID,
		Title:        title,
		StyleMode:    state.StyleMode,
		ChapterCount: len(state.Chapters),
		PanelCount:   len(state.Panels),
		PageCount:    len(state.Pages),
		CreatedAt:    formatTime(state.CreatedAt),
		UpdatedAt:    formatTime(state.UpdatedAt),
	}
}

// GetState は指定ジョブの MangaState を取得します。
//
// state がまだ無い場合は domain.ErrStateNotFound、あるはずなのに読めなかった場合は
// domain.ErrStateUnavailable を返します。切り分けは store.Load が返す go-comic-kit の
// 番兵（ports.ErrNotFound）で行うので、ストレージへの往復は増えません。
//
// どちらにも原因のエラーを包んであります。以前はすべてを「見つかりません」に潰し、
// 原因も捨てていたため、権限エラーも GCS 障害も画面には「作品がありません」と出て、
// ログにも理由が残りませんでした。
func (r *ComicRepository) GetState(ctx context.Context, jobID string) (*kitcomic.MangaState, error) {
	if err := domain.ValidateJobID(jobID); err != nil {
		return nil, err
	}
	statePath, err := r.stateObjectPath(jobID)
	if err != nil {
		return nil, err
	}
	state, err := store.Load(ctx, r.reader, statePath)
	if err != nil {
		// go-comic-kit v1.6.0 から store.Load は失敗を ports の番兵で分類します。
		// 「まだ無い」は ports.ErrNotFound で、原因の os.ErrNotExist は包まれません。
		// 壊れた state（ports.ErrInvalidRequest）は「あるのに読めない」側に倒します。
		if errors.Is(err, kitports.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w (job %q): %w", domain.ErrStateNotFound, jobID, err)
		}
		return nil, fmt.Errorf("%w (job %q): %w", domain.ErrStateUnavailable, jobID, err)
	}
	return state, nil
}
