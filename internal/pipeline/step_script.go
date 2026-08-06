package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/shouni/go-comic-kit/ports"
)

// OutlineStep は、Task の入力（SourceURL/SourceText）から章立てのみを持つ
// 新しい MangaState を生成し、pc.Manga にセットします。
type OutlineStep struct{}

// Name はステップ名を返します。
func (OutlineStep) Name() string { return "outline" }

// Execute は章立てのみを持つ新しい MangaState を生成します。
// すでに章立てがある場合（再配信による再実行）は、生成済みの成果を捨てないよう何もしません。
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
	})
	if err != nil {
		return fmt.Errorf("outline: %w", err)
	}
	manga.ID = pc.Task.JobID
	pc.Manga = manga
	return nil
}

// AllChapterScriptsStep は、pc.Manga の全章についてネーム（パネル群）を生成します。
// compose_comic の一括生成で使います。
type AllChapterScriptsStep struct{}

// Name はステップ名を返します。
func (AllChapterScriptsStep) Name() string { return "chapter_scripts" }

// Execute は pc.Manga の全章についてネームを生成します。
func (AllChapterScriptsStep) Execute(ctx context.Context, pc *Context) error {
	if pc.Manga == nil {
		return fmt.Errorf("chapter_scripts: manga state is nil")
	}
	// GenerateChapterScript は pc.Manga を書き換えて返すため、章IDを先に固定して列挙する
	// （ループ中に pc.Manga.Chapters の並びが変わっても対象がぶれないようにする）。
	// すでにネームがある章は飛ばす。GenerateChapterScript は章のパネルを丸ごと
	// 置き換えるため、作り直すとそのコマの生成済み画像の記録まで消える。
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

	for _, chapterID := range chapterIDs {
		manga, err := pc.Ops.ChapterScript.GenerateChapterScript(ctx, pc.Manga, chapterID)
		if err != nil {
			return fmt.Errorf("chapter_scripts: chapter %q: %w", chapterID, err)
		}
		pc.Manga = manga
	}
	return nil
}

// SingleChapterScriptStep は、Task.ChapterID の1章のみネームを生成/再生成します。
// regenerate_chapter_script で使います。
type SingleChapterScriptStep struct{}

// Name はステップ名を返します。
func (SingleChapterScriptStep) Name() string { return "chapter_script" }

// Execute は Task.ChapterID の1章のみネームを生成/再生成します。
func (SingleChapterScriptStep) Execute(ctx context.Context, pc *Context) error {
	manga, err := pc.Ops.ChapterScript.GenerateChapterScript(ctx, pc.Manga, pc.Task.ChapterID)
	if err != nil {
		return fmt.Errorf("chapter_script: chapter %q: %w", pc.Task.ChapterID, err)
	}
	pc.Manga = manga
	return nil
}

// chapterHasPanels は、指定章のネームがすでに生成済みかを返します。
func chapterHasPanels(state *ports.MangaState, chapterID string) bool {
	for i := range state.Panels {
		if state.Panels[i].ChapterID == chapterID {
			return true
		}
	}
	return false
}
