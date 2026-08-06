// Package builder は、設定値から各サービスクライアント・DI コンテナの
// 依存関係を組み立てるファクトリ関数を提供します。
package builder

import (
	"context"
	"fmt"
	"io"

	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-remote-io/remoteio/gcs"

	"github.com/shouni/ap-story/internal/adapters"
	"github.com/shouni/ap-story/internal/app"
	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/repository"
)

// BuildContainer は外部サービスとの接続を確立し、依存関係を組み立てた app.Container を返します。
func BuildContainer(ctx context.Context, cfg *config.Config) (container *app.Container, err error) {
	var resources []io.Closer
	defer func() {
		if err != nil {
			for _, r := range resources {
				if r != nil {
					_ = r.Close()
				}
			}
		}
	}()

	// 1. I/O Infrastructure (GCS)
	storage, closeErr := gcs.New(ctx)
	if closeErr != nil {
		return nil, fmt.Errorf("failed to create GCS factory: %w", closeErr)
	}
	resources = append(resources, storage)

	rio, ioErr := buildRemoteIO(storage)
	if ioErr != nil {
		return nil, fmt.Errorf("failed to initialize IO components: %w", ioErr)
	}

	httpClient := httpkit.New(config.DefaultHTTPTimeout)

	// 2. Characters
	characters, charErr := adapters.LoadCharacters(ctx, rio.Reader, cfg.Storage.CharactersJSONPath)
	if charErr != nil {
		return nil, fmt.Errorf("failed to load characters: %w", charErr)
	}

	// 3. go-comic-kit Operations
	ops, opsErr := buildOperations(ctx, cfg, rio, httpClient, characters)
	if opsErr != nil {
		return nil, fmt.Errorf("failed to initialize go-comic-kit operations: %w", opsErr)
	}

	// 4. Notifier (Slack)
	notifier, notifierErr := buildNotifier(httpClient.WithoutRetry(), cfg)
	if notifierErr != nil {
		return nil, fmt.Errorf("failed to initialize notifier: %w", notifierErr)
	}

	// 5. Worker Pipeline
	// Web プロセスは投入時の queued を、Worker プロセスは実行結果を書き込みます。
	jobStatus := repository.NewJobStatusRepository(cfg.Storage, rio.Reader, rio.Writer)
	pipelineRunner, pipeErr := buildPipeline(cfg, rio, ops, notifier, jobStatus)
	if pipeErr != nil {
		return nil, fmt.Errorf("failed to initialize pipeline: %w", pipeErr)
	}

	// 6. Task Enqueuer
	enqueuer, taskErr := buildTaskEnqueuer(ctx, cfg.Server, cfg.GCP)
	if taskErr != nil {
		return nil, fmt.Errorf("failed to initialize task enqueuer: %w", taskErr)
	}
	resources = append(resources, enqueuer)

	// 7. History Repository
	historyCache := repository.NewHistoryCache()
	repo := repository.NewComicRepository(cfg.Storage, rio.Reader, rio.Writer, historyCache)

	appCtx := &app.Container{
		Config:       cfg,
		RemoteIO:     rio,
		Ops:          ops,
		Pipeline:     pipelineRunner,
		TaskQueue:    enqueuer,
		Repository:   repo,
		JobStatus:    jobStatus,
		HistoryCache: historyCache,
		Characters:   characters,
		HTTPClient:   httpClient,
		Closers:      []io.Closer{rio, enqueuer},
	}

	return appCtx, nil
}
