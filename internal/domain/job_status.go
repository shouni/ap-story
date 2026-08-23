package domain

import (
	"path"
	"time"

	"github.com/shouni/go-job-kit/jobstatus"
)

// JobState はジョブのライフサイクル上の状態です。
// 実体は go-job-kit の jobstatus.State です。
type JobState = jobstatus.State

const (
	// JobStateQueued は Cloud Tasks へ投入済みで、まだワーカーが処理を始めていない状態です。
	JobStateQueued = jobstatus.StateQueued
	// JobStateRunning はワーカーが処理中の状態です。
	JobStateRunning = jobstatus.StateRunning
	// JobStateSucceeded は成果物の保存まで完了した状態です。
	JobStateSucceeded = jobstatus.StateSucceeded
	// JobStateFailed は処理が失敗した状態です。Cloud Tasks による再試行の対象になり得ます。
	JobStateFailed = jobstatus.StateFailed
)

// ErrJobStatusNotFound は、ジョブ状態がまだ記録されていないことを表します。
// 「状態が無い」は異常ではなく正常な状態（記録前の投入や、この機能より前に作られた
// ジョブ）なので、呼び出し側がストレージ障害と区別できるよう独立したエラーにしています。
//
// JobState と同じく go-job-kit の値をそのまま指しています。状態の定数だけを domain で
// 別名にしてエラーを repository に置いていたため、同じ jobstatus の面を扱うのに
// 「状態は domain 経由・エラーは具象パッケージ経由」と参照先が割れていました。
var ErrJobStatusNotFound = jobstatus.ErrNotFound

// jobStatusFile はジョブ進行状況を記録するオブジェクト名です。
const jobStatusFile = "status.json"

// JobStatus はジョブの進行状況です。
//
// 生成の成否はこれまで Slack 通知にしか残らず、失敗したジョブは UI から完全に消えていました。
// この記録があることで、UI・M2M クライアントの双方が投入後の状態を追跡できます。
// あわせて、Cloud Tasks の at-least-once 配信に対する再実行ガードの根拠にもなります。
//
// 共通フィールド（JobID・State・Attempts 等）と IsTerminal は jobstatus.Status が持ちます。
// 埋め込みなので JSON はフラットなまま保たれ、既存の status.json をそのまま読み書きできます。
type JobStatus struct {
	jobstatus.Status
	// OutputDir は成果物を格納する GCS URI です。
	OutputDir string `json:"output_dir,omitempty"`
}

// NewQueuedJobStatus は、キュー投入直後のジョブ状態を組み立てます。
func NewQueuedJobStatus(task Task, now time.Time) JobStatus {
	return JobStatus{
		Status: jobstatus.Status{
			JobID:     task.JobID,
			Command:   string(task.Command),
			State:     JobStateQueued,
			QueuedAt:  now,
			UpdatedAt: now,
		},
	}
}

// JobStatusPath は、ジョブ進行状況 JSON の相対オブジェクトパスを返します。
// 成果物と同じ comics/{jobID}/ 配下に置くため、履歴削除（プレフィックス一括削除）で
// 自動的に片付きます。履歴一覧は comic_state.json だけを拾うので一覧には混ざりません。
func JobStatusPath(jobID string) (string, error) {
	prefix, err := JobObjectPrefix(jobID)
	if err != nil {
		return "", err
	}
	return path.Join(prefix, jobStatusFile), nil
}
