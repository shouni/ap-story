package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	kitcomic "github.com/shouni/go-comic-kit/comic"

	"github.com/shouni/ap-story/internal/domain"
)

const scriptJobID = "c-20260811-185837-3ed89ff00505"

func scriptTestState() *kitcomic.MangaState {
	return &kitcomic.MangaState{
		ID:    scriptJobID,
		Title: "作品",
		Chapters: []kitcomic.Chapter{
			{ID: "ch01", Title: "第1話", Summary: "あらすじ"},
		},
		Panels: []kitcomic.Panel{
			{
				ID: "ch01-p01", ChapterID: "ch01", Page: 1,
				Dialogues:  []kitcomic.DialogueLine{{SpeakerID: "zundamon", Text: "元のセリフなのだ", Kind: "speech"}},
				Generation: &kitcomic.GenerationRecord{ImageURL: "gs://b/p01.png", UsedSeed: 7},
			},
			{
				ID: "ch01-p02", ChapterID: "ch01", Page: 2,
				Dialogues: []kitcomic.DialogueLine{{SpeakerID: "metan", Text: "そうよ", Kind: "speech"}},
			},
		},
	}
}

// scriptRequest は chi の URL パラメータを載せたリクエストを組み立てます。
func scriptRequest(t *testing.T, method, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, "/api/comics/"+scriptJobID+"/script", strings.NewReader(body))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("jobID", scriptJobID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

// runningJobStatus は「生成ジョブが処理中」の状態です。
func runningJobStatus() domain.JobStatus {
	status := domain.NewQueuedJobStatus(domain.Task{
		JobID:   scriptJobID,
		Command: domain.TaskCommandRegeneratePanel,
	}, time.Now().UTC())
	status.State = domain.JobStateRunning
	return status
}

func newScriptHandler(t *testing.T) (*Handler, *fakeComicRepository) {
	t.Helper()
	repo := &fakeComicRepository{states: map[string]*kitcomic.MangaState{scriptJobID: scriptTestState()}}
	return newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo), repo
}

func TestGetComicScriptReturnsOnlyTheEditablePart(t *testing.T) {
	t.Parallel()

	h, _ := newScriptHandler(t)
	rec := httptest.NewRecorder()
	h.GetComicScript(rec, scriptRequest(t, http.MethodGet, ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var draft domain.ScriptDraft
	if err := json.Unmarshal(rec.Body.Bytes(), &draft); err != nil {
		t.Fatalf("レスポンスが台本として読めません: %v", err)
	}
	if len(draft.Panels) != 2 || draft.Panels[0].Dialogues[0].Text != "元のセリフなのだ" {
		t.Errorf("台本が取れていません: %+v", draft)
	}
	// 生成記録は編集の入力ではないので、台本には出さない
	if strings.Contains(rec.Body.String(), "used_seed") {
		t.Errorf("生成記録が台本に混ざっています: %s", rec.Body.String())
	}
}

func TestUpdateComicScriptSavesTheEditedLines(t *testing.T) {
	t.Parallel()

	h, repo := newScriptHandler(t)
	body := `{"panels":[
		{"panel_id":"ch01-p01","dialogues":[{"speaker_id":"zundamon","text":"直したのだ","kind":"speech"}]},
		{"panel_id":"ch01-p02","dialogues":[{"speaker_id":"metan","text":"そうよ","kind":"speech"}]}
	]}`

	rec := httptest.NewRecorder()
	h.UpdateComicScript(rec, scriptRequest(t, http.MethodPut, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if len(repo.saved) != 1 {
		t.Fatalf("保存回数 = %d, want 1", len(repo.saved))
	}
	if got := repo.saved[0].Panels[0].Dialogues[0].Text; got != "直したのだ" {
		t.Errorf("保存されたセリフ = %q", got)
	}
	// コマ画像との対応そのものなので、台本の保存で消えてはならない
	if repo.saved[0].Panels[0].Generation == nil {
		t.Error("生成記録が保存で失われています")
	}
}

func TestUpdateComicScriptReportsThePagesThatMustBeRecomposed(t *testing.T) {
	t.Parallel()

	// セリフはページ合成のときに画像モデルが描き込むので、保存しただけでは絵は古いまま。
	// どのページを合成し直せばよいかを返さないと、保存で終わったと勘違いされる。
	h, _ := newScriptHandler(t)
	body := `{"panels":[
		{"panel_id":"ch01-p01","dialogues":[{"speaker_id":"zundamon","text":"元のセリフなのだ","kind":"speech"}]},
		{"panel_id":"ch01-p02","dialogues":[{"speaker_id":"metan","text":"直したわ","kind":"speech"}]}
	]}`

	rec := httptest.NewRecorder()
	h.UpdateComicScript(rec, scriptRequest(t, http.MethodPut, body))

	var got struct {
		ChangedLines  int   `json:"changed_lines"`
		AffectedPages []int `json:"affected_pages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("レスポンスが読めません: %v", err)
	}
	if got.ChangedLines != 1 {
		t.Errorf("changed_lines = %d, want 1", got.ChangedLines)
	}
	// 直したのは2ページ目のコマだけ
	if len(got.AffectedPages) != 1 || got.AffectedPages[0] != 2 {
		t.Errorf("affected_pages = %v, want [2]", got.AffectedPages)
	}
}

func TestUpdateComicScriptDoesNotWriteWhenNothingChanged(t *testing.T) {
	t.Parallel()

	// 何も変えない要求のために state を上書きすると、実行中ジョブとの競合の窓を
	// わざわざ開くことになる。
	h, repo := newScriptHandler(t)
	body := `{"panels":[
		{"panel_id":"ch01-p01","dialogues":[{"speaker_id":"zundamon","text":"元のセリフなのだ","kind":"speech"}]},
		{"panel_id":"ch01-p02","dialogues":[{"speaker_id":"metan","text":"そうよ","kind":"speech"}]}
	]}`

	rec := httptest.NewRecorder()
	h.UpdateComicScript(rec, scriptRequest(t, http.MethodPut, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(repo.saved) != 0 {
		t.Errorf("変更が無いのに保存しています（%d 回）", len(repo.saved))
	}
}

func TestUpdateComicScriptRejectsChangesToThePanelLineup(t *testing.T) {
	t.Parallel()

	h, repo := newScriptHandler(t)
	// コマを1つ落とした要求
	body := `{"panels":[
		{"panel_id":"ch01-p01","dialogues":[{"speaker_id":"zundamon","text":"直したのだ","kind":"speech"}]}
	]}`

	rec := httptest.NewRecorder()
	h.UpdateComicScript(rec, scriptRequest(t, http.MethodPut, body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	if len(repo.saved) != 0 {
		t.Error("拒否したのに保存しています")
	}
}

func TestUpdateComicScriptRefusesWhileAJobIsRunning(t *testing.T) {
	t.Parallel()

	// state は「常に上書き・常に最新」なので、生成ジョブと重なると編集が痕跡なく消える。
	repo := &fakeComicRepository{states: map[string]*kitcomic.MangaState{scriptJobID: scriptTestState()}}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)
	status := &fakeJobStatusStore{}
	if err := status.Save(t.Context(), scriptJobID, runningJobStatus()); err != nil {
		t.Fatalf("状態の記録に失敗: %v", err)
	}
	h.jobStatus = status

	body := `{"panels":[
		{"panel_id":"ch01-p01","dialogues":[{"speaker_id":"zundamon","text":"直したのだ","kind":"speech"}]},
		{"panel_id":"ch01-p02","dialogues":[{"speaker_id":"metan","text":"そうよ","kind":"speech"}]}
	]}`
	rec := httptest.NewRecorder()
	h.UpdateComicScript(rec, scriptRequest(t, http.MethodPut, body))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body: %s)", rec.Code, rec.Body.String())
	}
	if len(repo.saved) != 0 {
		t.Error("実行中なのに保存しています")
	}
}

func TestUpdateComicScriptAllowsEditingWhenNoStatusWasEverRecorded(t *testing.T) {
	t.Parallel()

	// 状態を追えないことを理由に編集まで止めると、状態記録より前に作られた作品が
	// 永久に直せなくなる。
	repo := &fakeComicRepository{states: map[string]*kitcomic.MangaState{scriptJobID: scriptTestState()}}
	h := newTestHandlerWithRepo(t, &fakeTaskQueue{}, repo)
	h.jobStatus = &fakeJobStatusStore{} // 何も記録されていない = Get はエラー

	body := `{"panels":[
		{"panel_id":"ch01-p01","dialogues":[{"speaker_id":"zundamon","text":"直したのだ","kind":"speech"}]},
		{"panel_id":"ch01-p02","dialogues":[{"speaker_id":"metan","text":"そうよ","kind":"speech"}]}
	]}`
	rec := httptest.NewRecorder()
	h.UpdateComicScript(rec, scriptRequest(t, http.MethodPut, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateComicScriptRejectsAnUnknownSpeaker(t *testing.T) {
	t.Parallel()

	h, _ := newScriptHandler(t)
	body := `{"panels":[
		{"panel_id":"ch01-p01","dialogues":[{"speaker_id":"dareka","text":"誰なのだ","kind":"speech"}]},
		{"panel_id":"ch01-p02","dialogues":[{"speaker_id":"metan","text":"そうよ","kind":"speech"}]}
	]}`

	rec := httptest.NewRecorder()
	h.UpdateComicScript(rec, scriptRequest(t, http.MethodPut, body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}
