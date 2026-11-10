// Package app provides the DI container that assembles and holds the application's dependencies.
package app

import (
	"io"
	"log/slog"

	characterkit "github.com/shouni/go-character-kit/character"
	"github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-job-kit/cache"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
	"github.com/shouni/gcp-kit/auth/session"
)

// Container holds the application's dependencies (the DI container).
type Container struct {
	Config *config.Config
	// I/O and Storage
	// Store is the window for reads, writes, listings and signing. Its lifetime belongs to the factory.
	Store remoteio.Store
	// Every go-comic-kit operation (chapters, chapter script, design sheet, panels, pages)
	Ops *ports.Operations
	// Pipeline is the worker pipeline that runs the Ops operation selected by the Task's command.
	Pipeline domain.Pipeline
	// TaskQueue enqueues asynchronous jobs (Task) onto Cloud Tasks.
	TaskQueue domain.TaskQueue
	// SessionStore is where login sessions are kept. Only the web face has one.
	SessionStore session.Store
	// Repository lists, reads and deletes the comic history.
	Repository domain.ComicRepository
	// JobStatus records and reads job progress.
	JobStatus domain.JobStatusStore
	// HistoryCache is the TTL cache of history summaries used by Repository.
	// Container holds it so that Close can stop it.
	HistoryCache *cache.TTL[domain.ComicHistory]
	// Characters holds the go-character-kit character definitions.
	Characters *characterkit.Characters
	// HTTPClient is the general-purpose client for outbound HTTP (go-web-reader and so on).
	HTTPClient httpkit.HTTPClient
	// Closers are the resources opened during assembly; Container.Close closes them together.
	// Close does not look at individual fields so that a new resource only needs the builder
	// to append it. HistoryCache alone has a Close that returns no error and so does not
	// satisfy io.Closer; it is closed separately at the end.
	Closers []io.Closer
}

// Close safely releases every external connection the Container holds.
//
// It returns no error because its only caller is the single defer in server.Run, and a
// returned error would have nowhere to go but slog.Error.
func (c *Container) Close() {
	for _, closer := range c.Closers {
		if closer == nil {
			continue
		}
		if err := closer.Close(); err != nil {
			slog.Error("failed to close resource", "error", err)
		}
	}
	if c.HistoryCache != nil {
		c.HistoryCache.Close()
	}
}
