package builder

import (
	"context"
	"fmt"

	characterkit "github.com/shouni/go-character-kit/character"
	"github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-comic-kit/workflow"
	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/adapters"
	"github.com/shouni/ap-story/internal/adapters/prompts"
	"github.com/shouni/ap-story/internal/config"
)

// buildOperations は、go-comic-kit の全操作（章立て・章台本・デザインシート・
// パネル・ページ）を workflow.New で組み立てます。AI クライアントは Vertex AI を使い、
// GCS URI を直接参照できるようにして File API アップロードのコストを抑えます。
func buildOperations(
	ctx context.Context,
	cfg *config.Config,
	store remoteio.Store,
	downloader httpkit.Streamer,
	characters *characterkit.Characters,
) (*ports.Operations, error) {
	aiClient, err := adapters.NewVertexAIAdapter(ctx, cfg.GCP)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Vertex AI adapter: %w", err)
	}

	scriptPrompts, err := prompts.NewScriptPrompts()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ap-story script prompts: %w", err)
	}

	styles, err := prompts.NewStyles()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ap-story styles: %w", err)
	}

	ops, err := workflow.New(workflow.Args{
		Config:              cfg.AI.KitConfig(),
		Downloader:          downloader,
		Reader:              store,
		Writer:              store,
		AIClient:            aiClient,
		Characters:          characters,
		OutlinePrompt:       scriptPrompts,
		ChapterScriptPrompt: scriptPrompts,
		DesignSheetPrompt:   prompts.DesignPrompt{Styles: styles},
		PanelPrompt:         prompts.PanelPrompt{Styles: styles},
		PagePrompt:          prompts.PagePrompt{Styles: styles},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize go-comic-kit operations: %w", err)
	}

	return ops, nil
}
