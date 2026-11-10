// ap-story is the orchestrator service that drives AI comic generation jobs.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/shouni/gcp-kit/cloudlog"
	"github.com/shouni/go-utils/slogctx"

	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/server"
)

// logLevelEnvKey is the environment variable that sets the log level.
const logLevelEnvKey = "LOG_LEVEL"

func main() {
	// Logger setup (honours LOG_LEVEL; structured logs compatible with Cloud Logging).
	// cloudlog owns the output format and slogctx attaches context attributes;
	// the app only wires the two together.
	level := slogctx.ParseLevel(os.Getenv(logLevelEnvKey))
	slog.SetDefault(slog.New(cloudlog.NewHandler(os.Stdout, level)))

	if err := run(); err != nil {
		os.Exit(1)
	}
}

// run initialises the application and starts the server. The exit code is decided in
// main so that the deferred cleanup is not skipped by os.Exit.
func run() error {
	// Create a context that reacts to signals,
	// so that ctx.Done() is closed on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Load and validate the configuration.
	cfg, err := config.LoadConfig()
	if err != nil {
		slog.Error("Config load failed", "error", err)
		return err
	}
	if err := cfg.ValidateEssentialConfig(); err != nil {
		slog.Error("Config validation failed", "error", err)
		return err
	}
	cfg.WarnOnContradictoryGenerationSettings()

	// Run the server.
	if err := server.Run(ctx, cfg); err != nil {
		slog.Error("Application failed", "error", err)
		return err
	}
	return nil
}
