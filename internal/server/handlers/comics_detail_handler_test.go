package handlers

import (
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
	req := httptest.NewRequest(http.MethodGet, "/history?page=2", nil)
	rec := httptest.NewRecorder()

	h.ServeHistory(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{"作品A", "/history/job-1", "js-delete-history"} {
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

	req := httptestRequestWithURLParam(t, http.MethodGet, "/history/job-1", "", "jobID", "job-1")
	rec := httptest.NewRecorder()
	h.ServeDetails(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"テスト作品",
		"ch01: 第一章",
		"/api/comics/job-1/images/images/panel_ch01-p01.png",
		"/api/comics/job-1/images/images/comic_page_1.png",
		"/api/characters/images/zundamon/job-1.png",
		"zundamon: なのだ！",
		"そして時は動き出す",
		"未生成", // 画像なしの ch01-p02
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestServeDetailsReturns404ForMissingJob(t *testing.T) {
	t.Parallel()

	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, &fakeComicRepository{states: map[string]*kitcomic.MangaState{}})
	req := httptestRequestWithURLParam(t, http.MethodGet, "/history/job-x", "", "jobID", "job-x")
	rec := httptest.NewRecorder()

	h.ServeDetails(rec, req)

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

	req := httptestRequestWithURLParam(t, http.MethodGet, "/history/job-1", "", "jobID", "job-1")
	rec := httptest.NewRecorder()
	h.ServeDetails(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	return rec.Body.String()
}

func TestServeDetailsOffersImageGenerationForScriptOnlyState(t *testing.T) {
	t.Parallel()

	body := renderDetail(t, scriptOnlyState())

	if !strings.Contains(body, "js-render-comic") {
		t.Error("台本だけの state に「画像生成へ進む」ボタンが出ていない")
	}
	if !strings.Contains(body, "画像生成へ進む") {
		t.Error("画像が1枚も無いのに「続きを生成」表記になっている")
	}
}

func TestServeDetailsOffersResumeForPartiallyGeneratedState(t *testing.T) {
	t.Parallel()

	state := scriptOnlyState()
	state.Panels[0].Generation = &kitcomic.GenerationRecord{
		ImageURL: "gs://test-bucket/comics/job-1/images/panel_ch01-p01.png",
	}

	body := renderDetail(t, state)

	if !strings.Contains(body, "js-render-comic") {
		t.Error("未生成が残る state に再開ボタンが出ていない")
	}
	if !strings.Contains(body, "続きを生成") {
		t.Error("生成済みがあるのに「続きを生成」表記になっていない")
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

	if strings.Contains(body, "js-render-comic") {
		t.Error("全て生成済みなのに「続きを生成」ボタンが出ている")
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
			t.Errorf("再生成コントロールに %q が無い", want)
		}
	}
}

func TestServeDetailsHidesEditForUngeneratedPanel(t *testing.T) {
	t.Parallel()

	// 編集モードは既存画像が前提なので、未生成のコマには出してはいけない
	// （go-comic-kit は生成済み画像が無い場合エラーになる）。
	body := renderDetail(t, scriptOnlyState())

	if strings.Contains(body, `data-mode="edit"`) {
		t.Error("画像が無いコマに編集ボタンが出ている")
	}
}
