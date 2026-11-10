package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/shouni/go-comic-kit/comic"

	"github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/domain"
)

// --- Fakes ---

type fakeOutline struct {
	called    int
	lastModel string
}

func (f *fakeOutline) GenerateOutline(_ context.Context, req ports.OutlineRequest) (*comic.MangaState, error) {
	f.called++
	f.lastModel = req.Model
	// Like the real one, copy the given mode into the state and return it (later stages read it from here).
	return &comic.MangaState{
		Version:    comic.StateSchemaVersion,
		Title:      "title from " + req.SourceText,
		ScriptMode: req.Mode,
		StyleMode:  req.StyleMode,
		Chapters:   []comic.Chapter{{ID: "ch01", Title: "第一章"}, {ID: "ch02", Title: "第二章"}},
	}, nil
}

type fakeChapterScript struct {
	calledChapters []string
	lastModel      string
}

func (f *fakeChapterScript) GenerateChapterScript(_ context.Context, state *comic.MangaState, chapterID string, opts ports.ChapterScriptOptions) (*comic.MangaState, error) {
	f.calledChapters = append(f.calledChapters, chapterID)
	f.lastModel = opts.Model
	state.Panels = append(state.Panels, comic.Panel{ID: chapterID + "-p01", ChapterID: chapterID, Page: len(f.calledChapters)})
	return state, nil
}

type fakeDesignSheet struct{ called int }

func (f *fakeDesignSheet) GenerateDesignSheet(_ context.Context, state *comic.MangaState, req ports.DesignSheetRequest) (*comic.MangaState, error) {
	f.called++
	if state == nil {
		state = &comic.MangaState{Version: comic.StateSchemaVersion}
	}
	for _, id := range req.CharacterIDs {
		state.SetDesignSheet(comic.DesignSheetRef{CharacterID: id, ImageURL: "gs://out/design_" + id + ".png"})
	}
	return state, nil
}

type fakePanel struct {
	calledPanels  []string
	lastOpts      ports.GenerateOptions
	lastBatchOpts ports.BatchOptions
}

func (f *fakePanel) GeneratePanel(_ context.Context, state *comic.MangaState, panelID string, opts ports.GenerateOptions) (*comic.MangaState, error) {
	f.calledPanels = append(f.calledPanels, panelID)
	f.lastOpts = opts
	p := state.PanelByID(panelID)
	if p != nil {
		p.Generation = &comic.GenerationRecord{ImageURL: "gs://out/" + panelID + ".png"}
	}
	return state, nil
}

// GenerateAllPanels imitates batch generation by repeating single generations.
// Checking concurrency is go-comic-kit's tests' job, so this only looks at what is called.
func (f *fakePanel) GenerateAllPanels(ctx context.Context, state *comic.MangaState, opts ports.BatchOptions) (*comic.MangaState, error) {
	f.lastBatchOpts = opts
	for i := range state.Panels {
		if opts.SkipGenerated && state.Panels[i].Generation != nil {
			continue
		}
		var err error
		state, err = f.GeneratePanel(ctx, state, state.Panels[i].ID,
			ports.GenerateOptions{Seed: opts.Seed, Model: opts.Model, OutputDir: opts.OutputDir})
		if err != nil {
			return state, err
		}
	}
	return state, nil
}

type fakePage struct {
	calledPages   []int
	lastOpts      ports.GenerateOptions
	lastBatchOpts ports.BatchOptions
}

func (f *fakePage) ComposePage(_ context.Context, state *comic.MangaState, page int, opts ports.GenerateOptions) (*comic.MangaState, error) {
	f.calledPages = append(f.calledPages, page)
	f.lastOpts = opts
	state.SetPageArtifact(comic.PageArtifact{PageNumber: page, Generation: &comic.GenerationRecord{
		ImageURL: fmt.Sprintf("gs://out/page_%d.png", page),
	}})
	return state, nil
}

// ComposeAllPages imitates batch composition by repeating single compositions.
func (f *fakePage) ComposeAllPages(ctx context.Context, state *comic.MangaState, opts ports.BatchOptions) (*comic.MangaState, error) {
	f.lastBatchOpts = opts
	seen := map[int]struct{}{}
	pages := make([]int, 0, len(state.Panels))
	for i := range state.Panels {
		if _, ok := seen[state.Panels[i].Page]; ok {
			continue
		}
		seen[state.Panels[i].Page] = struct{}{}
		pages = append(pages, state.Panels[i].Page)
	}
	slices.Sort(pages)

	for _, page := range pages {
		if opts.SkipGenerated {
			if existing := state.PageArtifactByNumber(page); existing != nil && existing.Generation != nil {
				continue
			}
		}
		var err error
		state, err = f.ComposePage(ctx, state, page,
			ports.GenerateOptions{Seed: opts.Seed, Model: opts.Model, OutputDir: opts.OutputDir})
		if err != nil {
			return state, err
		}
	}
	return state, nil
}

// memStore is an in-memory fake satisfying both ports.ContentReader and remoteio.Writer.
type memStore struct {
	files map[string][]byte
	// writes is the number of writes per path, used to check the per-stage checkpoint saves.
	writes map[string]int
}

func newMemStore() *memStore {
	return &memStore{files: map[string][]byte{}, writes: map[string]int{}}
}

// writeCount returns the number of writes to the given path.
func (m *memStore) writeCount(path string) int { return m.writes[path] }

func (m *memStore) Open(_ context.Context, path string) (io.ReadCloser, error) {
	data, ok := m.files[path]
	if !ok {
		// As in remoteio, a missing object is wrapped in fs.ErrNotExist. It used to return io.ErrUnexpectedEOF,
		// and the tests passed even for an implementation that did not tell "missing" from "unreadable".
		return nil, fmt.Errorf("open %s: %w", path, fs.ErrNotExist)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *memStore) Write(ctx context.Context, path string, r io.Reader, _ ...remoteio.WriteOption) error {
	// Honour the context, as a real GCS write does. Without this, saving on failure with an expired
	// context would slip through and the tests could not detect the regression.
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.files[path] = data
	m.writes[path]++
	return nil
}

// fakeNotifier records NotifyComplete/NotifyError calls for assertions.
type fakeNotifier struct {
	completed []domain.Task
	failed    []domain.Task
	failedErr []error
}

// Sending to Slack is HTTP, so it honours the context (the same reason as fakeJobStatusStore.Save).
// Ignoring it here would let the tests pass even when the notice of a job finishing at the deadline
// is sent with an expired context.
func (f *fakeNotifier) NotifyComplete(ctx context.Context, task domain.Task) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.completed = append(f.completed, task)
	return nil
}

func (f *fakeNotifier) NotifyError(ctx context.Context, task domain.Task, cause error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.failed = append(f.failed, task)
	f.failedErr = append(f.failedErr, cause)
	return nil
}

// --- Helpers ---

func newTestRunner(t *testing.T, store *memStore, ops *ports.Operations) *Runner {
	t.Helper()
	return newTestRunnerWithNotifier(t, store, ops, nil)
}

func newTestRunnerWithNotifier(t *testing.T, store *memStore, ops *ports.Operations, notifier domain.Notifier) *Runner {
	t.Helper()
	r, err := New(Dependencies{
		Ops:      ops,
		Reader:   store,
		Writer:   store,
		Bucket:   "test-bucket",
		Notifier: notifier,
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	return r
}

// completeOps returns an Operations with every operation present.
// It is used by New's dependency tests to build a state where "only the one under test is missing".
func completeOps() *ports.Operations {
	return fullOps(&fakeOutline{}, &fakeChapterScript{}, &fakeDesignSheet{}, &fakePanel{}, &fakePage{})
}

func fullOps(outline *fakeOutline, chapter *fakeChapterScript, design *fakeDesignSheet, panel *fakePanel, page *fakePage) *ports.Operations {
	return &ports.Operations{
		Outline:       outline,
		ChapterScript: chapter,
		DesignSheet:   design,
		Panel:         panel,
		Page:          page,
	}
}

// --- Tests ---

func TestNewValidatesRequiredDependencies(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	batchless := completeOps()
	batchless.Panel = nil
	batchless.Page = nil

	cases := map[string]Dependencies{
		"Ops":    {Reader: store, Writer: store, Bucket: "b"},
		"Reader": {Ops: completeOps(), Writer: store, Bucket: "b"},
		"Writer": {Ops: completeOps(), Reader: store, Bucket: "b"},
		"Bucket": {Ops: completeOps(), Reader: store, Writer: store},
		// The batch generators are called directly from steps, so if missing, startup must fail
		"Ops.Panel/Page": {Ops: batchless, Reader: store, Writer: store, Bucket: "b"},
	}
	for name, deps := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(deps); err == nil {
				t.Errorf("New without %s succeeded, want error", name)
			}
		})
	}
}

func TestRunnerComposeComicEndToEnd(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	outline := &fakeOutline{}
	chapter := &fakeChapterScript{}
	panel := &fakePanel{}
	page := &fakePage{}
	r := newTestRunner(t, store, fullOps(outline, chapter, &fakeDesignSheet{}, panel, page))

	task := &domain.Task{
		Command:    domain.TaskCommandComposeComic,
		JobID:      "job-abc",
		SourceText: "元文章",
	}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if outline.called != 1 {
		t.Errorf("outline called %d times, want 1", outline.called)
	}
	if len(chapter.calledChapters) != 2 || chapter.calledChapters[0] != "ch01" || chapter.calledChapters[1] != "ch02" {
		t.Errorf("chapter.calledChapters = %v, want [ch01 ch02]", chapter.calledChapters)
	}
	if len(panel.calledPanels) != 2 {
		t.Errorf("panel.calledPanels = %v, want 2 panels (one per chapter)", panel.calledPanels)
	}
	if len(page.calledPages) == 0 {
		t.Error("page was never composed")
	}

	// The state has been saved
	savedPath := "gs://test-bucket/comics/job-abc/comic_state.json"
	if _, ok := store.files[savedPath]; !ok {
		t.Errorf("state not saved at %q; files: %v", savedPath, keysOf(store.files))
	}
	if !strings.Contains(string(store.files[savedPath]), `"id": "job-abc"`) {
		t.Errorf("saved state does not contain job ID: %s", store.files[savedPath])
	}
}

func TestRunnerRegeneratePanelLoadsAndUpdatesExistingState(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	statePath := "gs://test-bucket/comics/job-xyz/comic_state.json"
	store.files[statePath] = []byte(`{
		"version": 1, "id": "job-xyz",
		"panels": [{"id": "ch01-p01", "chapter_id": "ch01", "page": 1, "characters": [], "dialogues": []}]
	}`)

	panel := &fakePanel{}
	r := newTestRunner(t, store, fullOps(&fakeOutline{}, &fakeChapterScript{}, &fakeDesignSheet{}, panel, &fakePage{}))

	seed := int64(42)
	task := &domain.Task{
		Command: domain.TaskCommandRegeneratePanel,
		JobID:   "job-xyz",
		PanelID: "ch01-p01",
		Seed:    &seed,
	}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if len(panel.calledPanels) != 1 || panel.calledPanels[0] != "ch01-p01" {
		t.Errorf("calledPanels = %v, want [ch01-p01]", panel.calledPanels)
	}
	if panel.lastOpts.Seed == nil || *panel.lastOpts.Seed != 42 {
		t.Errorf("Seed = %v, want 42", panel.lastOpts.Seed)
	}
	if _, ok := store.files[statePath]; !ok {
		t.Error("state was not re-saved")
	}
}

func TestRunnerRegenerateChapterScript(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	statePath := "gs://test-bucket/comics/job-c/comic_state.json"
	store.files[statePath] = []byte(`{"version": 1, "id": "job-c", "chapters": [{"id": "ch01", "title": "t"}], "panels": []}`)

	chapter := &fakeChapterScript{}
	r := newTestRunner(t, store, fullOps(&fakeOutline{}, chapter, &fakeDesignSheet{}, &fakePanel{}, &fakePage{}))

	task := &domain.Task{Command: domain.TaskCommandRegenerateChapterScript, JobID: "job-c", ChapterID: "ch01"}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(chapter.calledChapters) != 1 || chapter.calledChapters[0] != "ch01" {
		t.Errorf("calledChapters = %v, want [ch01]", chapter.calledChapters)
	}
}

func TestRunnerGenerateDesignSheetWithoutExistingJob(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	design := &fakeDesignSheet{}
	r := newTestRunner(t, store, fullOps(&fakeOutline{}, &fakeChapterScript{}, design, &fakePanel{}, &fakePage{}))

	task := &domain.Task{
		Command:      domain.TaskCommandGenerateDesignSheet,
		JobID:        "job-design",
		CharacterIDs: []string{"zundamon"},
	}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if design.called != 1 {
		t.Errorf("design.called = %d, want 1", design.called)
	}
	// The state of a standalone generation with no existing comic state is saved in design-jobs/, not comics/
	savedPath := "gs://test-bucket/design-jobs/job-design/comic_state.json"
	if !strings.Contains(string(store.files[savedPath]), "zundamon") {
		t.Errorf("saved state does not contain character: %s", store.files[savedPath])
	}
	if _, ok := store.files["gs://test-bucket/comics/job-design/comic_state.json"]; ok {
		t.Error("design-only job state must not be saved under comics/")
	}
}

func TestRunnerGenerateDesignSheetForExistingComicSavesToComics(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	store.files["gs://test-bucket/comics/job-comic/comic_state.json"] = []byte(
		`{"version":1,"id":"job-comic","title":"t","chapters":[{"id":"ch01"}],"panels":[]}`)
	design := &fakeDesignSheet{}
	r := newTestRunner(t, store, fullOps(&fakeOutline{}, &fakeChapterScript{}, design, &fakePanel{}, &fakePage{}))

	task := &domain.Task{
		Command:      domain.TaskCommandGenerateDesignSheet,
		JobID:        "job-comic",
		CharacterIDs: []string{"zundamon"},
	}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	// When a comic state exists, comics/ is updated and nothing is saved to design-jobs/
	if !strings.Contains(string(store.files["gs://test-bucket/comics/job-comic/comic_state.json"]), "zundamon") {
		t.Error("comic state was not updated with the design sheet")
	}
	if _, ok := store.files["gs://test-bucket/design-jobs/job-comic/comic_state.json"]; ok {
		t.Error("comic-attached design generation must not create design-jobs/ state")
	}
}

func TestRunnerRejectsInvalidTask(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	r := newTestRunner(t, store, fullOps(&fakeOutline{}, &fakeChapterScript{}, &fakeDesignSheet{}, &fakePanel{}, &fakePage{}))

	task := &domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-1"} // missing both SourceURL and SourceText
	if err := r.Run(context.Background(), task); err == nil {
		t.Error("Run with invalid task succeeded, want error")
	}
}

func TestRunnerUnknownCommandFails(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	r := newTestRunner(t, store, fullOps(&fakeOutline{}, &fakeChapterScript{}, &fakeDesignSheet{}, &fakePanel{}, &fakePage{}))

	task := &domain.Task{Command: "unknown", JobID: "job-1"}
	if err := r.Run(context.Background(), task); err == nil {
		t.Error("Run with unknown command succeeded, want error")
	}
}

func TestRunnerNotifiesCompleteOnSuccess(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	notifier := &fakeNotifier{}
	r := newTestRunnerWithNotifier(t, store, fullOps(&fakeOutline{}, &fakeChapterScript{}, &fakeDesignSheet{}, &fakePanel{}, &fakePage{}), notifier)

	// Notices only come from Execute, the production entry point (Run only runs the steps).
	task := domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-notify", SourceText: "元文章"}
	if err := r.Execute(context.Background(), task); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(notifier.completed) != 1 || notifier.completed[0].JobID != "job-notify" {
		t.Errorf("completed = %v, want one notification for job-notify", notifier.completed)
	}
	if len(notifier.failed) != 0 {
		t.Errorf("failed = %v, want no error notifications on success", notifier.failed)
	}
}

func TestRunnerNotifiesErrorOnStepFailure(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	notifier := &fakeNotifier{}
	failingPanel := &fakeFailingPanel{err: errors.New("boom")}
	ops := &ports.Operations{
		Outline:       &fakeOutline{},
		ChapterScript: &fakeChapterScript{},
		DesignSheet:   &fakeDesignSheet{},
		Panel:         failingPanel,
		Page:          &fakePage{},
	}
	r := newTestRunnerWithNotifier(t, store, ops, notifier)

	task := domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-fail", SourceText: "元文章"}
	if err := r.Execute(context.Background(), task); err == nil {
		t.Fatal("Execute succeeded, want error")
	}

	if len(notifier.failed) != 1 || notifier.failed[0].JobID != "job-fail" {
		t.Errorf("failed = %v, want one error notification for job-fail", notifier.failed)
	}
	if len(notifier.completed) != 0 {
		t.Errorf("completed = %v, want no success notifications on failure", notifier.completed)
	}
}

// fakeFailingPanel always returns err, used to exercise the error-notification path.
type fakeFailingPanel struct{ err error }

func (f *fakeFailingPanel) GeneratePanel(_ context.Context, _ *comic.MangaState, _ string, _ ports.GenerateOptions) (*comic.MangaState, error) {
	return nil, f.err
}

func (f *fakeFailingPanel) GenerateAllPanels(_ context.Context, state *comic.MangaState, _ ports.BatchOptions) (*comic.MangaState, error) {
	return state, f.err
}

func keysOf(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// fakePartialPanel fails only the given panel and leaves generation records for the rest.
// It is for checking that "even when part fails, the successes remain in the state".
type fakePartialPanel struct {
	failOn string
}

func (f *fakePartialPanel) GeneratePanel(_ context.Context, state *comic.MangaState, panelID string, _ ports.GenerateOptions) (*comic.MangaState, error) {
	if panelID == f.failOn {
		return nil, errors.New("boom")
	}
	if p := state.PanelByID(panelID); p != nil {
		p.Generation = &comic.GenerationRecord{ImageURL: "gs://out/" + panelID + ".png"}
	}
	return state, nil
}

// GenerateAllPanels, like go-comic-kit's batch generation, returns the state recording the successes
// along with the error even when it fails.
func (f *fakePartialPanel) GenerateAllPanels(_ context.Context, state *comic.MangaState, opts ports.BatchOptions) (*comic.MangaState, error) {
	var failure error
	for i := range state.Panels {
		if opts.SkipGenerated && state.Panels[i].Generation != nil {
			continue
		}
		if state.Panels[i].ID == f.failOn {
			failure = errors.New("boom")
			continue
		}
		state.Panels[i].Generation = &comic.GenerationRecord{ImageURL: "gs://out/" + state.Panels[i].ID + ".png"}
	}
	return state, failure
}

func TestRunnerSavesStateBetweenPhases(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	ops := fullOps(&fakeOutline{}, &fakeChapterScript{}, &fakeDesignSheet{}, &fakePanel{}, &fakePage{})
	r := newTestRunner(t, store, ops)

	task := &domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-checkpoint", SourceText: "元文章"}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// It is saved once per stage, for the script, panels and pages (not just the last time)
	if got := store.writeCount(statePathFor("job-checkpoint")); got != 3 {
		t.Errorf("state save count = %d, want 3 (a checkpoint per stage)", got)
	}
}

func TestRunnerSavesPartialResultsOnFailure(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	partialPanel := &fakePartialPanel{failOn: "ch02-p01"}
	pageFake := &fakePage{}
	ops := &ports.Operations{
		Outline:       &fakeOutline{},
		ChapterScript: &fakeChapterScript{},
		DesignSheet:   &fakeDesignSheet{},
		Panel:         partialPanel,
		Page:          pageFake,
	}
	r := newTestRunner(t, store, ops)

	task := &domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-partial", SourceText: "元文章"}
	if err := r.Run(context.Background(), task); err == nil {
		t.Fatal("Run succeeded, want error")
	}

	// Even on failure the state remains, and it holds the generation records of the panels that succeeded.
	// Without this, generated images are left in GCS with no reference from the state.
	saved := loadSavedState(t, store, "job-partial")
	if saved == nil {
		t.Fatal("the state was not saved on failure")
	}
	first := saved.PanelByID("ch01-p01")
	if first == nil || first.Generation == nil {
		t.Error("the saved state has no generation record for the panel that succeeded")
	}
	if failed := saved.PanelByID("ch02-p01"); failed != nil && failed.Generation != nil {
		t.Error("the failed panel has a generation record")
	}
}

func TestRunnerSavesPartialResultsWhenContextExpired(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	partialPanel := &fakePartialPanel{failOn: "ch02-p01"}
	pageFake := &fakePage{}
	ops := &ports.Operations{
		Outline:       &fakeOutline{},
		ChapterScript: &fakeChapterScript{},
		DesignSheet:   &fakeDesignSheet{},
		Panel:         partialPanel,
		Page:          pageFake,
	}
	r := newTestRunner(t, store, ops)

	// Imitates being cut off by PIPELINE_TIMEOUT. Saving is detached from this ctx,
	// so the results must remain even after it has expired.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	task := &domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-expired", SourceText: "元文章"}
	if err := r.Run(ctx, task); err == nil {
		t.Fatal("Run succeeded, want error")
	}

	if saved := loadSavedState(t, store, "job-expired"); saved == nil {
		t.Error("the state was not saved when the context had expired")
	}
}

// statePathFor returns the state path of the given job.
func statePathFor(jobID string) string {
	return "gs://test-bucket/comics/" + jobID + "/comic_state.json"
}

// loadSavedState reads the state saved in memStore. It returns nil if nothing was saved.
func loadSavedState(t *testing.T, store *memStore, jobID string) *comic.MangaState {
	t.Helper()
	data, ok := store.files[statePathFor(jobID)]
	if !ok {
		return nil
	}
	var state comic.MangaState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse the saved state: %v", err)
	}
	return &state
}

// stepNames returns the names of the planned steps in order.
func stepNames(t *testing.T, task *domain.Task) []string {
	t.Helper()
	steps, err := (DefaultPlanner{}).Plan(task)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	names := make([]string, len(steps))
	for i, s := range steps {
		names[i] = s.Name()
	}
	return names
}

func TestPlannerComposeComicStopsAfterScript(t *testing.T) {
	t.Parallel()

	full := stepNames(t, &domain.Task{Command: domain.TaskCommandComposeComic})
	if !slices.Contains(full, "panels") || !slices.Contains(full, "pages") {
		t.Errorf("the compose_comic plan does not include image generation: %v", full)
	}

	gated := stepNames(t, &domain.Task{Command: domain.TaskCommandComposeComic, StopAfterScript: true})
	want := []string{"load_state_if_exists", "outline", "chapter_scripts", "save_state"}
	if !slices.Equal(gated, want) {
		t.Errorf("stop_after_script plan = %v, want %v", gated, want)
	}
}

func TestPlannerRenderComicResumes(t *testing.T) {
	t.Parallel()

	names := stepNames(t, &domain.Task{Command: domain.TaskCommandRenderComic})
	want := []string{"load_state", "panels", "save_state", "pages", "save_state"}
	if !slices.Equal(names, want) {
		t.Fatalf("render_comic plan = %v, want %v", names, want)
	}

	// It is set to skip what has been generated (the reason a resume only covers what is missing)
	steps, err := (DefaultPlanner{}).Plan(&domain.Task{Command: domain.TaskCommandRenderComic})
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	panels, ok := steps[1].(AllPanelsStep)
	if !ok || !panels.SkipGenerated {
		t.Errorf("render_comic panels step = %+v, want SkipGenerated=true", steps[1])
	}
	pages, ok := steps[3].(AllPagesStep)
	if !ok || !pages.SkipGenerated {
		t.Errorf("render_comic pages step = %+v, want SkipGenerated=true", steps[3])
	}
}

func TestRunnerRenderComicGeneratesOnlyMissing(t *testing.T) {
	t.Parallel()
	store := newMemStore()

	// Prepare a state that is done up to the script, with only one panel generated
	scriptOps := fullOps(&fakeOutline{}, &fakeChapterScript{}, &fakeDesignSheet{}, &fakePanel{}, &fakePage{})
	scriptRunner := newTestRunner(t, store, scriptOps)
	scriptTask := &domain.Task{
		Command: domain.TaskCommandComposeComic, JobID: "job-resume",
		SourceText: "元文章", StopAfterScript: true,
	}
	if err := scriptRunner.Run(context.Background(), scriptTask); err != nil {
		t.Fatalf("script generation failed: %v", err)
	}

	saved := loadSavedState(t, store, "job-resume")
	if saved == nil || len(saved.Panels) == 0 {
		t.Fatal("the script state was not saved")
	}
	for i := range saved.Panels {
		if saved.Panels[i].Generation != nil {
			t.Fatal("images were generated despite stop_after_script")
		}
	}

	// Mark one panel as generated, then resume
	saved.Panels[0].Generation = &comic.GenerationRecord{ImageURL: "gs://out/already.png"}
	writeState(t, store, "job-resume", saved)

	panel := &fakePanel{}
	renderRunner := newTestRunner(t, store,
		fullOps(&fakeOutline{}, &fakeChapterScript{}, &fakeDesignSheet{}, panel, &fakePage{}))
	renderTask := &domain.Task{Command: domain.TaskCommandRenderComic, JobID: "job-resume"}
	if err := renderRunner.Run(context.Background(), renderTask); err != nil {
		t.Fatalf("render_comic failed: %v", err)
	}

	if slices.Contains(panel.calledPanels, saved.Panels[0].ID) {
		t.Errorf("the generated panel %q is being remade", saved.Panels[0].ID)
	}
	if len(panel.calledPanels) != len(saved.Panels)-1 {
		t.Errorf("generated panels = %v, want only the %d panels not yet generated", panel.calledPanels, len(saved.Panels)-1)
	}
}

// writeState writes a state directly into memStore.
func writeState(t *testing.T, store *memStore, jobID string, state *comic.MangaState) {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("failed to convert the state to JSON: %v", err)
	}
	store.files[statePathFor(jobID)] = data
}

// TestRunnerComposeComicRetryResumesInsteadOfRestarting checks that when a failed compose_comic is
// redelivered by Cloud Tasks, it moves on with only what is unfinished, without discarding saved output.
// If this breaks, every failure pays the cost of the generated images twice.
func TestRunnerComposeComicRetryResumesInsteadOfRestarting(t *testing.T) {
	t.Parallel()
	store := newMemStore()

	// First run: fails at ch02-p01. Everything up to ch01-p01 is saved as generated.
	partial := &fakePartialPanel{failOn: "ch02-p01"}
	firstOps := &ports.Operations{
		Outline: &fakeOutline{}, ChapterScript: &fakeChapterScript{}, DesignSheet: &fakeDesignSheet{},
		Panel: partial, Page: &fakePage{},
	}
	task := &domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-retry", SourceText: "元文章"}
	if err := newTestRunner(t, store, firstOps).Run(context.Background(), task); err == nil {
		t.Fatal("the first Run succeeded; the premise of making it fail is broken")
	}

	saved := loadSavedState(t, store, "job-retry")
	if saved == nil || saved.PanelByID("ch01-p01").Generation == nil {
		t.Fatal("the generated panels were not saved on failure")
	}

	// Second run: a redelivery of the same task. The script is not remade; only missing panels are generated.
	outline := &fakeOutline{}
	chapter := &fakeChapterScript{}
	panel := &fakePanel{}
	retryOps := fullOps(outline, chapter, &fakeDesignSheet{}, panel, &fakePage{})
	if err := newTestRunner(t, store, retryOps).Run(context.Background(), task); err != nil {
		t.Fatalf("re-run failed: %v", err)
	}

	if outline.called != 0 {
		t.Errorf("GenerateOutline was called %d times; the existing outline is being remade", outline.called)
	}
	if len(chapter.calledChapters) != 0 {
		t.Errorf("the script is being remade: %v; the panels' generation records would be erased too", chapter.calledChapters)
	}
	if slices.Contains(panel.calledPanels, "ch01-p01") {
		t.Error("the generated ch01-p01 is being remade")
	}
	if !slices.Contains(panel.calledPanels, "ch02-p01") {
		t.Errorf("the missing ch02-p01 was not generated: %v", panel.calledPanels)
	}

	final := loadSavedState(t, store, "job-retry")
	for i := range final.Panels {
		if final.Panels[i].Generation == nil {
			t.Errorf("panel %q is still not generated after the re-run", final.Panels[i].ID)
		}
	}
}

func TestRunnerComposeComicFreshJobRunsFullPipeline(t *testing.T) {
	t.Parallel()
	store := newMemStore()

	outline := &fakeOutline{}
	chapter := &fakeChapterScript{}
	panel := &fakePanel{}
	ops := fullOps(outline, chapter, &fakeDesignSheet{}, panel, &fakePage{})

	task := &domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-fresh", SourceText: "元文章"}
	if err := newTestRunner(t, store, ops).Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// A job with no state runs every stage, as before (resume support has not broken a fresh run)
	if outline.called != 1 {
		t.Errorf("GenerateOutline = %d calls, want 1", outline.called)
	}
	if len(chapter.calledChapters) != 2 {
		t.Errorf("script generation = %v, want 2 chapters", chapter.calledChapters)
	}
	if len(panel.calledPanels) == 0 {
		t.Error("no panel was generated")
	}
}

// The model and art style chosen in the form reach the script, the panels and the pages alike.
// Mixing models per chapter or per stage leaves one work with an uneven writing and drawing style.
func TestRunnerComposeComicPassesModelAndStyleChoices(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	outline := &fakeOutline{}
	chapter := &fakeChapterScript{}
	panel := &fakePanel{}
	page := &fakePage{}
	r := newTestRunner(t, store, fullOps(outline, chapter, &fakeDesignSheet{}, panel, page))

	task := &domain.Task{
		Command:       domain.TaskCommandComposeComic,
		JobID:         "job-models",
		SourceText:    "元文章",
		TextModel:     "gemini-selected",
		ModelOverride: "image-selected",
		StyleMode:     "watercolor",
	}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if outline.lastModel != "gemini-selected" {
		t.Errorf("outline model = %q, want gemini-selected", outline.lastModel)
	}
	if chapter.lastModel != "gemini-selected" {
		t.Errorf("chapter script model = %q, want gemini-selected", chapter.lastModel)
	}
	if panel.lastBatchOpts.Model != "image-selected" {
		t.Errorf("panel model = %q, want image-selected", panel.lastBatchOpts.Model)
	}
	if page.lastBatchOpts.Model != "image-selected" {
		t.Errorf("page model = %q, want image-selected", page.lastBatchOpts.Model)
	}
	if panel.lastBatchOpts.StyleMode != "watercolor" {
		t.Errorf("panel style mode = %q, want watercolor", panel.lastBatchOpts.StyleMode)
	}
	if page.lastBatchOpts.StyleMode != "watercolor" {
		t.Errorf("page style mode = %q, want watercolor", page.lastBatchOpts.StyleMode)
	}

	// The choice is kept in the state too. A later render_comic or chapter remake inherits it from there.
	saved := string(store.files["gs://test-bucket/comics/job-models/comic_state.json"])
	for _, want := range []string{`"text_model": "gemini-selected"`, `"image_model": "image-selected"`, `"style_mode": "watercolor"`} {
		if !strings.Contains(saved, want) {
			t.Errorf("saved state missing %s; got: %s", want, saved)
		}
	}
}

// When a work made up to the script gets its images later, the choice recorded in the state is inherited
// even when the Task names no model. Otherwise the rest would be drawn by a different model.
func TestRunnerRenderComicInheritsRecordedChoices(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	store.files["gs://test-bucket/comics/job-resume/comic_state.json"] = []byte(`{
		"version": 1,
		"id": "job-resume",
		"style_mode": "watercolor",
		"text_model": "gemini-recorded",
		"image_model": "image-recorded",
		"chapters": [{"id": "ch01", "title": "第一章"}],
		"panels": [{"id": "ch01-p01", "chapter_id": "ch01", "page": 1, "visual_anchor": "a", "characters": [], "dialogues": []}]
	}`)
	panel := &fakePanel{}
	page := &fakePage{}
	r := newTestRunner(t, store, fullOps(&fakeOutline{}, &fakeChapterScript{}, &fakeDesignSheet{}, panel, page))

	task := &domain.Task{Command: domain.TaskCommandRenderComic, JobID: "job-resume"}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if panel.lastBatchOpts.Model != "image-recorded" {
		t.Errorf("panel model = %q, want image-recorded", panel.lastBatchOpts.Model)
	}
	if panel.lastBatchOpts.StyleMode != "watercolor" {
		t.Errorf("panel style mode = %q, want watercolor", panel.lastBatchOpts.StyleMode)
	}
	if page.lastBatchOpts.StyleMode != "watercolor" {
		t.Errorf("page style mode = %q, want watercolor", page.lastBatchOpts.StyleMode)
	}
}

// Remaking a chapter's script also inherits the model that wrote the work.
func TestRunnerRegenerateChapterScriptInheritsRecordedTextModel(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	store.files["gs://test-bucket/comics/job-chap/comic_state.json"] = []byte(`{
		"version": 1,
		"id": "job-chap",
		"text_model": "gemini-recorded",
		"chapters": [{"id": "ch01", "title": "第一章"}],
		"panels": []
	}`)
	chapter := &fakeChapterScript{}
	r := newTestRunner(t, store, fullOps(&fakeOutline{}, chapter, &fakeDesignSheet{}, &fakePanel{}, &fakePage{}))

	task := &domain.Task{Command: domain.TaskCommandRegenerateChapterScript, JobID: "job-chap", ChapterID: "ch01"}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if chapter.lastModel != "gemini-recorded" {
		t.Errorf("chapter script model = %q, want gemini-recorded", chapter.lastModel)
	}
}

// Aspect ratio and resolution are now per-call values rather than kit configuration, so this
// checks that they arrive from the app (otherwise generation uses the kit's defaults).
func TestRunnerPassesImageLayout(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	panel := &fakePanel{}
	page := &fakePage{}
	r, err := New(Dependencies{
		Ops:    fullOps(&fakeOutline{}, &fakeChapterScript{}, &fakeDesignSheet{}, panel, page),
		Reader: store,
		Writer: store,
		Layout: ImageLayout{AspectRatio: "16:9", PanelImageSize: "1K", PageImageSize: "2K"},
		Bucket: "test-bucket",
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	task := &domain.Task{
		Command:       domain.TaskCommandComposeComic,
		JobID:         "job-layout",
		SourceText:    "元文章",
		TextModel:     "gemini-model",
		ModelOverride: "image-model",
	}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if panel.lastBatchOpts.AspectRatio != "16:9" || panel.lastBatchOpts.ImageSize != "1K" {
		t.Errorf("panel layout = %q/%q, want 16:9/1K",
			panel.lastBatchOpts.AspectRatio, panel.lastBatchOpts.ImageSize)
	}
	if page.lastBatchOpts.AspectRatio != "16:9" || page.lastBatchOpts.ImageSize != "2K" {
		t.Errorf("page layout = %q/%q, want 16:9/2K",
			page.lastBatchOpts.AspectRatio, page.lastBatchOpts.ImageSize)
	}
}

// A page is a composite of the panels, so composition waits until the panels have been checked.
// After stopping, page composition runs when the same command is sent again (the generated panels are skipped).
func TestRunnerRenderComicStopsAfterPanels(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	store.files["gs://test-bucket/comics/job-stage/comic_state.json"] = []byte(`{
		"version": 1,
		"id": "job-stage",
		"text_model": "gemini-model",
		"image_model": "image-model",
		"chapters": [{"id": "ch01", "title": "第一章"}],
		"panels": [{"id": "ch01-p01", "chapter_id": "ch01", "page": 1, "visual_anchor": "a", "characters": [], "dialogues": []}]
	}`)
	panel := &fakePanel{}
	page := &fakePage{}
	r := newTestRunner(t, store, fullOps(&fakeOutline{}, &fakeChapterScript{}, &fakeDesignSheet{}, panel, page))

	task := &domain.Task{Command: domain.TaskCommandRenderComic, JobID: "job-stage", StopAfterPanels: true}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if len(panel.calledPanels) != 1 {
		t.Errorf("panels = %v, want 1 generated", panel.calledPanels)
	}
	if len(page.calledPages) != 0 {
		t.Errorf("pages = %v, want none composed", page.calledPages)
	}

	// Sending the continuation runs only the pages (the panels are generated, so they are skipped).
	panel.calledPanels = nil
	if err := r.Run(context.Background(), &domain.Task{
		Command: domain.TaskCommandRenderComic, JobID: "job-stage",
	}); err != nil {
		t.Fatalf("Run (pages) failed: %v", err)
	}
	if len(panel.calledPanels) != 0 {
		t.Errorf("panels = %v, want none regenerated", panel.calledPanels)
	}
	if len(page.calledPages) != 1 {
		t.Errorf("pages = %v, want 1 composed", page.calledPages)
	}
}

// The seed applies to both panels and pages within the same command.
// If it applied to only one, the intended conditions could not be reproduced.
func TestRunnerRenderComicPassesSeedToBothStages(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	store.files["gs://test-bucket/comics/job-seed/comic_state.json"] = []byte(`{
		"version": 1,
		"id": "job-seed",
		"image_model": "image-model",
		"chapters": [{"id": "ch01", "title": "第一章"}],
		"panels": [{"id": "ch01-p01", "chapter_id": "ch01", "page": 1, "visual_anchor": "a", "characters": [], "dialogues": []}]
	}`)
	panel := &fakePanel{}
	page := &fakePage{}
	r := newTestRunner(t, store, fullOps(&fakeOutline{}, &fakeChapterScript{}, &fakeDesignSheet{}, panel, page))

	seed := int64(4242)
	if err := r.Run(context.Background(), &domain.Task{
		Command: domain.TaskCommandRenderComic, JobID: "job-seed", Seed: &seed,
	}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	for _, got := range []struct {
		name string
		seed *int64
	}{
		{"panels", panel.lastBatchOpts.Seed},
		{"pages", page.lastBatchOpts.Seed},
	} {
		if got.seed == nil || *got.seed != seed {
			t.Errorf("%s seed = %v, want %d", got.name, got.seed, seed)
		}
	}
}

// The image model is recorded on the work at the first image generation, and the next chapter inherits it.
// It is undecided at the script stage, so without the record the second button would go out with no model name.
func TestRunnerRecordsImageModelOnFirstRender(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	statePath := "gs://test-bucket/comics/job-rec/comic_state.json"
	store.files[statePath] = []byte(`{
		"version": 1,
		"id": "job-rec",
		"chapters": [{"id": "ch01", "title": "第一章"}],
		"panels": [{"id": "ch01-p01", "chapter_id": "ch01", "page": 1, "visual_anchor": "a", "characters": [], "dialogues": []}]
	}`)
	panel := &fakePanel{}
	r := newTestRunner(t, store, fullOps(&fakeOutline{}, &fakeChapterScript{}, &fakeDesignSheet{}, panel, &fakePage{}))

	// First run: the model chosen on screen arrives on the Task.
	if err := r.Run(context.Background(), &domain.Task{
		Command: domain.TaskCommandRenderComic, JobID: "job-rec",
		ModelOverride: "image-alt", StopAfterPanels: true,
	}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(string(store.files[statePath]), `"image_model": "image-alt"`) {
		t.Fatalf("the image model was not recorded on the work: %s", store.files[statePath])
	}

	// Second run: even with no model given, the recorded value is inherited.
	panel.lastBatchOpts = ports.BatchOptions{}
	if err := r.Run(context.Background(), &domain.Task{
		Command: domain.TaskCommandRenderComic, JobID: "job-rec", StopAfterPanels: true,
	}); err != nil {
		t.Fatalf("Run (2nd) failed: %v", err)
	}
	if panel.lastBatchOpts.Model != "image-alt" {
		t.Errorf("model = %q, want image-alt (inheriting the record)", panel.lastBatchOpts.Model)
	}
}
