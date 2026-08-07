package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shouni/gcp-kit/auth"

	"github.com/shouni/ap-story/internal/builder"
	"github.com/shouni/ap-story/internal/server/handlers"
)

func testAuthHandler(t *testing.T) *auth.Handler {
	t.Helper()
	h, err := auth.NewHandler(auth.Config{
		ClientID:          "client-id",
		ClientSecret:      "client-secret",
		RedirectURL:       "https://example.com/auth/callback",
		SessionAuthKey:    "session-auth-key-32-bytes-long!",
		SessionEncryptKey: "0123456789abcdef",
		SessionName:       "test-session",
	})
	if err != nil {
		t.Fatalf("auth.NewHandler failed: %v", err)
	}
	return h
}

func TestNewRouterHealthz(t *testing.T) {
	t.Parallel()

	router := NewRouter(nil, "")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "ok")
	}
}

func TestNewRouterNilHandlersDoesNotPanic(t *testing.T) {
	t.Parallel()

	router := NewRouter(nil, "")
	req := httptest.NewRequest(http.MethodPost, "/tasks/generate", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (no worker route registered without handlers)", rec.Code, http.StatusNotFound)
	}
}

func TestProtectedAccessMiddlewareFallsBackToSessionWithoutM2MToken(t *testing.T) {
	t.Parallel()

	authHandler := testAuthHandler(t)
	m2m := auth.NewM2MVerifier("https://example.com", nil)

	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	mw := authHandler.ProtectedMiddleware(m2m)(next)

	req := httptest.NewRequest(http.MethodGet, "/web/history", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	// M2M トークンが無いので、未ログインのセッション認証にフォールバックし
	// ログイン画面へリダイレクトされる（next には到達しない）。
	if called {
		t.Error("next handler was called, want fallback to session auth (no valid session)")
	}
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect to login)", rec.Code, http.StatusFound)
	}
}

func TestProtectedAccessMiddlewareRejectsInvalidM2MToken(t *testing.T) {
	t.Parallel()

	authHandler := testAuthHandler(t)
	m2m := auth.NewM2MVerifier("https://example.com", []string{"sa@project.iam.gserviceaccount.com"})

	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	mw := authHandler.ProtectedMiddleware(m2m)(next)

	req := httptest.NewRequest(http.MethodGet, "/web/history", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	// 不正なトークンは M2M 認証に失敗するため、セッション認証にフォールバックする。
	if called {
		t.Error("next handler was called, want M2M rejection to fall back to session auth")
	}
	if rec.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect to login after M2M failure)", rec.Code, http.StatusFound)
	}
}

func TestCSRFAutoGenMiddlewareGeneratesTokenOnGet(t *testing.T) {
	t.Parallel()

	authHandler := testAuthHandler(t)
	var gotToken string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotToken = handlers.CSRFTokenFromContext(r.Context())
	})
	mw := authHandler.CSRFContextMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/web/history", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if gotToken == "" {
		t.Error("CSRF token was not generated and propagated to context")
	}
	if rec.Result().Cookies() == nil {
		t.Error("no session cookie was set to persist the generated CSRF token")
	}
}

// TestNewRouterOmitsWorkerRoutesWithoutTaskAuth は、SERVER_ROLE=web のプロセスで
// /tasks/generate が登録されないことを確認します。
//
// 見るのは「401 で拒否される」ではなく「404 でルートが無い」ことです。分離の目的は
// 公開サービス上からタスク受付口を消すことなので、アプリのコードが応答する余地を
// 残していない状態を確かめる必要があります。
func TestNewRouterOmitsWorkerRoutesWithoutTaskAuth(t *testing.T) {
	t.Parallel()

	// Web 面だけを担う構成: TaskAuth も Worker も nil。
	router := NewRouter(&builder.AppHandlers{Auth: testAuthHandler(t)}, "")
	req := httptest.NewRequest(http.MethodPost, "/tasks/generate", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (worker route must not be registered for the web role)", rec.Code, http.StatusNotFound)
	}
}

// TestNewRouterOmitsWebRoutesWithoutWebHandler は、SERVER_ROLE=worker のプロセスで
// Web 面のルートが登録されないことを確認します。
func TestNewRouterOmitsWebRoutesWithoutWebHandler(t *testing.T) {
	t.Parallel()

	// Worker 面だけを担う構成: Auth も Web も nil。
	router := NewRouter(&builder.AppHandlers{
		TaskAuth: auth.NewTaskVerifier("https://worker.example.com", []string{"tasks@example.iam.gserviceaccount.com"}),
	}, "")

	for _, path := range []string{"/", "/api/comics"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want %d (web routes must not be registered for the worker role)", path, rec.Code, http.StatusNotFound)
		}
	}
}

// TestNewRouterKeepsHealthzForWorkerRole は、Worker 面だけの構成でも
// ヘルスチェックが残ることを確認します。Cloud Run の起動判定に使われます。
func TestNewRouterKeepsHealthzForWorkerRole(t *testing.T) {
	t.Parallel()

	router := NewRouter(&builder.AppHandlers{
		TaskAuth: auth.NewTaskVerifier("https://worker.example.com", []string{"tasks@example.iam.gserviceaccount.com"}),
	}, "")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
