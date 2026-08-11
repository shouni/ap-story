package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shouni/ap-story/internal/adapters/prompts"
	"github.com/shouni/ap-story/internal/domain"
)

func TestEnqueueComicSuccess(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	body := `{"source_text": "元文章", "script_mode": "default"}`
	req := httptest.NewRequest(http.MethodPost, "/api/comics", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.EnqueueComic(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if q.called != 1 {
		t.Fatalf("Enqueue called %d times, want 1", q.called)
	}
	if q.lastTask.Command != domain.TaskCommandComposeComic {
		t.Errorf("Command = %q, want %q", q.lastTask.Command, domain.TaskCommandComposeComic)
	}
	if q.lastTask.SourceText != "元文章" {
		t.Errorf("SourceText = %q, want 元文章", q.lastTask.SourceText)
	}
	if err := domain.ValidateJobID(q.lastTask.JobID); err != nil {
		t.Errorf("generated JobID %q is invalid: %v", q.lastTask.JobID, err)
	}

	var resp enqueueResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != "queued" || resp.JobID != q.lastTask.JobID {
		t.Errorf("response = %+v, want status=queued job_id=%s", resp, q.lastTask.JobID)
	}
}

func TestEnqueueComicRejectsMissingSource(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	req := httptest.NewRequest(http.MethodPost, "/api/comics", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()

	h.EnqueueComic(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if q.called != 0 {
		t.Error("Enqueue was called despite invalid submission")
	}
}

func TestEnqueueComicRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	req := httptest.NewRequest(http.MethodPost, "/api/comics", bytes.NewBufferString(`not json`))
	rec := httptest.NewRecorder()

	h.EnqueueComic(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestEnqueueComicRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	req := httptest.NewRequest(http.MethodGet, "/api/comics", nil)
	rec := httptest.NewRecorder()

	h.EnqueueComic(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestEnqueueComicReturns500OnQueueFailure(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{err: context.DeadlineExceeded}
	h := newTestHandler(t, q)

	req := httptest.NewRequest(http.MethodPost, "/api/comics", bytes.NewBufferString(`{"source_text": "x"}`))
	rec := httptest.NewRecorder()

	h.EnqueueComic(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestNewHandlerRequiresTaskQueue(t *testing.T) {
	t.Parallel()

	if _, err := NewHandler(HandlerOptions{Repository: &fakeComicRepository{}}); err == nil {
		t.Error("NewHandler without TaskQueue succeeded, want error")
	}
}

func TestNewHandlerRequiresRepository(t *testing.T) {
	t.Parallel()

	if _, err := NewHandler(HandlerOptions{TaskQueue: &fakeTaskQueue{}}); err == nil {
		t.Error("NewHandler without Repository succeeded, want error")
	}
}

func TestNewHandlerRequiresSigner(t *testing.T) {
	t.Parallel()

	opts := HandlerOptions{TaskQueue: &fakeTaskQueue{}, Repository: &fakeComicRepository{}, Bucket: "b"}
	if _, err := NewHandler(opts); err == nil {
		t.Error("NewHandler without Signer succeeded, want error")
	}
}

func TestNewHandlerRequiresBucket(t *testing.T) {
	t.Parallel()

	opts := HandlerOptions{TaskQueue: &fakeTaskQueue{}, Repository: &fakeComicRepository{}, Signer: &fakeSigner{}}
	if _, err := NewHandler(opts); err == nil {
		t.Error("NewHandler without Bucket succeeded, want error")
	}
}

// モデルと画風の選択は Task へ運ばれること。画像モデルは既存の ModelOverride を使います
// （デザインシートと同じ欄なので、Task 側に2つ目の画像モデルは作りません）。
func TestEnqueueComicCarriesModelAndModeChoices(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	body := `{"source_text": "元文章", "script_mode": "alt", "style_mode": "watercolor",
		"text_model": "gemini-alt", "image_model": "image-alt"}`
	req := httptest.NewRequest(http.MethodPost, "/api/comics", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	h.EnqueueComic(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	task := q.lastTask
	if task.ScriptMode != "alt" || task.StyleMode != "watercolor" {
		t.Errorf("modes = %q/%q, want alt/watercolor", task.ScriptMode, task.StyleMode)
	}
	if task.TextModel != "gemini-alt" || task.ModelOverride != "image-alt" {
		t.Errorf("models = %q/%q, want gemini-alt/image-alt", task.TextModel, task.ModelOverride)
	}
}

// 許可リスト外の選択は投入前に弾くこと。ブラウザは <select> に縛られますが、
// JSON API は任意の文字列を送れます。
func TestEnqueueComicRejectsUnknownChoices(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"台本モード":   `{"source_text": "x", "script_mode": "no-such-mode"}`,
		"スタイルモード": `{"source_text": "x", "style_mode": "no-such-style"}`,
		"テキストモデル": `{"source_text": "x", "text_model": "no-such-model"}`,
		"画像モデル":   `{"source_text": "x", "image_model": "no-such-model"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			q := &fakeTaskQueue{}
			h := newTestHandler(t, q)

			req := httptest.NewRequest(http.MethodPost, "/api/comics", bytes.NewBufferString(body))
			rec := httptest.NewRecorder()

			h.EnqueueComic(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if q.called != 0 {
				t.Errorf("Enqueue called %d times, want 0", q.called)
			}
		})
	}
}

// モデル未指定の JSON 呼び出し（MCP 等）も、既定モデルで埋めた Task になること。
// worker 側の設定へ落とすと、その作品だけ出自が state に残りません。
func TestEnqueueComicFillsInDefaultModels(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	req := httptest.NewRequest(http.MethodPost, "/api/comics", bytes.NewBufferString(`{"source_text": "元文章"}`))
	rec := httptest.NewRecorder()

	h.EnqueueComic(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if q.lastTask.TextModel != "gemini-model" {
		t.Errorf("TextModel = %q, want gemini-model (the head of GEMINI_MODELS)", q.lastTask.TextModel)
	}
	if q.lastTask.ModelOverride != "image-model" {
		t.Errorf("ModelOverride = %q, want image-model (the head of IMAGE_MODELS)", q.lastTask.ModelOverride)
	}
}

// 選択肢を引ける口があること。ブラウザは <select> を見て選べますが、JSON API の
// 呼び出し側には何が指定できるか知る手段がなく、知らずに送れば 400 になります。
func TestComicOptionsListsChoices(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})

	rec := httptest.NewRecorder()
	h.ComicOptions(rec, httptest.NewRequest(http.MethodGet, "/api/comic-options", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp comicOptionsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.ScriptModes) == 0 || len(resp.StyleModes) == 0 {
		t.Fatalf("modes = %+v/%+v, want both listed", resp.ScriptModes, resp.StyleModes)
	}
	// 先頭が既定モデル。フォームと同じ並びで返す。
	if len(resp.TextModels) == 0 || resp.TextModels[0] != "gemini-model" {
		t.Errorf("text models = %v, want gemini-model first", resp.TextModels)
	}
	if len(resp.ImageModels) == 0 || resp.ImageModels[0] != "image-model" {
		t.Errorf("image models = %v, want image-model first", resp.ImageModels)
	}
	// 名前だけでは選べないので、投入時の許可リストと同じ値が名前として返ること。
	if resp.StyleModes[0].Name != prompts.ModeDefault {
		t.Errorf("style modes[0] = %q, want %q", resp.StyleModes[0].Name, prompts.ModeDefault)
	}
}
