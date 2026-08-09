package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/shouni/gcp-kit/tasks"

	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
)

// taskDispatchDeadline は Cloud Tasks がワーカーの応答を待つ上限です。
// HTTP ターゲットに指定できる最大値で、これ以上は伸ばせません。
const taskDispatchDeadline = 30 * time.Minute

// TaskEnqueuer は gcp-kit の Cloud Tasks Enqueuer をラップし、Task.TaskName() から
// 導出した決定的な名前でタスクを投入する domain.TaskQueue の実装です。
// 同名タスクの重複投入（Cloud Tasks の at-least-once 配信や呼び出し元の再試行によるもの）は
// ALREADY_EXISTS として成功扱いされ、実際に作られるタスクは1つだけになります。
type TaskEnqueuer struct {
	enqueuer *tasks.Enqueuer[domain.Task]
}

var _ domain.TaskQueue = (*TaskEnqueuer)(nil)

// NewTaskEnqueuer は Cloud Tasks エンキューアを初期化します。
// 生成されたインスタンスは内部で gRPC コネクションを保持するため、シングルトンとして
// 再利用し、アプリケーション終了時に Close してください。
func NewTaskEnqueuer(ctx context.Context, cfg *config.Config) (*TaskEnqueuer, error) {
	taskCfg := tasks.Config{
		ProjectID:  cfg.GCP.ProjectID,
		LocationID: cfg.GCP.LocationID,
		QueueID:    cfg.Tasks.QueueID,
		WorkerURL:  cfg.Tasks.WorkerURL,
		// タスクに指定する caller SA です。トークンを生成して付与するのは Cloud Tasks で、
		// このプロセスが署名するわけではありません。受信側が受け付ける許可リスト
		// （Tasks.AllowedServiceAccounts）とは別物なので取り違えないこと。
		ServiceAccountEmail: cfg.TaskCallerServiceAccount(),
		Audience:            cfg.Tasks.TaskAudienceURL,
		// ワーカーの実行時間の実効上限です。未指定だと Cloud Tasks の既定 10 分が効き、
		// worker 側の Cloud Run timeout をいくら長くしてもそこで打ち切られます。
		// compose_comic は既定値で下限 14 分かかるため、既定のままでは足りません。
		// PIPELINE_TIMEOUT をこれより短く（本番では 25m）設定して、アプリが自分で先に
		// 諦められるようにしています。関係は README「web / worker の分離」を参照。
		DispatchDeadline: taskDispatchDeadline,
	}
	enqueuer, err := tasks.NewEnqueuer[domain.Task](ctx, taskCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create cloud tasks enqueuer: %w", err)
	}
	return &TaskEnqueuer{enqueuer: enqueuer}, nil
}

// Enqueue は、task.TaskName() から導出した決定的な名前でタスクを Cloud Tasks に投入します。
func (e *TaskEnqueuer) Enqueue(ctx context.Context, task domain.Task) error {
	return e.enqueuer.EnqueueWithName(ctx, task.TaskName(), task)
}

// Close はエンキューアが保持するリソース（gRPC コネクション）を解放します。
func (e *TaskEnqueuer) Close() error {
	return e.enqueuer.Close()
}
