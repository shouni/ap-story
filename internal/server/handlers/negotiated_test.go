package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// browserAccept は、ブラウザが実際に送る Accept です。
// application/json を含まないので、表現は HTML 側に倒れます。
const browserAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

// 同じルートが、相手の求める表現で答えること。
//
// ap-mcp の baseClient は全リクエストに Accept: application/json を付けるため、
// /api の別名を消したあとも機械側は JSON を受け取り続けます。
func TestCharactersServesBothAudiences(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})

	t.Run("機械には JSON を返す", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/characters", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		h.Characters(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		var items []characterSummaryResponse
		if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
			t.Fatalf("JSON を読めません: %v", err)
		}
		if len(items) == 0 {
			t.Error("キャラクターが 1 件も返っていない")
		}
	})

	t.Run("人には画面を返す", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/characters", nil)
		req.Header.Set("Accept", browserAccept)
		rec := httptest.NewRecorder()
		h.Characters(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("Content-Type"); strings.HasPrefix(got, "application/json") {
			t.Errorf("Content-Type = %q, want HTML", got)
		}
	})

	// 同じ URL が Accept で中身を変えるため、キャッシュへ伝える必要があります。
	t.Run("Vary: Accept を立てること", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequest(http.MethodGet, "/characters", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		h.Characters(rec, req)

		if got := rec.Header().Get("Vary"); got != "Accept" {
			t.Errorf("Vary = %q, want %q", got, "Accept")
		}
	})
}

// 不正なジョブ ID のエラーも、要求された表現で返ること。
// 機械に HTML のエラーページを返しても解釈できません。
func TestComicErrorFollowsTheRequestedFormat(t *testing.T) {
	t.Parallel()

	h := newTestHandler(t, &fakeTaskQueue{})

	req := httptest.NewRequest(http.MethodGet, "/history/bad", nil)
	req.Header.Set("Accept", "application/json")
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("jobID", "../etc/passwd")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
	rec := httptest.NewRecorder()

	h.Comic(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", got)
	}
}
