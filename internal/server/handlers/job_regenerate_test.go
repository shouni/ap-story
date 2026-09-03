package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shouni/ap-story/internal/domain"
)

func regenerateRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	return httptestRequestWithURLParam(t, http.MethodPost, "/jobs/job-1/regenerate", body, "jobID", "job-1")
}

func TestRegenerateComicChapterScript(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	body := `{"command": "regenerate_chapter_script", "chapter_id": "ch01"}`
	req := regenerateRequest(t, body)
	rec := httptest.NewRecorder()
	h.JobRegenerate(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if q.lastTask.Command != domain.TaskCommandRegenerateChapterScript {
		t.Errorf("Command = %q, want regenerate_chapter_script", q.lastTask.Command)
	}
	if q.lastTask.JobID != "job-1" {
		t.Errorf("JobID = %q, want job-1 (from URL, not body)", q.lastTask.JobID)
	}
	if q.lastTask.ChapterID != "ch01" {
		t.Errorf("ChapterID = %q, want ch01", q.lastTask.ChapterID)
	}
}

func TestRegenerateComicDesignSheet(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	body := `{"command": "generate_design_sheet", "character_ids": ["zundamon", "metan"], "aspect_ratio": "9:16"}`
	req := regenerateRequest(t, body)
	rec := httptest.NewRecorder()
	h.JobRegenerate(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if len(q.lastTask.CharacterIDs) != 2 || q.lastTask.AspectRatio != "9:16" {
		t.Errorf("task = %+v, unexpected", q.lastTask)
	}
}

func TestRegenerateComicPanelWithSeed(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	body := `{"command": "regenerate_panel", "panel_id": "ch01-p01", "seed": 42}`
	req := regenerateRequest(t, body)
	rec := httptest.NewRecorder()
	h.JobRegenerate(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if q.lastTask.PanelID != "ch01-p01" {
		t.Errorf("PanelID = %q, want ch01-p01", q.lastTask.PanelID)
	}
	if q.lastTask.Seed == nil || *q.lastTask.Seed != 42 {
		t.Errorf("Seed = %v, want 42", q.lastTask.Seed)
	}
}

func TestRegenerateComicPanelWithEditPrompt(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	body := `{"command": "regenerate_panel", "panel_id": "ch01-p01", "edit_prompt": "表情を笑顔に変える"}`
	req := regenerateRequest(t, body)
	rec := httptest.NewRecorder()
	h.JobRegenerate(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if q.lastTask.EditPrompt != "表情を笑顔に変える" {
		t.Errorf("EditPrompt = %q, want 表情を笑顔に変える", q.lastTask.EditPrompt)
	}
}

func TestRegenerateComicPage(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	body := `{"command": "regenerate_page", "page": 2}`
	req := regenerateRequest(t, body)
	rec := httptest.NewRecorder()
	h.JobRegenerate(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if q.lastTask.Page != 2 {
		t.Errorf("Page = %d, want 2", q.lastTask.Page)
	}
}

func TestRegenerateComicJobIDComesFromURLNotBody(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	// リクエストボディに紛れ込んだ job_id は無視され、URL の jobID が使われる。
	body := `{"command": "regenerate_page", "page": 1, "job_id": "spoofed-job"}`
	req := regenerateRequest(t, body)
	rec := httptest.NewRecorder()
	h.JobRegenerate(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if q.lastTask.JobID != "job-1" {
		t.Errorf("JobID = %q, want job-1 (URL must win over body)", q.lastTask.JobID)
	}
}

func TestRegenerateComicRejectsComposeCommand(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	body := `{"command": "compose_comic", "source_text": "x"}`
	req := regenerateRequest(t, body)
	rec := httptest.NewRecorder()
	h.JobRegenerate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if q.called != 0 {
		t.Error("Enqueue was called despite compose_comic being rejected")
	}
}

func TestRegenerateComicRejectsInvalidJobIDInURL(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	req := httptestRequestWithURLParam(t, http.MethodPost, "/jobs/../escape/regenerate",
		`{"command": "regenerate_page", "page": 1}`, "jobID", "../escape")
	rec := httptest.NewRecorder()
	h.JobRegenerate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if q.called != 0 {
		t.Error("Enqueue was called despite invalid job id")
	}
}

func TestRegenerateComicRejectsMissingRequiredField(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	// regenerate_panel なのに panel_id が無い
	body := `{"command": "regenerate_panel"}`
	req := regenerateRequest(t, body)
	rec := httptest.NewRecorder()
	h.JobRegenerate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRegenerateComicRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	req := httptestRequestWithURLParam(t, http.MethodGet, "/jobs/job-1/regenerate", "", "jobID", "job-1")
	rec := httptest.NewRecorder()
	h.JobRegenerate(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestRegenerateComicAcceptsRenderComic(t *testing.T) {
	t.Parallel()

	q := &fakeTaskQueue{}
	h := newTestHandler(t, q)

	// 詳細画面の「続きを生成」ボタンが送る内容。専用エンドポイントは設けず、
	// 既存の regenerate エンドポイントがそのまま受け付ける。
	req := regenerateRequest(t, `{"command": "render_comic"}`)
	rec := httptest.NewRecorder()
	h.JobRegenerate(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if q.lastTask.Command != domain.TaskCommandRenderComic {
		t.Errorf("Command = %q, want render_comic", q.lastTask.Command)
	}
	if q.lastTask.JobID != "job-1" {
		t.Errorf("JobID = %q, want job-1", q.lastTask.JobID)
	}
}

func TestRegenerateComicPanelWithSeedAndEditPrompt(t *testing.T) {
	t.Parallel()

	t.Run("シード振り直し", func(t *testing.T) {
		q := &fakeTaskQueue{}
		h := newTestHandler(t, q)

		req := regenerateRequest(t, `{"command": "regenerate_panel", "panel_id": "ch01-p01", "seed": 12345}`)
		rec := httptest.NewRecorder()
		h.JobRegenerate(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
		}
		if q.lastTask.Seed == nil || *q.lastTask.Seed != 12345 {
			t.Errorf("Seed = %v, want 12345", q.lastTask.Seed)
		}
	})

	t.Run("指示による編集", func(t *testing.T) {
		q := &fakeTaskQueue{}
		h := newTestHandler(t, q)

		req := regenerateRequest(t, `{"command": "regenerate_panel", "panel_id": "ch01-p01", "edit_prompt": "表情を笑顔に"}`)
		rec := httptest.NewRecorder()
		h.JobRegenerate(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusAccepted, rec.Body.String())
		}
		if q.lastTask.EditPrompt != "表情を笑顔に" {
			t.Errorf("EditPrompt = %q, want 表情を笑顔に", q.lastTask.EditPrompt)
		}
	})
}

// 再生成の経路も許可リストを通ること。compose とデザインシートだけ塞いで
// ここが空いていると、Cloud Tasks を1往復してから worker で落ちます。
func TestRegenerateRejectsUnknownChoices(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"画像モデル":   `{"command": "regenerate_panel", "panel_id": "ch01-p01", "model_override": "no-such-model"}`,
		"スタイルモード": `{"command": "generate_design_sheet", "character_ids": ["zundamon"], "style_mode": "no-such-style"}`,
		"テキストモデル": `{"command": "regenerate_chapter_script", "chapter_id": "ch01", "text_model": "no-such-model"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			q := &fakeTaskQueue{}
			h := newTestHandler(t, q)

			rec := httptest.NewRecorder()
			h.JobRegenerate(rec, regenerateRequest(t, body))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if q.called != 0 {
				t.Errorf("Enqueue called %d times, want 0", q.called)
			}
		})
	}
}
