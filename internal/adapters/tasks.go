package adapters

import (
	"context"
	"fmt"

	"github.com/shouni/gcp-kit/tasks"

	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
)

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
func NewTaskEnqueuer(ctx context.Context, server config.ServerConfig, gcp config.GCPConfig) (*TaskEnqueuer, error) {
	taskCfg := tasks.Config{
		ProjectID:           gcp.ProjectID,
		LocationID:          gcp.LocationID,
		QueueID:             gcp.QueueID,
		WorkerURL:           server.WorkerURL,
		ServiceAccountEmail: gcp.ServiceAccountEmail,
		Audience:            gcp.TaskAudienceURL,
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
