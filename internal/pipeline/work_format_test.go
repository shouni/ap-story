package pipeline

import (
	"context"
	"slices"
	"testing"

	"github.com/shouni/go-comic-kit/comic"

	"github.com/shouni/ap-story/internal/domain"
)

// This file covers what belongs to one work rather than to the deployment: its cast, its
// language, its ratio and its length, and the copy that carries them into another language.

// fixedFormats gives every named mode a format, as the builder's adapter over the templates does.
type fixedFormats map[string]ScriptFormat

func (f fixedFormats) ScriptFormat(mode string) ScriptFormat { return f[mode] }

func formatRunner(t *testing.T, store *memStore, outline *fakeOutline, chapter *fakeChapterScript, panel *fakePanel, page *fakePage) *Runner {
	t.Helper()
	r, err := New(Dependencies{
		Ops:    fullOps(outline, chapter, &fakeDesignSheet{}, panel, page),
		Reader: store,
		Writer: store,
		Copier: store,
		Bucket: "test-bucket",
		Layout: ImageLayout{AspectRatio: "3:4"},
		Formats: fixedFormats{
			"song_story": {MaxChapters: 1, MaxPanels: 6},
			"cast_mode":  {Cast: []string{"zundamon", "metan"}},
		},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	return r
}

func TestComposeComicCarriesTheWorkFormat(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	outline, chapter, panel, page := &fakeOutline{}, &fakeChapterScript{}, &fakePanel{}, &fakePage{}
	r := formatRunner(t, store, outline, chapter, panel, page)

	task := &domain.Task{
		Command: domain.TaskCommandComposeComic, JobID: "job-song", SourceText: "歌詞",
		ScriptMode: "song_story", Lang: domain.LangEnglish, AspectRatio: "9:16",
		CharacterIDs: []string{"singer"},
	}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	req := outline.lastReq
	if req.MaxChapters != 1 {
		t.Errorf("outline MaxChapters = %d, want the mode's 1", req.MaxChapters)
	}
	if !slices.Equal(req.CharacterIDs, []string{"singer"}) || req.Lang != "en" || req.AspectRatio != "9:16" {
		t.Errorf("outline request = cast %v, lang %q, ratio %q; want the Task's choices", req.CharacterIDs, req.Lang, req.AspectRatio)
	}
	if chapter.lastOpts.MaxPanels != 6 {
		t.Errorf("chapter MaxPanels = %d, want the mode's 6", chapter.lastOpts.MaxPanels)
	}
	if panel.lastBatchOpts.AspectRatio != "9:16" || page.lastBatchOpts.AspectRatio != "9:16" {
		t.Errorf("image ratio = panels %q, pages %q; want the work's 9:16, not the deployment's",
			panel.lastBatchOpts.AspectRatio, page.lastBatchOpts.AspectRatio)
	}
}

func TestComposeComicFallsBackToTheModeCastAndTheDeploymentRatio(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	outline := &fakeOutline{}
	r := formatRunner(t, store, outline, &fakeChapterScript{}, &fakePanel{}, &fakePage{})

	task := &domain.Task{
		Command: domain.TaskCommandComposeComic, JobID: "job-cast", SourceText: "x",
		ScriptMode: "cast_mode", StopAfterScript: true,
	}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if want := []string{"zundamon", "metan"}; !slices.Equal(outline.lastReq.CharacterIDs, want) {
		t.Errorf("cast = %v, want the mode's %v when the Task names none", outline.lastReq.CharacterIDs, want)
	}
	if outline.lastReq.AspectRatio != "3:4" {
		t.Errorf("ratio = %q, want the deployment's 3:4 recorded, so a later change of the default cannot redraw this work at another ratio", outline.lastReq.AspectRatio)
	}
	if outline.lastReq.MaxChapters != 0 {
		t.Errorf("MaxChapters = %d, want 0 (the deployment's limit) for a mode that declares none", outline.lastReq.MaxChapters)
	}
}

func TestLaterJobsReadTheFormatFromTheState(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	writeState(t, store, "job-later", &comic.MangaState{
		Version: comic.StateSchemaVersion, ScriptMode: "song_story", AspectRatio: "1:1", ImageModel: "img",
		Chapters: []comic.Chapter{{ID: "ch01"}},
		Panels:   []comic.Panel{{ID: "ch01-p01", ChapterID: "ch01", Page: 1}},
	})
	chapter, panel, page := &fakeChapterScript{}, &fakePanel{}, &fakePage{}
	r := formatRunner(t, store, &fakeOutline{}, chapter, panel, page)

	// The Tasks below carry no format at all: it comes from the state.
	for _, task := range []*domain.Task{
		{Command: domain.TaskCommandRegenerateChapterScript, JobID: "job-later", ChapterID: "ch01"},
		{Command: domain.TaskCommandRegeneratePanel, JobID: "job-later", PanelID: "ch01-p01"},
		{Command: domain.TaskCommandRegeneratePage, JobID: "job-later", Page: 1},
	} {
		if err := r.Run(context.Background(), task); err != nil {
			t.Fatalf("%s failed: %v", task.Command, err)
		}
	}
	if chapter.lastOpts.MaxPanels != 6 {
		t.Errorf("remade chapter MaxPanels = %d, want 6 from the recorded mode", chapter.lastOpts.MaxPanels)
	}
	if panel.lastOpts.AspectRatio != "1:1" || page.lastOpts.AspectRatio != "1:1" {
		t.Errorf("ratio = panel %q, page %q; want the state's 1:1", panel.lastOpts.AspectRatio, page.lastOpts.AspectRatio)
	}
}

func TestWorkWithoutARecordedRatioKeepsTheDeploymentDefault(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	writeState(t, store, "job-old", &comic.MangaState{
		Version: comic.StateSchemaVersion, ImageModel: "img",
		Panels: []comic.Panel{{ID: "ch01-p01", ChapterID: "ch01", Page: 1}},
	})
	panel := &fakePanel{}
	r := formatRunner(t, store, &fakeOutline{}, &fakeChapterScript{}, panel, &fakePage{})

	task := &domain.Task{Command: domain.TaskCommandRegeneratePanel, JobID: "job-old", PanelID: "ch01-p01"}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if panel.lastOpts.AspectRatio != "3:4" {
		t.Errorf("ratio = %q, want the deployment's 3:4 for a work made before the ratio was recorded", panel.lastOpts.AspectRatio)
	}
}

func sourceWork() *comic.MangaState {
	return &comic.MangaState{
		Version: comic.StateSchemaVersion, ID: "job-src", Title: "曲名", ScriptMode: "song_story",
		StyleMode: "cel_graphic", ImageModel: "img", AspectRatio: "3:4", Cast: []string{"singer"},
		Chapters: []comic.Chapter{{ID: "ch01", PanelIDs: []string{"ch01-p01", "ch01-p02"}}},
		Panels: []comic.Panel{
			{
				ID: "ch01-p01", ChapterID: "ch01", Page: 1,
				Dialogues:  []comic.DialogueLine{{Text: "夜明けまで", Kind: comic.DialogueKindLyric}},
				Generation: &comic.GenerationRecord{ImageURL: "gs://test-bucket/comics/job-src/images/panel_ch01-p01.png", UsedSeed: 7},
			},
			{ID: "ch01-p02", ChapterID: "ch01", Page: 1},
		},
		Pages: []comic.PageArtifact{{PageNumber: 1, PanelIDs: []string{"ch01-p01", "ch01-p02"},
			Generation: &comic.GenerationRecord{ImageURL: "gs://test-bucket/comics/job-src/images/comic_page_1.png"}}},
	}
}

func TestDuplicateComicCopiesTheScriptAndPanelsButNotThePages(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	writeState(t, store, "job-src", sourceWork())
	srcImage := "gs://test-bucket/comics/job-src/images/panel_ch01-p01.png"
	store.files[srcImage] = []byte("panel-bytes")
	r := formatRunner(t, store, &fakeOutline{}, &fakeChapterScript{}, &fakePanel{}, &fakePage{})

	task := &domain.Task{Command: domain.TaskCommandDuplicateComic, JobID: "job-en", SourceJobID: "job-src", Lang: domain.LangEnglish}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	copied := loadSavedState(t, store, "job-en")
	if copied == nil {
		t.Fatal("the copy was not saved")
	}
	if copied.ID != "job-en" || copied.Lang != "en" {
		t.Errorf("copy ID = %q, Lang = %q; want job-en and en", copied.ID, copied.Lang)
	}
	if copied.Title != "曲名" || copied.ScriptMode != "song_story" || copied.StyleMode != "cel_graphic" ||
		copied.AspectRatio != "3:4" || !slices.Equal(copied.Cast, []string{"singer"}) {
		t.Errorf("the copy lost part of the work's format: %+v", copied)
	}
	if len(copied.Pages) != 0 {
		t.Errorf("the copy kept %d page(s); pages hold the lettering and must be composed again", len(copied.Pages))
	}

	wantImage := "gs://test-bucket/comics/job-en/images/panel_ch01-p01.png"
	drawn := copied.Panels[0]
	if drawn.Generation == nil || drawn.Generation.ImageURL != wantImage || drawn.Generation.UsedSeed != 7 {
		t.Errorf("panel record = %+v, want it pointing at the copy's own image with the seed kept", drawn.Generation)
	}
	if string(store.files[wantImage]) != "panel-bytes" {
		t.Errorf("the panel image was not copied to %s", wantImage)
	}
	if copied.Panels[1].Generation != nil {
		t.Errorf("a panel never drawn gained a record: %+v", copied.Panels[1].Generation)
	}
	if got := drawn.Dialogues[0].Text; got != "夜明けまで" {
		t.Errorf("dialogue = %q, want the source's text carried over for translation", got)
	}

	// The source is untouched: its record still names its own image, and it still has its page.
	source := loadSavedState(t, store, "job-src")
	if source.Panels[0].Generation.ImageURL != srcImage || len(source.Pages) != 1 || source.Lang != "" {
		t.Errorf("the source work changed: %+v", source)
	}
}

func TestDuplicateComicLeavesAnExistingCopyAlone(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	writeState(t, store, "job-src", sourceWork())
	edited := sourceWork()
	edited.ID, edited.Lang = "job-en", "en"
	edited.Panels[0].Dialogues[0].Text = "Until dawn"
	writeState(t, store, "job-en", edited)
	r := formatRunner(t, store, &fakeOutline{}, &fakeChapterScript{}, &fakePanel{}, &fakePage{})

	// A redelivery after the copy's script was translated must not write the source's text back.
	task := &domain.Task{Command: domain.TaskCommandDuplicateComic, JobID: "job-en", SourceJobID: "job-src", Lang: domain.LangEnglish}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if got := loadSavedState(t, store, "job-en").Panels[0].Dialogues[0].Text; got != "Until dawn" {
		t.Errorf("dialogue = %q, want the translated line kept", got)
	}
}

func TestDuplicateComicFailsWhenTheSourceIsMissing(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	r := formatRunner(t, store, &fakeOutline{}, &fakeChapterScript{}, &fakePanel{}, &fakePage{})

	task := &domain.Task{Command: domain.TaskCommandDuplicateComic, JobID: "job-en", SourceJobID: "job-none"}
	if err := r.Run(context.Background(), task); err == nil {
		t.Fatal("Run succeeded without a source work")
	}
	if loadSavedState(t, store, "job-en") != nil {
		t.Error("a state was saved for a copy that never happened")
	}
}
