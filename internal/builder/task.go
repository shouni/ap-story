package builder

import (
	"context"

	"github.com/shouni/ap-story/internal/adapters"
	"github.com/shouni/ap-story/internal/config"
)

// buildTaskEnqueuer は、Cloud Tasks エンキューアを初期化します。
func buildTaskEnqueuer(ctx context.Context, tasksCfg config.TasksConfig, gcp config.GCPConfig) (*adapters.TaskEnqueuer, error) {
	return adapters.NewTaskEnqueuer(ctx, tasksCfg, gcp)
}
