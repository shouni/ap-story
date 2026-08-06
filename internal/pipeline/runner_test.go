package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/domain"
)

// --- Fakes ---

type fakeOutline struct{ called int }

func (f *fakeOutline) GenerateOutline(_ context.Context, req ports.OutlineRequest) (*ports.MangaState, error) {
	f.called++
	return &ports.MangaState{
		Version:  ports.StateSchemaVersion,
		Title:    "title from " + req.SourceText,
		Chapters: []ports.Chapter{{ID: "ch01", Title: "第一章"}, {ID: "ch02", Title: "第二章"}},
	}, nil
}

type fakeChapterScript struct{ calledChapters []string }

func (f *fakeChapterScript) GenerateChapterScript(_ context.Context, state *ports.MangaState, chapterID string) (*ports.MangaState, error) {
	f.calledChapters = append(f.calledChapters, chapterID)
	state.Panels = append(state.Panels, ports.Panel{ID: chapterID + "-p01", ChapterID: chapterID, Page: len(f.calledChapters)})
	return state, nil
}

type fakeDesignSheet struct{ called int }

func (f *fakeDesignSheet) GenerateDesignSheet(_ context.Context, state *ports.MangaState, req ports.DesignSheetRequest) (*ports.MangaState, error) {
	f.called++
	if state == nil {
		state = &ports.MangaState{Version: ports.StateSchemaVersion}
	}
	for _, id := range req.CharacterIDs {
		state.SetDesignSheet(ports.DesignSheetRef{CharacterID: id, ImageURL: "gs://out/design_" + id + ".png"})
	}
	return state, nil
}

type fakePanel struct {
	calledPanels  []string
	lastOpts      ports.GenerateOptions
	lastBatchOpts ports.BatchOptions
}

func (f *fakePanel) GeneratePanel(_ context.Context, state *ports.MangaState, panelID string, opts ports.GenerateOptions) (*ports.MangaState, error) {
	f.calledPanels = append(f.calledPanels, panelID)
	f.lastOpts = opts
	p := state.PanelByID(panelID)
	if p != nil {
		p.Generation = &ports.GenerationRecord{ImageURL: "gs://out/" + panelID + ".png"}
	}
	return state, nil
}

// GenerateAllPanels は一括生成を単体生成の繰り返しで模します。
// 並列数の検証は go-comic-kit 側のテストが担うため、ここでは呼び出し内容だけを見ます。
func (f *fakePanel) GenerateAllPanels(ctx context.Context, state *ports.MangaState, opts ports.BatchOptions) (*ports.MangaState, error) {
	f.lastBatchOpts = opts
	for i := range state.Panels {
		if opts.SkipGenerated && state.Panels[i].Generation != nil {
			continue
		}
		var err error
		state, err = f.GeneratePanel(ctx, state, state.Panels[i].ID,
			ports.GenerateOptions{Seed: opts.Seed, ModelOverride: opts.ModelOverride, OutputDir: opts.OutputDir})
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

func (f *fakePage) ComposePage(_ context.Context, state *ports.MangaState, page int, opts ports.GenerateOptions) (*ports.MangaState, error) {
	f.calledPages = append(f.calledPages, page)
	f.lastOpts = opts
	state.SetPageArtifact(ports.PageArtifact{PageNumber: page, Generation: &ports.GenerationRecord{
		ImageURL: fmt.Sprintf("gs://out/page_%d.png", page),
	}})
	return state, nil
}

// ComposeAllPages は一括合成を単体合成の繰り返しで模します。
func (f *fakePage) ComposeAllPages(ctx context.Context, state *ports.MangaState, opts ports.BatchOptions) (*ports.MangaState, error) {
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
			ports.GenerateOptions{Seed: opts.Seed, ModelOverride: opts.ModelOverride, OutputDir: opts.OutputDir})
		if err != nil {
			return state, err
		}
	}
	return state, nil
}

// memStore is an in-memory fake satisfying both ports.ContentReader and remoteio.Writer.
type memStore struct {
	files map[string][]byte
	// writes はパスごとの書き込み回数です。工程ごとのチェックポイント保存の検証に使います。
	writes map[string]int
}

func newMemStore() *memStore {
	return &memStore{files: map[string][]byte{}, writes: map[string]int{}}
}

// writeCount は指定パスへの書き込み回数を返します。
func (m *memStore) writeCount(path string) int { return m.writes[path] }

func (m *memStore) Open(_ context.Context, path string) (io.ReadCloser, error) {
	data, ok := m.files[path]
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *memStore) Write(ctx context.Context, path string, r io.Reader, _ ...remoteio.WriteOption) error {
	// 実際の GCS 書き込みと同じく context を尊重する。これが無いと、失敗時の保存を
	// 期限切れ context で行っていても素通りしてしまい、テストが退行を検出できない。
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

func (f *fakeNotifier) NotifyComplete(_ context.Context, task domain.Task) error {
	f.completed = append(f.completed, task)
	return nil
}

func (f *fakeNotifier) NotifyError(_ context.Context, task domain.Task, cause error) error {
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

// completeOps は、全操作が揃った Operations を返します。
// New の依存検証テストで「検証したい1つだけが欠けている」状態を作るために使います。
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
		PanelBatch:    panel,
		PageBatch:     page,
	}
}

// --- Tests ---

func TestNewValidatesRequiredDependencies(t *testing.T) {
	t.Parallel()
	store := newMemStore()
	batchless := completeOps()
	batchless.PanelBatch = nil
	batchless.PageBatch = nil

	cases := map[string]Dependencies{
		"Ops":    {Reader: store, Writer: store, Bucket: "b"},
		"Reader": {Ops: completeOps(), Writer: store, Bucket: "b"},
		"Writer": {Ops: completeOps(), Reader: store, Bucket: "b"},
		"Bucket": {Ops: completeOps(), Reader: store, Writer: store},
		// 一括生成はステップから直接呼ぶため、欠けていれば起動時に落ちること
		"Ops.PanelBatch/PageBatch": {Ops: batchless, Reader: store, Writer: store, Bucket: "b"},
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

	// state が保存されていること
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
	// 既存の作品 state がない単体生成の state は comics/ ではなく design-jobs/ に保存される
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
	// 既存の作品 state がある場合は comics/ 側を更新し、design-jobs/ には保存しない
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

	task := &domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-1"} // SourceURL/SourceText 両方欠落
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

	task := &domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-notify", SourceText: "元文章"}
	if err := r.Run(context.Background(), task); err != nil {
		t.Fatalf("Run failed: %v", err)
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
		PanelBatch:    failingPanel,
		PageBatch:     &fakePage{},
	}
	r := newTestRunnerWithNotifier(t, store, ops, notifier)

	task := &domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-fail", SourceText: "元文章"}
	if err := r.Run(context.Background(), task); err == nil {
		t.Fatal("Run succeeded, want error")
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

func (f *fakeFailingPanel) GeneratePanel(_ context.Context, _ *ports.MangaState, _ string, _ ports.GenerateOptions) (*ports.MangaState, error) {
	return nil, f.err
}

func (f *fakeFailingPanel) GenerateAllPanels(_ context.Context, state *ports.MangaState, _ ports.BatchOptions) (*ports.MangaState, error) {
	return state, f.err
}

func keysOf(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// fakePartialPanel は指定パネルだけ失敗し、それ以外は生成記録を残します。
// 「一部失敗しても成功分は state に残る」ことの検証用です。
type fakePartialPanel struct {
	failOn string
}

func (f *fakePartialPanel) GeneratePanel(_ context.Context, state *ports.MangaState, panelID string, _ ports.GenerateOptions) (*ports.MangaState, error) {
	if panelID == f.failOn {
		return nil, errors.New("boom")
	}
	if p := state.PanelByID(panelID); p != nil {
		p.Generation = &ports.GenerationRecord{ImageURL: "gs://out/" + panelID + ".png"}
	}
	return state, nil
}

// GenerateAllPanels は go-comic-kit の一括生成と同じく、失敗しても成功分を記録した
// state をエラーと一緒に返します。
func (f *fakePartialPanel) GenerateAllPanels(_ context.Context, state *ports.MangaState, opts ports.BatchOptions) (*ports.MangaState, error) {
	var failure error
	for i := range state.Panels {
		if opts.SkipGenerated && state.Panels[i].Generation != nil {
			continue
		}
		if state.Panels[i].ID == f.failOn {
			failure = errors.New("boom")
			continue
		}
		state.Panels[i].Generation = &ports.GenerationRecord{ImageURL: "gs://out/" + state.Panels[i].ID + ".png"}
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

	// 台本・パネル・ページの3工程ぶん保存される（最後の1回だけではない）
	if got := store.writeCount(statePathFor("job-checkpoint")); got != 3 {
		t.Errorf("state の保存回数 = %d, want 3（工程ごとのチェックポイント）", got)
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
		PanelBatch:    partialPanel,
		PageBatch:     pageFake,
	}
	r := newTestRunner(t, store, ops)

	task := &domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-partial", SourceText: "元文章"}
	if err := r.Run(context.Background(), task); err == nil {
		t.Fatal("Run succeeded, want error")
	}

	// 失敗しても state は残り、成功したコマの生成記録が入っていること。
	// これが無いと、生成済みの画像が state から参照されないまま GCS に取り残される。
	saved := loadSavedState(t, store, "job-partial")
	if saved == nil {
		t.Fatal("失敗時に state が保存されていない")
	}
	first := saved.PanelByID("ch01-p01")
	if first == nil || first.Generation == nil {
		t.Error("成功したコマの生成記録が保存された state に無い")
	}
	if failed := saved.PanelByID("ch02-p01"); failed != nil && failed.Generation != nil {
		t.Error("失敗したコマに生成記録が付いている")
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
		PanelBatch:    partialPanel,
		PageBatch:     pageFake,
	}
	r := newTestRunner(t, store, ops)

	// PIPELINE_TIMEOUT で打ち切られた状況を模す。保存はこの ctx から切り離して
	// 行うため、期限切れでも成果は残らなければならない。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	task := &domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-expired", SourceText: "元文章"}
	if err := r.Run(ctx, task); err == nil {
		t.Fatal("Run succeeded, want error")
	}

	if saved := loadSavedState(t, store, "job-expired"); saved == nil {
		t.Error("context 期限切れ時に state が保存されていない")
	}
}

// statePathFor は指定ジョブの state 保存先パスを返します。
func statePathFor(jobID string) string {
	return "gs://test-bucket/comics/" + jobID + "/comic_state.json"
}

// loadSavedState は memStore に保存された state を読み出します。未保存なら nil を返します。
func loadSavedState(t *testing.T, store *memStore, jobID string) *ports.MangaState {
	t.Helper()
	data, ok := store.files[statePathFor(jobID)]
	if !ok {
		return nil
	}
	var state ports.MangaState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("保存された state のパースに失敗しました: %v", err)
	}
	return &state
}

// stepNames は計画されたステップ名を並び順で返します。
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
		t.Errorf("compose_comic の計画に画像生成が含まれていない: %v", full)
	}

	gated := stepNames(t, &domain.Task{Command: domain.TaskCommandComposeComic, StopAfterScript: true})
	want := []string{"load_state_if_exists", "outline", "chapter_scripts", "save_state"}
	if !slices.Equal(gated, want) {
		t.Errorf("stop_after_script の計画 = %v, want %v", gated, want)
	}
}

func TestPlannerRenderComicResumes(t *testing.T) {
	t.Parallel()

	names := stepNames(t, &domain.Task{Command: domain.TaskCommandRenderComic})
	want := []string{"load_state", "panels", "save_state", "pages", "save_state"}
	if !slices.Equal(names, want) {
		t.Fatalf("render_comic の計画 = %v, want %v", names, want)
	}

	// 生成済みを飛ばす設定になっていること（再開が未生成分だけで済む根拠）
	steps, err := (DefaultPlanner{}).Plan(&domain.Task{Command: domain.TaskCommandRenderComic})
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	panels, ok := steps[1].(AllPanelsStep)
	if !ok || !panels.SkipGenerated {
		t.Errorf("render_comic の panels ステップ = %+v, want SkipGenerated=true", steps[1])
	}
	pages, ok := steps[3].(AllPagesStep)
	if !ok || !pages.SkipGenerated {
		t.Errorf("render_comic の pages ステップ = %+v, want SkipGenerated=true", steps[3])
	}
}

func TestRunnerRenderComicGeneratesOnlyMissing(t *testing.T) {
	t.Parallel()
	store := newMemStore()

	// 台本まで済んで、1コマだけ生成済みの state を用意する
	scriptOps := fullOps(&fakeOutline{}, &fakeChapterScript{}, &fakeDesignSheet{}, &fakePanel{}, &fakePage{})
	scriptRunner := newTestRunner(t, store, scriptOps)
	scriptTask := &domain.Task{
		Command: domain.TaskCommandComposeComic, JobID: "job-resume",
		SourceText: "元文章", StopAfterScript: true,
	}
	if err := scriptRunner.Run(context.Background(), scriptTask); err != nil {
		t.Fatalf("台本生成に失敗: %v", err)
	}

	saved := loadSavedState(t, store, "job-resume")
	if saved == nil || len(saved.Panels) == 0 {
		t.Fatal("台本の state が保存されていない")
	}
	for i := range saved.Panels {
		if saved.Panels[i].Generation != nil {
			t.Fatal("stop_after_script なのに画像が生成されている")
		}
	}

	// 1コマだけ生成済みにしてから再開する
	saved.Panels[0].Generation = &ports.GenerationRecord{ImageURL: "gs://out/already.png"}
	writeState(t, store, "job-resume", saved)

	panel := &fakePanel{}
	renderRunner := newTestRunner(t, store,
		fullOps(&fakeOutline{}, &fakeChapterScript{}, &fakeDesignSheet{}, panel, &fakePage{}))
	renderTask := &domain.Task{Command: domain.TaskCommandRenderComic, JobID: "job-resume"}
	if err := renderRunner.Run(context.Background(), renderTask); err != nil {
		t.Fatalf("render_comic に失敗: %v", err)
	}

	if slices.Contains(panel.calledPanels, saved.Panels[0].ID) {
		t.Errorf("生成済みのコマ %q を作り直している", saved.Panels[0].ID)
	}
	if len(panel.calledPanels) != len(saved.Panels)-1 {
		t.Errorf("生成したコマ = %v, want 未生成の %d コマだけ", panel.calledPanels, len(saved.Panels)-1)
	}
}

// writeState は memStore に state を直接書き込みます。
func writeState(t *testing.T, store *memStore, jobID string, state *ports.MangaState) {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("state の JSON 変換に失敗: %v", err)
	}
	store.files[statePathFor(jobID)] = data
}

// TestRunnerComposeComicRetryResumesInsteadOfRestarting は、失敗した compose_comic が
// Cloud Tasks に再配信されたとき、保存済みの成果を捨てずに未了分だけを進めることを検証します。
// これが崩れると、失敗するたびに生成済み画像ぶんの費用がそのまま二重にかかります。
func TestRunnerComposeComicRetryResumesInsteadOfRestarting(t *testing.T) {
	t.Parallel()
	store := newMemStore()

	// 1回目: ch02-p01 で失敗する。ch01-p01 までは生成済みとして保存される。
	partial := &fakePartialPanel{failOn: "ch02-p01"}
	firstOps := &ports.Operations{
		Outline: &fakeOutline{}, ChapterScript: &fakeChapterScript{}, DesignSheet: &fakeDesignSheet{},
		Panel: partial, Page: &fakePage{}, PanelBatch: partial, PageBatch: &fakePage{},
	}
	task := &domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-retry", SourceText: "元文章"}
	if err := newTestRunner(t, store, firstOps).Run(context.Background(), task); err == nil {
		t.Fatal("1回目の Run が成功した。失敗させる前提が崩れている")
	}

	saved := loadSavedState(t, store, "job-retry")
	if saved == nil || saved.PanelByID("ch01-p01").Generation == nil {
		t.Fatal("失敗時に生成済みコマが保存されていない")
	}

	// 2回目: 同じタスクの再配信。台本は作り直さず、未生成のコマだけを生成する。
	outline := &fakeOutline{}
	chapter := &fakeChapterScript{}
	panel := &fakePanel{}
	retryOps := fullOps(outline, chapter, &fakeDesignSheet{}, panel, &fakePage{})
	if err := newTestRunner(t, store, retryOps).Run(context.Background(), task); err != nil {
		t.Fatalf("再実行に失敗: %v", err)
	}

	if outline.called != 0 {
		t.Errorf("GenerateOutline が %d 回呼ばれた。既存の章立てを作り直している", outline.called)
	}
	if len(chapter.calledChapters) != 0 {
		t.Errorf("台本を作り直している: %v。コマの生成記録まで消える", chapter.calledChapters)
	}
	if slices.Contains(panel.calledPanels, "ch01-p01") {
		t.Error("生成済みの ch01-p01 を作り直している")
	}
	if !slices.Contains(panel.calledPanels, "ch02-p01") {
		t.Errorf("未生成の ch02-p01 が生成されていない: %v", panel.calledPanels)
	}

	final := loadSavedState(t, store, "job-retry")
	for i := range final.Panels {
		if final.Panels[i].Generation == nil {
			t.Errorf("再実行後もコマ %q が未生成", final.Panels[i].ID)
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

	// state が無いジョブでは従来どおり全工程が走ること（再開対応が新規実行を壊していない）
	if outline.called != 1 {
		t.Errorf("GenerateOutline = %d 回, want 1", outline.called)
	}
	if len(chapter.calledChapters) != 2 {
		t.Errorf("台本生成 = %v, want 2章ぶん", chapter.calledChapters)
	}
	if len(panel.calledPanels) == 0 {
		t.Error("コマが1つも生成されていない")
	}
}
