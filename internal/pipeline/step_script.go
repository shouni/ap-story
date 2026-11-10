package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/shouni/go-comic-kit/comic"

	"github.com/shouni/go-comic-kit/ports"
)

// OutlineStep generates a new MangaState holding only the chapter outline from the Task's input
// (SourceURL/SourceText) and sets it on pc.Manga.
type OutlineStep struct{}

// Name returns the step name.
func (OutlineStep) Name() string { return "outline" }

// Execute generates a new MangaState holding only the chapter outline.
// When an outline already exists (a re-run from redelivery), it does nothing so as not to discard generated output.
func (OutlineStep) Execute(ctx context.Context, pc *Context) error {
	if pc.Manga != nil && len(pc.Manga.Chapters) > 0 {
		slog.InfoContext(ctx, "outline already exists, skipping", "chapters", len(pc.Manga.Chapters))
		return nil
	}

	manga, err := pc.Ops.Outline.GenerateOutline(ctx, ports.OutlineRequest{
		SourceURL:  pc.Task.SourceURL,
		SourceText: pc.Task.SourceText,
		Mode:       pc.Task.ScriptMode,
		StyleMode:  pc.Task.StyleMode,
		Model:      pc.Task.TextModel,
	})
	if err != nil {
		return fmt.Errorf("outline: %w", err)
	}
	manga.ID = pc.Task.JobID
	// Note the model chosen for this work in the state. When the script is made first and images later,
	// or a chapter is remade, the Task carries no choice, so this is where it is inherited from
	// (the same reason the kit records StyleMode / ScriptMode).
	manga.TextModel = pc.Task.TextModel
	manga.ImageModel = pc.Task.ModelOverride
	pc.Manga = manga
	return nil
}

// AllChapterScriptsStep generates the name (panels) for every chapter of pc.Manga.
// It is used by compose_comic's batch generation.
type AllChapterScriptsStep struct{}

// Name returns the step name.
func (AllChapterScriptsStep) Name() string { return "chapter_scripts" }

// Execute generates the name for every chapter of pc.Manga.
func (AllChapterScriptsStep) Execute(ctx context.Context, pc *Context) error {
	if pc.Manga == nil {
		return fmt.Errorf("chapter_scripts: manga state is nil")
	}
	// GenerateChapterScript rewrites pc.Manga and returns it, so fix the chapter IDs first and iterate over them
	// (so the targets do not shift even if the order of pc.Manga.Chapters changes during the loop).
	// Chapters that already have a name are skipped. GenerateChapterScript replaces a chapter's panels
	// whole, so remaking it would also erase the records of those panels' generated images.
	var chapterIDs []string
	for _, ch := range pc.Manga.Chapters {
		if chapterHasPanels(pc.Manga, ch.ID) {
			continue
		}
		chapterIDs = append(chapterIDs, ch.ID)
	}
	if len(chapterIDs) == 0 {
		return nil
	}

	// Have the same model write the outline and every chapter (a different writer per chapter makes the style uneven).
	opts := ports.ChapterScriptOptions{Model: pc.textModel()}
	for _, chapterID := range chapterIDs {
		manga, err := pc.Ops.ChapterScript.GenerateChapterScript(ctx, pc.Manga, chapterID, opts)
		if err != nil {
			return fmt.Errorf("chapter_scripts: chapter %q: %w", chapterID, err)
		}
		pc.Manga = manga
	}
	return nil
}

// SingleChapterScriptStep generates or regenerates the name for only the chapter in Task.ChapterID.
// It is used by regenerate_chapter_script.
type SingleChapterScriptStep struct{}

// Name returns the step name.
func (SingleChapterScriptStep) Name() string { return "chapter_script" }

// Execute generates or regenerates the name for only the chapter in Task.ChapterID.
func (SingleChapterScriptStep) Execute(ctx context.Context, pc *Context) error {
	// Inherit the model recorded in the state so that the chapter being remade is not written by a different model.
	opts := ports.ChapterScriptOptions{Model: pc.textModel()}
	manga, err := pc.Ops.ChapterScript.GenerateChapterScript(ctx, pc.Manga, pc.Task.ChapterID, opts)
	if err != nil {
		return fmt.Errorf("chapter_script: chapter %q: %w", pc.Task.ChapterID, err)
	}
	pc.Manga = manga
	return nil
}

// chapterHasPanels reports whether the given chapter's name has already been generated.
func chapterHasPanels(state *comic.MangaState, chapterID string) bool {
	for i := range state.Panels {
		if state.Panels[i].ChapterID == chapterID {
			return true
		}
	}
	return false
}
