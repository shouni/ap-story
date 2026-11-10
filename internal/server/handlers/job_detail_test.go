package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	kitcomic "github.com/shouni/go-comic-kit/comic"

	"github.com/shouni/ap-story/internal/domain"
)

func TestServeHistoryRendersItemsWithPaging(t *testing.T) {
	t.Parallel()

	repo := &fakeComicRepository{
		historyPage: domain.ComicHistoryPage{
			Items: []domain.ComicHistory{
				{JobID: "job-1", Title: "作品A", ChapterCount: 2, PanelCount: 8, PageCount: 3},
			},
			PageMeta: domain.PageMeta{Page: 2, PerPage: 20, Total: 25, TotalPages: 2, HasPrev: true, PrevPage: 1},
		},
	}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)
	req := httptest.NewRequest(http.MethodGet, "/jobs?page=2", nil)
	rec := httptest.NewRecorder()

	h.JobList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{"作品A", "/jobs/job-1", "js-delete-history"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestServeDetailsRendersStateWithImageLinks(t *testing.T) {
	t.Parallel()

	state := &kitcomic.MangaState{
		Title:       "テスト作品",
		Description: "あらすじ",
		UpdatedAt:   time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
		Chapters: []kitcomic.Chapter{
			{ID: "ch01", Title: "第一章", Summary: "導入"},
		},
		Panels: []kitcomic.Panel{
			{
				ID: "ch01-p01", ChapterID: "ch01", Page: 1, Shot: "close-up",
				Dialogues: []kitcomic.DialogueLine{
					{SpeakerID: "zundamon", Text: "なのだ！", Kind: "speech"},
					{SpeakerID: "", Text: "そして時は動き出す", Kind: "narration"},
				},
				Generation: &kitcomic.GenerationRecord{ImageURL: "gs://test-bucket/comics/job-1/images/panel_ch01-p01.png"},
			},
			{ID: "ch01-p02", ChapterID: "ch01", Page: 1},
		},
		Pages: []kitcomic.PageArtifact{
			{PageNumber: 1, Generation: &kitcomic.GenerationRecord{ImageURL: "gs://test-bucket/comics/job-1/images/comic_page_1.png"}},
		},
		DesignSheets: []kitcomic.DesignSheetRef{
			{CharacterID: "zundamon", ImageURL: "gs://test-bucket/character/zundamon/job-1.png"},
		},
	}
	repo := &fakeComicRepository{states: map[string]*kitcomic.MangaState{"job-1": state}}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)

	req := httptestRequestWithURLParam(t, http.MethodGet, "/jobs/job-1", "", "jobID", "job-1")
	rec := httptest.NewRecorder()
	h.Job(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"テスト作品",
		"ch01: 第一章",
		"/jobs/job-1/images/images/panel_ch01-p01.png",
		"/jobs/job-1/images/images/comic_page_1.png",
		"/characters/images/zundamon/job-1.png",
		"zundamon: なのだ！",
		"そして時は動き出す",
		"未生成", // ch01-p02, which has no image
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestServeDetailsReturns404ForMissingJob(t *testing.T) {
	t.Parallel()

	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, &fakeComicRepository{states: map[string]*kitcomic.MangaState{}})
	req := httptestRequestWithURLParam(t, http.MethodGet, "/jobs/job-x", "", "jobID", "job-x")
	rec := httptest.NewRecorder()

	h.Job(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func scriptOnlyState() *kitcomic.MangaState {
	return &kitcomic.MangaState{
		Title:    "台本だけの作品",
		Chapters: []kitcomic.Chapter{{ID: "ch01", Title: "第一章"}},
		Panels: []kitcomic.Panel{
			{ID: "ch01-p01", ChapterID: "ch01", Page: 1},
			{ID: "ch01-p02", ChapterID: "ch01", Page: 1},
		},
	}
}

func renderDetail(t *testing.T, state *kitcomic.MangaState) string {
	t.Helper()
	repo := &fakeComicRepository{states: map[string]*kitcomic.MangaState{"job-1": state}}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)

	req := httptestRequestWithURLParam(t, http.MethodGet, "/jobs/job-1", "", "jobID", "job-1")
	rec := httptest.NewRecorder()
	h.Job(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	return rec.Body.String()
}

// Panels and pages get separate buttons. A page is a composite of the panels, so
// composition waits until the panels have been checked.
func TestServeDetailsOffersPanelsFirstForScriptOnlyState(t *testing.T) {
	t.Parallel()

	body := renderDetail(t, scriptOnlyState())

	if !strings.Contains(body, `data-stage="panels"`) {
		t.Error("a script-only state shows no \"コマを生成\" button")
	}
	if !strings.Contains(body, "コマを生成") {
		t.Error("with no image at all, the label reads \"続きのコマを生成\"")
	}
	if strings.Contains(body, "ページを合成") {
		t.Error("with no panel at all, a page-composition button is shown")
	}
}

// Once the panels are all there, only page composition is offered next.
func TestServeDetailsOffersPageCompositionAfterPanels(t *testing.T) {
	t.Parallel()

	state := scriptOnlyState()
	for i := range state.Panels {
		state.Panels[i].Generation = &kitcomic.GenerationRecord{ImageURL: "gs://test-bucket/comics/job-1/images/p.png"}
	}

	body := renderDetail(t, state)

	if !strings.Contains(body, "ページを合成") {
		t.Error("the panels are all there but no page-composition button is shown")
	}
	if strings.Contains(body, `data-stage="panels"`) {
		t.Error("the panels are all there but a panel-generation button remains")
	}
}

func TestServeDetailsOffersResumeForPartiallyGeneratedState(t *testing.T) {
	t.Parallel()

	state := scriptOnlyState()
	state.Panels[0].Generation = &kitcomic.GenerationRecord{
		ImageURL: "gs://test-bucket/comics/job-1/images/panel_ch01-p01.png",
	}

	body := renderDetail(t, state)

	if !strings.Contains(body, `data-stage="panels"`) {
		t.Error("a state with panels still missing shows no resume button")
	}
	if !strings.Contains(body, "続きのコマを生成") {
		t.Error("with some panels generated, the label does not read \"続きのコマを生成\"")
	}
}

func TestServeDetailsHidesRenderButtonWhenComplete(t *testing.T) {
	t.Parallel()

	state := scriptOnlyState()
	for i := range state.Panels {
		state.Panels[i].Generation = &kitcomic.GenerationRecord{ImageURL: "gs://test-bucket/comics/job-1/images/p.png"}
	}
	state.Pages = []kitcomic.PageArtifact{
		{PageNumber: 1, Generation: &kitcomic.GenerationRecord{ImageURL: "gs://test-bucket/comics/job-1/images/comic_page_1.png"}},
	}

	body := renderDetail(t, state)

	for _, unwanted := range []string{`data-stage="panels"`, "ページを合成"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("everything is generated but a %q button is shown", unwanted)
		}
	}
}

func TestServeDetailsRendersRegenerateControls(t *testing.T) {
	t.Parallel()

	state := scriptOnlyState()
	state.Panels[0].Generation = &kitcomic.GenerationRecord{
		ImageURL: "gs://test-bucket/comics/job-1/images/panel_ch01-p01.png",
	}
	state.Pages = []kitcomic.PageArtifact{
		{PageNumber: 1, Generation: &kitcomic.GenerationRecord{ImageURL: "gs://test-bucket/comics/job-1/images/comic_page_1.png"}},
	}

	body := renderDetail(t, state)

	for _, want := range []string{
		`data-command="regenerate_panel"`,
		`data-command="regenerate_page"`,
		`data-command="regenerate_chapter_script"`,
		`data-target="ch01-p01"`,
		`data-target="ch01"`,
		`data-mode="reroll"`,
		`data-mode="edit"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the regenerate controls lack %q", want)
		}
	}
}

func TestServeDetailsHidesEditForUngeneratedPanel(t *testing.T) {
	t.Parallel()

	// Edit mode presupposes an existing image, so it must not appear for a panel not yet generated
	// (go-comic-kit fails when there is no generated image).
	body := renderDetail(t, scriptOnlyState())

	if strings.Contains(body, `data-mode="edit"`) {
		t.Error("an edit button is shown for a panel with no image")
	}
}

// Chapter cards carry "この章の画像を生成". Images are the most expensive stage, so an entry point for trying
// per chapter rather than the whole work sits in the same place as script regeneration.
func TestServeDetailsRendersChapterRenderControl(t *testing.T) {
	t.Parallel()

	body := renderDetail(t, scriptOnlyState())

	for _, want := range []string{
		`data-command="render_comic"`,
		`data-target="ch01"`,
		`この章のコマを生成`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the chapter card lacks %q", want)
		}
	}
}

// A chapter with no script yet gains nothing from an image-generation button; pressing it would end with 0 items.
func TestServeDetailsHidesChapterRenderWithoutPanels(t *testing.T) {
	t.Parallel()

	state := scriptOnlyState()
	state.Panels = nil
	state.Chapters[0].PanelIDs = nil

	if body := renderDetail(t, state); strings.Contains(body, `この章のコマを生成`) {
		t.Error("an image-generation button is shown for a chapter with no panels")
	}
}

// The image model is chosen not on the script form but on this page, where image generation starts.
// The initial choice is the value recorded in the state, so as long as people keep pressing, the work stays consistent.
func TestServeDetailsOffersImageModelDefaultedToRecorded(t *testing.T) {
	t.Parallel()

	state := scriptOnlyState()
	state.ImageModel = "image-alt"

	body := renderDetail(t, state)

	if !strings.Contains(body, "js-image-model") {
		t.Fatal("no image model picker is shown")
	}
	if !strings.Contains(body, `<option value="image-alt" selected>`) {
		t.Errorf("the recorded model is not the initial choice:\n%s", body)
	}
}

// A work with nothing recorded yet (script only) gets the head of the list as the initial choice.
func TestServeDetailsDefaultsImageModelToHeadWhenUnrecorded(t *testing.T) {
	t.Parallel()

	body := renderDetail(t, scriptOnlyState())

	if !strings.Contains(body, `<option value="image-model" selected>`) {
		t.Errorf("the default model is not the initial choice:\n%s", body)
	}
}

// A finished work has no button to press, so no picker either.
func TestServeDetailsHidesImageModelWhenComplete(t *testing.T) {
	t.Parallel()

	state := scriptOnlyState()
	for i := range state.Panels {
		state.Panels[i].Generation = &kitcomic.GenerationRecord{ImageURL: "gs://test-bucket/comics/job-1/images/p.png"}
	}
	state.Pages = []kitcomic.PageArtifact{
		{PageNumber: 1, Generation: &kitcomic.GenerationRecord{ImageURL: "gs://test-bucket/comics/job-1/images/comic_page_1.png"}},
	}

	if body := renderDetail(t, state); strings.Contains(body, "js-image-model") {
		t.Error("generation is done but an image model picker is shown")
	}
}

// TestServeDetailsOrdersPagesByNumber checks that the pages are listed in reading order.
//
// state.Pages is in composition order (go-comic-kit's SetPageArtifact just appends a new page
// at the end). Remaking page 3 first makes the state order 3, 1, 2, and reading straight through
// one would not even notice the order is wrong.
func TestServeDetailsOrdersPagesByNumber(t *testing.T) {
	t.Parallel()

	page := func(n int) kitcomic.PageArtifact {
		return kitcomic.PageArtifact{
			PageNumber: n,
			Generation: &kitcomic.GenerationRecord{
				ImageURL: fmt.Sprintf("gs://test-bucket/comics/job-1/images/comic_page_%d.png", n),
			},
		}
	}
	state := &kitcomic.MangaState{
		Title: "テスト作品",
		// The state after page 3 was remade first
		Pages: []kitcomic.PageArtifact{page(3), page(1), page(2)},
	}
	repo := &fakeComicRepository{states: map[string]*kitcomic.MangaState{"job-1": state}}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)

	req := httptestRequestWithURLParam(t, http.MethodGet, "/jobs/job-1", "", "jobID", "job-1")
	rec := httptest.NewRecorder()
	h.Job(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()

	last := -1
	for n := 1; n <= 3; n++ {
		at := strings.Index(body, fmt.Sprintf("comic_page_%d.png", n))
		if at < 0 {
			t.Fatalf("page %d is not rendered", n)
		}
		if at < last {
			t.Errorf("page %d comes before the previous page (not in reading order)", n)
		}
		last = at
	}
}
