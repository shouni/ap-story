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

// コマとページはボタンを分けます。ページはコマを並べた合成物なので、
// コマの出来を見てから合成へ進めるようにするためです。
func TestServeDetailsOffersPanelsFirstForScriptOnlyState(t *testing.T) {
	t.Parallel()

	body := renderDetail(t, scriptOnlyState())

	if !strings.Contains(body, `data-stage="panels"`) {
		t.Error("台本だけの state に「コマを生成」ボタンが出ていない")
	}
	if !strings.Contains(body, "コマを生成") {
		t.Error("画像が1枚も無いのに「続きのコマを生成」表記になっている")
	}
	if strings.Contains(body, "ページを合成") {
		t.Error("コマが1枚も無いのにページ合成ボタンが出ている")
	}
}

// コマが揃ったら、次はページ合成だけを出します。
func TestServeDetailsOffersPageCompositionAfterPanels(t *testing.T) {
	t.Parallel()

	state := scriptOnlyState()
	for i := range state.Panels {
		state.Panels[i].Generation = &kitcomic.GenerationRecord{ImageURL: "gs://test-bucket/comics/job-1/images/p.png"}
	}

	body := renderDetail(t, state)

	if !strings.Contains(body, "ページを合成") {
		t.Error("コマが揃ったのにページ合成ボタンが出ていない")
	}
	if strings.Contains(body, `data-stage="panels"`) {
		t.Error("コマが揃っているのにコマ生成ボタンが残っている")
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
		t.Error("未生成のコマが残る state に再開ボタンが出ていない")
	}
	if !strings.Contains(body, "続きのコマを生成") {
		t.Error("生成済みがあるのに「続きのコマを生成」表記になっていない")
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
			t.Errorf("全て生成済みなのに %q のボタンが出ている", unwanted)
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

// 章カードには「この章の画像を生成」が並びます。画像はいちばん高価な工程なので、
// 作品まるごとではなく章単位で試せる入口を、台本再生成と同じ場所に置いています。
func TestServeDetailsRendersChapterRenderControl(t *testing.T) {
	t.Parallel()

	body := renderDetail(t, scriptOnlyState())

	for _, want := range []string{
		`data-command="render_comic"`,
		`data-target="ch01"`,
		`この章のコマを生成`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("章カードに %q が無い", want)
		}
	}
}

// 台本がまだ無い章に画像生成のボタンを出しても、押せば0件で終わるだけです。
func TestServeDetailsHidesChapterRenderWithoutPanels(t *testing.T) {
	t.Parallel()

	state := scriptOnlyState()
	state.Panels = nil
	state.Chapters[0].PanelIDs = nil

	if body := renderDetail(t, state); strings.Contains(body, `この章のコマを生成`) {
		t.Error("コマの無い章に画像生成ボタンが出ている")
	}
}

// 画像モデルは台本生成のフォームではなく、画像生成を始めるこの画面で選びます。
// 初期選択は state に記録された値で、押し続ける限り作品内で揃います。
func TestServeDetailsOffersImageModelDefaultedToRecorded(t *testing.T) {
	t.Parallel()

	state := scriptOnlyState()
	state.ImageModel = "image-alt"

	body := renderDetail(t, state)

	if !strings.Contains(body, "js-image-model") {
		t.Fatal("画像モデルの選択が出ていない")
	}
	if !strings.Contains(body, `<option value="image-alt" selected>`) {
		t.Errorf("記録済みのモデルが初期選択になっていない:\n%s", body)
	}
}

// まだ記録が無い作品（台本だけ）は、一覧の先頭が初期選択になります。
func TestServeDetailsDefaultsImageModelToHeadWhenUnrecorded(t *testing.T) {
	t.Parallel()

	body := renderDetail(t, scriptOnlyState())

	if !strings.Contains(body, `<option value="image-model" selected>`) {
		t.Errorf("既定モデルが初期選択になっていない:\n%s", body)
	}
}

// 生成し終えた作品には、押すボタンが無いので選択も出しません。
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
		t.Error("生成が済んでいるのに画像モデルの選択が出ている")
	}
}

// TestServeDetailsOrdersPagesByNumber は、ページが読む順に並ぶことを確認します。
//
// state.Pages は合成した順に並びます（go-comic-kit の SetPageArtifact は新規ページを
// 末尾に足すだけ）。ページ3だけ先に作り直すと state 上は 3, 1, 2 の順になり、
// 通し読みでは順序が狂っていること自体に気づけません。
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
		// ページ3を先に作り直した状態
		Pages: []kitcomic.PageArtifact{page(3), page(1), page(2)},
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

	last := -1
	for n := 1; n <= 3; n++ {
		at := strings.Index(body, fmt.Sprintf("comic_page_%d.png", n))
		if at < 0 {
			t.Fatalf("ページ %d が出力されていません", n)
		}
		if at < last {
			t.Errorf("ページ %d が前のページより先に出ています（読む順に並んでいません）", n)
		}
		last = at
	}
}
