// Package pipeline provides the worker pipeline that runs go-comic-kit operations in order
// according to the command of a domain.Task.
package pipeline

import (
	"github.com/shouni/go-comic-kit/comic"
	"github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/domain"
)

// State holds the values that change while the pipeline runs.
type State struct {
	Task *domain.Task
	// Manga is the MangaState being worked on. Steps read and write it.
	// It is created from nil only by the first step of compose_comic (Outline).
	Manga *comic.MangaState
	// OutputDir is the GCS URI holding this job's artifacts. For a comic job it points at
	// comics/{jobID}. For a standalone design sheet whose comic state is not found,
	// LoadStateStepOptional switches it to DesignJobOutputDir.
	OutputDir string
	// DesignJobOutputDir is where the state of a standalone design-sheet job is saved
	// (design-jobs/{jobID}).
	DesignJobOutputDir string
	// CharacterOutputDir is the bucket root URI where shared character assets
	// (design sheets and so on) that belong to no job are saved.
	CharacterOutputDir string
}

// ImageLayout is the aspect ratio and resolution of generated images.
// go-comic-kit does not hold them as configuration; it receives them on every generation.
type ImageLayout struct {
	// AspectRatio is shared by panels, pages and design sheets.
	// If they differ, the reference-image consistency silently stops working, so it is one value.
	AspectRatio string
	// PanelImageSize / PageImageSize are the resolutions. They are separate because cost applies per panel.
	PanelImageSize string
	PageImageSize  string
}

// Services are external dependencies fixed for the duration of a run. Steps only refer to them
// and never rewrite them.
type Services struct {
	Ops    *ports.Operations
	Reader ports.ContentReader
	Writer remoteio.Writer
	// Layout is the aspect ratio and resolution of generated images.
	Layout ImageLayout
}

// textModel returns the text model used for script generation in this run.
// The Task's choice wins; otherwise it inherits what the state records (the model that wrote this work).
// When both are empty it returns the empty string and the worker's configuration (head of GEMINI_MODELS) is used.
//
// Inheriting it keeps a chapter script remade later from being written by a different model than the other chapters.
func (pc *Context) textModel() string {
	if pc.Task != nil && pc.Task.TextModel != "" {
		return pc.Task.TextModel
	}
	if pc.Manga != nil {
		return pc.Manga.TextModel
	}
	return ""
}

// imageModel returns the model used for image generation in this run, recording it in the state if nothing is recorded yet.
//
// It is recorded because the place where the image model is chosen moved from the script form to
// "Generate panels" on the work's detail page. It is undecided at the script stage and set by the first image generation.
// Without the record, the next chapter's button would go out with no model name and fail with ErrInvalidRequest.
// The resolution rule itself is the same as textModel (the Task's choice → the state's record).
func (pc *Context) imageModel() string {
	model := pc.resolveImageModel()
	if model != "" && pc.Manga != nil && pc.Manga.ImageModel == "" {
		pc.Manga.ImageModel = model
	}
	return model
}

func (pc *Context) resolveImageModel() string {
	if pc.Task != nil && pc.Task.ModelOverride != "" {
		return pc.Task.ModelOverride
	}
	if pc.Manga != nil {
		return pc.Manga.ImageModel
	}
	return ""
}

// styleMode returns the art-style mode used in this run.
// The art style is the look of the whole work, so it reads the state (the value chosen at outline time), not the Task.
// That way the same style is used even for "generate up to the script → generate images later".
//
// Resolving it into the style wording and the negative prompt is done by the prompt implementation
// (Styles in internal/adapters/prompts). The pipeline only carries the name.
func (pc *Context) styleMode() string {
	if pc.Manga == nil {
		return ""
	}
	return pc.Manga.StyleMode
}

// Context is the execution context handed between the pipeline's steps.
// Field promotion through embedding allows flat access such as pc.Task / pc.Manga.
type Context struct {
	State
	Services
}
