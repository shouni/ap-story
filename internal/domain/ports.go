package domain

import (
	"context"
	"errors"

	kitcomic "github.com/shouni/go-comic-kit/comic"
)

// ErrStateNotFound は、そのジョブの state がまだ無いことを表します。
//
// 生成が始まる前や、state の保存前に落ちたジョブでも起こる正常な状態です。
// ハンドラーはこれを 404（あるいは「処理中」の表示）へマップしてください。
var ErrStateNotFound = errors.New("comic state not found")

// ErrStateUnavailable は、state が「あるはずなのに読めなかった」ことを表します。
//
// ErrStateNotFound と分けているのは、両者で取るべき判断が正反対だからです。
// 権限不足や GCS 障害まで「無い」とみなすと、障害の間ずっと全作品が
// 「まだありません」と表示され、原因を追う手掛かりも残りません。
// ハンドラーはこれを 502（または 500）へマップし、原因をログへ残してください。
var ErrStateUnavailable = errors.New("comic state unavailable")

// TaskQueue は非同期キューを抽象化します。
type TaskQueue interface {
	Enqueue(ctx context.Context, task Task) error
}

// ComicRepository は作品（ジョブ）の履歴取得・削除、および共有キャラクター資産
// （デザインシート）の生成履歴取得を抽象化します。
// 詳細（GetState）は go-comic-kit の MangaState をそのまま返します。ap-story では
// MusicRecipe に相当する独自のドメイン型を持たず、state 自体が完結した表現だからです。
type ComicRepository interface {
	ListHistoryPage(ctx context.Context, page int, perPage int) (ComicHistoryPage, error)
	GetState(ctx context.Context, jobID string) (*kitcomic.MangaState, error)
	// SaveState は state をまるごと書き戻します。台本の手直しのための口で、
	// 生成パイプライン（worker）は go-comic-kit の store を直接使うためここは通りません。
	SaveState(ctx context.Context, jobID string, state *kitcomic.MangaState) error
	DeleteHistory(ctx context.Context, jobID string) error
	// ListCharacterDesignHistory は、指定キャラクター単体のデザインシート生成履歴を
	// 新しい順で返します。複数キャラクター合成生成のシートは対象外です
	// （character/{単体キャラクターID}/ 配下にしか記録されないため）。
	ListCharacterDesignHistory(ctx context.Context, characterID string) ([]CharacterDesignHistoryItem, error)
	// DeleteCharacterDesign は、指定キャラクターの生成履歴1件（character/{characterID}/{jobID}.ext）を
	// 削除します。ジョブがデザインシート単体生成（章立てなし）だった場合は、対応する
	// state（comics/{jobID}/）も併せて削除します。作品ジョブの state は削除しません。
	DeleteCharacterDesign(ctx context.Context, characterID string, jobID string) error
}

// Notifier はジョブ完了・失敗の通知（Slack 等）を抽象化します。
// 実装は Webhook 未設定時など通知が無効な場合、エラーを返さず黙ってスキップして構いません
// （通知の成否がジョブ自体の成否に影響してはならないため）。
type Notifier interface {
	NotifyComplete(ctx context.Context, task Task) error
	NotifyError(ctx context.Context, task Task, cause error) error
}

// JobStatusStore は、ジョブの進行状況（queued/running/succeeded/failed）を永続化します。
// Web プロセスが投入時の状態を、Worker プロセスが実行結果を書き込みます。
//
// Save / Get のシグネチャは go-job-kit の jobstatus.StatusStore に揃えてあります。
// これにより *jobstatus.Store[JobStatus] がそのまま実装となり、jobstatus.Recorder へ
// 渡すためのアダプタが要りません（以前はジョブ ID を状態に含める形だったため、
// 形を合わせるだけのアダプタを 3 サービスがそれぞれ持っていました）。
type JobStatusStore interface {
	// Save はジョブ状態を保存します。
	Save(ctx context.Context, jobID string, status JobStatus) error
	// Get はジョブ状態を取得します。未記録の場合はエラーを返します。
	Get(ctx context.Context, jobID string) (JobStatus, error)
}
