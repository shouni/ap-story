// Package handlers provides the web face's HTTP handlers, which answer JSON and HTML
// on the same routes.
package handlers

import (
	"context"
	"fmt"
	"html/template"
	"strings"
	"time"

	characterkit "github.com/shouni/go-character-kit/character"

	"github.com/shouni/ap-story/internal/adapters/prompts"
	"github.com/shouni/ap-story/internal/domain"
)

// Signer represents only the signing that image redirects need.
// remoteio.Store satisfies it as is.
type Signer interface {
	SignURL(ctx context.Context, name, method string, expires time.Duration) (string, error)
}

// Handler holds the dependencies shared by the API/web UI handlers.
type Handler struct {
	taskQueue  domain.TaskQueue
	repository domain.ComicRepository
	// jobStatus is where job progress is recorded and read. Unset, the status features are disabled.
	jobStatus  domain.JobStatusStore
	signer     Signer
	bucket     string
	templates  map[string]*template.Template
	characters *characterkit.Characters
	// imageModels / geminiModels are the generation forms' model options (the head is the default).
	// When empty, no options are shown. They are also the allowlist used to validate the JSON API.
	imageModels  []string
	geminiModels []string
	// scriptModes / styleModes are the comic generation form's script-mode and style-mode
	// options (the templates under assets/prompts and their front matter).
	scriptModes []prompts.ModeInfo
	styleModes  []prompts.ModeInfo
}

// HandlerOptions are the construction options for a Handler, passed to NewHandler.
type HandlerOptions struct {
	TaskQueue    domain.TaskQueue
	Repository   domain.ComicRepository
	JobStatus    domain.JobStatusStore
	Signer       Signer
	Bucket       string
	Characters   *characterkit.Characters
	ImageModels  []string
	GeminiModels []string
	ScriptModes  []prompts.ModeInfo
	StyleModes   []prompts.ModeInfo
}

// NewHandler initialises a new handler from the given configuration.
func NewHandler(opts HandlerOptions) (*Handler, error) {
	if opts.TaskQueue == nil {
		return nil, fmt.Errorf("task queue is required")
	}
	if opts.Repository == nil {
		return nil, fmt.Errorf("repository is required")
	}
	if opts.Signer == nil {
		return nil, fmt.Errorf("signer is required")
	}
	bucket := strings.TrimSpace(opts.Bucket)
	if bucket == "" {
		return nil, fmt.Errorf("bucket is required")
	}
	if opts.Characters == nil {
		return nil, fmt.Errorf("characters is required")
	}
	templates, err := loadTemplateCache()
	if err != nil {
		return nil, fmt.Errorf("failed to load HTML templates: %w", err)
	}
	return &Handler{
		taskQueue:    opts.TaskQueue,
		repository:   opts.Repository,
		jobStatus:    opts.JobStatus,
		signer:       opts.Signer,
		bucket:       bucket,
		templates:    templates,
		characters:   opts.Characters,
		imageModels:  opts.ImageModels,
		geminiModels: opts.GeminiModels,
		scriptModes:  opts.ScriptModes,
		styleModes:   opts.StyleModes,
	}, nil
}
