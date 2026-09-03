package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/shouni/ap-story/internal/domain"
)

func TestComposeFormRenders(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := httptest.NewRequest(http.MethodGet, "/compose", nil)
	rec := httptest.NewRecorder()

	h.ComposeForm(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `name="source_text"`) {
		t.Errorf("body missing source_text field")
	}
}

func postComposeForm(t *testing.T, h *Handler, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.createComicForm(rec, req)
	return rec
}

func TestEnqueueComicFormAcceptsValidSubmission(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	rec := postComposeForm(t, h, url.Values{"source_text": {"元文章"}, "script_mode": {"default"}})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if q.called != 1 {
		t.Errorf("enqueue called %d times, want 1", q.called)
	}
	if q.lastTask.Command != domain.TaskCommandComposeComic || q.lastTask.SourceText != "元文章" {
		t.Errorf("task = %+v, want compose_comic with source text", q.lastTask)
	}
	if !strings.Contains(rec.Body.String(), q.lastTask.JobID) {
		t.Errorf("accepted page does not contain job id %q", q.lastTask.JobID)
	}
}

func TestEnqueueComicFormRejectsMissingSource(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	rec := postComposeForm(t, h, url.Values{})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if q.called != 0 {
		t.Errorf("enqueue called %d times, want 0", q.called)
	}
	if !strings.Contains(rec.Body.String(), "source_url") {
		t.Errorf("error page should mention missing source input, got: %s", rec.Body.String())
	}
}

// このフォームは台本までしか作りません。押した時点では章立てが未実行で、
// 何コマになるか誰も知らないためです。画像は作品詳細から始めます。
func TestEnqueueComicFormAlwaysStopsAfterScript(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	// 画像まで走らせようとする細工（フォームには無い項目）を送っても効かないこと。
	rec := postComposeForm(t, h, url.Values{
		"source_text":       {"元文章"},
		"stop_after_script": {""},
	})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if !q.lastTask.StopAfterScript {
		t.Error("フォームからの投入なのに画像生成まで走る指定になっている")
	}
}

func TestComposeFormHasNoWholeRunControl(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})
	req := httptest.NewRequest(http.MethodGet, "/compose", nil)
	rec := httptest.NewRecorder()

	h.ComposeForm(rec, req)

	if strings.Contains(rec.Body.String(), `name="stop_after_script"`) {
		t.Error("台本までか画像までかを選ばせる操作が残っている（このフォームは台本専用）")
	}
}

// フォームには台本モード・スタイルモード・テキストモデル・画像モデルの選択欄が出ること。
// 選択肢が消えると、既定でしか生成できないことに気付けないまま運用が続きます。
func TestComposeFormRendersChoiceSelects(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})

	req := httptest.NewRequest(http.MethodGet, "/compose", nil)
	rec := httptest.NewRecorder()
	h.ComposeForm(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`name="script_mode"`, `name="style_mode"`, `name="text_model"`,
		`value="watercolor"`, `value="gemini-alt"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("compose form is missing %q", want)
		}
	}
	// 画像モデルはここでは選ばせない。台本の時点ではコマ数も絵柄も分からないので、
	// 選ぶのは作品詳細（画像生成を始める画面）。
	if strings.Contains(body, `name="image_model"`) {
		t.Error("台本生成のフォームに画像モデルの選択が残っている")
	}
}

// 送信した選択はフォーム入力からタスクへ運ばれること。
func TestEnqueueComicFormCarriesChoices(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	rec := postComposeForm(t, h, url.Values{
		"source_text": {"元文章"},
		"script_mode": {"alt"},
		"style_mode":  {"watercolor"},
		"text_model":  {"gemini-alt"},
	})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	task := q.lastTask
	if task.ScriptMode != "alt" || task.StyleMode != "watercolor" {
		t.Errorf("modes = %q/%q, want alt/watercolor", task.ScriptMode, task.StyleMode)
	}
	if task.TextModel != "gemini-alt" {
		t.Errorf("text model = %q, want gemini-alt", task.TextModel)
	}
}

// モデル欄に「既定を使う」空選択は置きません。ブラウザからは常に具体名が送られ、
// どのモデルで作られた作品かが state の記録から辿れるようにするためです。
func TestComposeFormAlwaysSubmitsAModel(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})

	req := httptest.NewRequest(http.MethodGet, "/compose", nil)
	rec := httptest.NewRecorder()
	h.ComposeForm(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, `<option value=""`) {
		t.Errorf("空の選択肢が残っています: %s", body)
	}
	// 未選択のときは先頭（＝既定モデル）が選ばれた状態で表示される。
	for _, want := range []string{
		`<option value="gemini-model" selected>既定: gemini-model</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("既定モデルが選択済みで表示されていません: want %q", want)
		}
	}
}
