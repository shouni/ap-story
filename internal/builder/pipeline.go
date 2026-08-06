package builder

import (
	"fmt"

	"github.com/shouni/go-comic-kit/ports"

	"github.com/shouni/ap-story/internal/app"
	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
	"github.com/shouni/ap-story/internal/pipeline"
)

// buildPipeline は、Task のコマンドに応じて Ops の操作を実行する Worker パイプライン
// （pipeline.Runner）を構築します。
func buildPipeline(cfg *config.Config, rio *app.RemoteIO, ops *ports.Operations, notifier domain.Notifier, jobStatus domain.JobStatusStore) (*pipeline.Runner, error) {
	runner, err := pipeline.New(pipeline.Dependencies{
		Ops:       ops,
		Reader:    rio.Reader,
		Writer:    rio.Writer,
		Bucket:    cfg.Storage.GCSBucket,
		Notifier:  notifier,
		Timeout:   cfg.AI.PipelineTimeout,
		JobStatus: jobStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize pipeline runner: %w", err)
	}
	return runner, nil
}
