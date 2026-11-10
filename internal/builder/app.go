// Package builder provides the factory functions that assemble the service clients and the
// dependencies of the DI container from the configuration.
package builder

import (
	"context"
	"fmt"
	"io"

	"github.com/shouni/gcp-kit/auth/session"
	"github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-remote-io/remoteio/gcs"

	"github.com/shouni/ap-story/internal/adapters"
	"github.com/shouni/ap-story/internal/app"
	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
	"github.com/shouni/ap-story/internal/repository"
)

// BuildContainer connects to the external services and returns an app.Container with its dependencies assembled.
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
	// resources is for unwinding a failed assembly; nobody looks at it after a successful return.
	// Releasing at run time belongs to closers (app.Container.Close), so a resource that
	// outlives a successful build goes into both. The storage's lifetime belongs to the factory
	// (Bundle.Close closes the factory).
	closers := []io.Closer{storage}

	httpClient := httpkit.New()

	// 2. Characters
	characters, charErr := adapters.LoadCharacters(ctx, store, cfg.Storage.CharactersJSONPath)
	if charErr != nil {
		return nil, fmt.Errorf("failed to load characters: %w", charErr)
	}

	// 3. Job Status
	// Needed regardless of role: the web process writes `queued` at enqueue time and the
	// worker process writes the outcome.
	jobStatus := repository.NewJobStatusRepository(cfg.Storage, store)

	// 4. go-comic-kit Operations、Notifier、Worker Pipeline
	// Only the worker face runs generation. Not assembling it on the web face keeps the
	// web face free of the Vertex AI client and the Slack webhook
	// (ap-story-web-runner has neither aiplatform.user nor access to SLACK_WEBHOOK_URL,
	// and there is no reason to give it either).
	//
	// Both stay nil in the Container on the web face.
	// The only reader of Pipeline is the ServesWorker branch of BuildHandlers.
	//
	// pipelineRunner is declared as domain.Pipeline rather than the concrete *pipeline.Runner
	// because assigning a nil pointer to an interface field makes the interface itself non-nil
	// even though its content is nil, which would defeat the nil check on the web face.
	var ops *ports.Operations
	var pipelineRunner domain.Pipeline
	if cfg.Server.Role.ServesWorker() {
		builtOps, opsErr := buildOperations(ctx, cfg, store, httpClient, characters)
		if opsErr != nil {
			return nil, fmt.Errorf("failed to initialize go-comic-kit operations: %w", opsErr)
		}
		// Since go-comic-kit v1.6.0, Operations holds no resource that needs releasing
		// (the reference-image cache now expires on read, so its cleanup goroutine is gone).
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
	// Only the web face enqueues tasks. The worker face is the receiving side, so not
	// assembling it there keeps it free of an unused Cloud Tasks client and of
	// CLOUD_TASKS_QUEUE_ID.
	//
	// Assigning a nil pointer to domain.TaskQueue produces "a non-nil interface holding nil",
	// which slips past the nil check in Closers and calls Close on a nil receiver.
	// To avoid that, the interface and Closers are filled only when a client was built.
	var taskQueue domain.TaskQueue
	var sessionStore session.Store
	if cfg.Server.Role.ServesWeb() {
		// Sessions live in a database separate from job status (SessionDatabase).
		// The store owns the client's lifetime, so the store is what goes into Closers.
		owned, fsErr := session.OpenFirestoreStore(ctx, cfg.GCP.ProjectID, cfg.Auth.SessionDatabase, cfg.Auth.SessionCollection)
		if fsErr != nil {
			return nil, fmt.Errorf("failed to build the session store: %w", fsErr)
		}
		resources = append(resources, owned)
		closers = append(closers, owned)
		sessionStore = owned

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
