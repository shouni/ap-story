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
	"github.com/shouni/ap-story/internal/pipeline"
)

// scriptFormats hands the pipeline each script mode's format from the same templates the kit's
// prompts are built from. The two must come from one load: a format read from another copy of the
// templates could cap a chapter at four panels while the prompt asks for eight.
type scriptFormats struct {
	prompts *prompts.ScriptPrompts
}

func (f scriptFormats) ScriptFormat(mode string) pipeline.ScriptFormat {
	info := f.prompts.ModeInfo(mode)
	return pipeline.ScriptFormat{Cast: info.Cast, MaxChapters: info.MaxChapters, MaxPanels: info.MaxPanels}
}

// buildOperations assembles every go-comic-kit operation (chapters, chapter script, design sheet,
// panels, pages) with workflow.New, and returns the script modes' formats alongside. The AI client uses Vertex AI so that GCS URIs can be
// referenced directly, avoiding the cost of File API uploads.
func buildOperations(
	ctx context.Context,
	cfg *config.Config,
	store remoteio.Store,
	downloader httpkit.Streamer,
	characters *characterkit.Characters,
) (*ports.Operations, pipeline.ScriptFormats, error) {
	aiClient, err := adapters.NewVertexAIAdapter(ctx, cfg.GCP)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize Vertex AI adapter: %w", err)
	}

	scriptPrompts, err := prompts.NewScriptPrompts()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize ap-story script prompts: %w", err)
	}

	styles, err := prompts.NewStyles()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize ap-story styles: %w", err)
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
		return nil, nil, fmt.Errorf("failed to initialize go-comic-kit operations: %w", err)
	}

	return ops, scriptFormats{prompts: scriptPrompts}, nil
}
