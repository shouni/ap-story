// Package builder は、設定値から各サービスクライアント・DI コンテナの
// 依存関係を組み立てるファクトリ関数を提供します。
package builder

import (
	"context"
	"fmt"
	"io"

	"cloud.google.com/go/firestore"
	"github.com/shouni/gcp-kit/auth/session"
	"github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-remote-io/remoteio/gcs"

	"github.com/shouni/ap-story/internal/adapters"
	"github.com/shouni/ap-story/internal/app"
	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
	"github.com/shouni/ap-story/internal/pipeline"
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

	store, ioErr := storage.Store()
	if ioErr != nil {
		return nil, fmt.Errorf("failed to initialize IO components: %w", ioErr)
	}
	// resources は組み立て中の巻き戻し用で、成功して返ったあとは誰も見ません。
	// 実行中の解放は closers（app.Container.Close）が受け持つため、成功後も
	// 生き続ける資源は両方へ入れます。ストレージの寿命はファクトリが持ちます
	// （Bundle.Close が factory を閉じます）。
	closers := []io.Closer{storage}

	httpClient := httpkit.New()

	// 2. Characters
	characters, charErr := adapters.LoadCharacters(ctx, store, cfg.Storage.CharactersJSONPath)
	if charErr != nil {
		return nil, fmt.Errorf("failed to load characters: %w", charErr)
	}

	// 3. Job Status
	// Web プロセスは投入時の queued を、Worker プロセスは実行結果を書き込むため、
	// 役割によらず必要です。
	jobStatus := repository.NewJobStatusRepository(cfg.Storage, store)

	// 4. go-comic-kit Operations、Notifier、Worker Pipeline
	// 生成を実行するのは Worker 面だけです。Web 面で組み立てないことで、
	// Vertex AI クライアントと Slack Webhook への依存を持たずに済みます
	// （ap-story-web-runner には aiplatform.user も SLACK_WEBHOOK_URL への
	// アクセス権も無く、持たせる理由がありません）。
	//
	// どちらもポインタなので、Web 面では nil のまま Container に入ります。
	// Pipeline を参照するのは BuildHandlers の ServesWorker 分岐だけです。
	var ops *ports.Operations
	var pipelineRunner *pipeline.Runner
	if cfg.Server.Role.ServesWorker() {
		builtOps, opsErr := buildOperations(ctx, cfg, store, httpClient, characters)
		if opsErr != nil {
			return nil, fmt.Errorf("failed to initialize go-comic-kit operations: %w", opsErr)
		}
		// go-comic-kit v1.6.0 から Operations は解放すべきリソースを持ちません
		// （参照画像のキャッシュが読み出し時失効になり、掃除用 goroutine が消えたため）。
		ops = builtOps

		notifier, notifierErr := buildNotifier(httpClient.WithoutRetry(), cfg)
		if notifierErr != nil {
			return nil, fmt.Errorf("failed to initialize notifier: %w", notifierErr)
		}

		runner, pipeErr := buildPipeline(cfg, store, ops, notifier, jobStatus)
		if pipeErr != nil {
			return nil, fmt.Errorf("failed to initialize pipeline: %w", pipeErr)
		}
		pipelineRunner = runner
	}

	// 5. Task Enqueuer
	// タスクを投入するのは Web 面だけです。Worker 面は受け取る側なので、
	// 組み立てないことで未使用の Cloud Tasks クライアントと CLOUD_TASKS_QUEUE_ID への
	// 依存を持たずに済みます。
	//
	// nil のポインタを domain.TaskQueue に代入すると「非 nil のインターフェースが nil を保持する」
	// 状態になり、Closers の nil チェックをすり抜けて nil レシーバーで Close が呼ばれます。
	// それを避けるため、組み立てたときにだけインターフェースと Closers へ入れます。
	var taskQueue domain.TaskQueue
	var sessionStore session.Store
	if cfg.Server.Role.ServesWeb() {
		// セッションはジョブ状態とは別のデータベースに置きます（SessionDatabase）。
		fsClient, fsErr := firestore.NewClientWithDatabase(ctx, cfg.GCP.ProjectID, cfg.Auth.SessionDatabase)
		if fsErr != nil {
			return nil, fmt.Errorf("セッション用 Firestore の初期化に失敗しました: %w", fsErr)
		}
		resources = append(resources, fsClient)
		closers = append(closers, fsClient)

		sessionStore, fsErr = session.NewFirestoreStore(session.FirestoreConfig{
			Client:      fsClient,
			Collection:  cfg.Auth.SessionCollection,
			StoreConfig: session.StoreConfig{Secure: cfg.IsSecureServiceURL()},
		})
		if fsErr != nil {
			return nil, fmt.Errorf("セッションストアの構築に失敗しました: %w", fsErr)
		}

		enqueuer, taskErr := buildTaskEnqueuer(ctx, cfg)
		if taskErr != nil {
			return nil, fmt.Errorf("failed to initialize task enqueuer: %w", taskErr)
		}
		resources = append(resources, enqueuer)
		closers = append(closers, enqueuer)
		taskQueue = enqueuer
	}

	// 6. History Repository
	historyCache := repository.NewHistoryCache()
	repo := repository.NewComicRepository(cfg.Storage, store, historyCache)

	appCtx := &app.Container{
		Config:       cfg,
		Store:        store,
		Ops:          ops,
		Pipeline:     pipelineRunner,
		TaskQueue:    taskQueue,
		SessionStore: sessionStore,
		Repository:   repo,
		JobStatus:    jobStatus,
		HistoryCache: historyCache,
		Characters:   characters,
		HTTPClient:   httpClient,
		Closers:      closers,
	}

	return appCtx, nil
}
