package builder

import (
	"context"

	"github.com/shouni/ap-story/internal/adapters"
	"github.com/shouni/ap-story/internal/config"
)

// buildTaskEnqueuer initialises the Cloud Tasks enqueuer.
func buildTaskEnqueuer(ctx context.Context, cfg *config.Config) (*adapters.TaskEnqueuer, error) {
	return adapters.NewTaskEnqueuer(ctx, cfg)
}
