package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/shouni/gcp-kit/cloudrun"

	"github.com/shouni/ap-story/internal/builder"
	"github.com/shouni/ap-story/internal/config"
)

// Run builds, starts and manages the lifecycle of the server.
func Run(ctx context.Context, cfg *config.Config) error {
	appCtx, err := builder.BuildContainer(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to build application context: %w", err)
	}
	defer func() {
		slog.Info("♻️ Closing application context...")
		appCtx.Close()
	}()

	h, err := builder.BuildHandlers(appCtx)
	if err != nil {
		return fmt.Errorf("failed to build handlers: %w", err)
	}

	slog.InfoContext(ctx, "🚀 Server starting...", "port", cfg.Server.Port, "service_url", cfg.Server.ServiceURL)

	// Startup, waiting for signals and graceful shutdown (force-closing past the grace period) belong to cloudrun.
	return cloudrun.Serve(ctx, cloudrun.Config{
		Port:    cfg.Server.Port,
		Handler: NewRouter(h, cfg.GCP.ProjectID),
	})
}
