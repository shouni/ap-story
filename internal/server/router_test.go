package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/shouni/gcp-kit/auth/oidc"
	"github.com/shouni/gcp-kit/auth/session"

	"github.com/gorilla/sessions"
	"github.com/shouni/ap-story/internal/builder"
	"github.com/shouni/ap-story/internal/server/handlers"
	"github.com/shouni/gcp-kit/auth"
)

const (
	testSessionAuthKey    = "session-auth-key-32-bytes-long!"
	testSessionEncryptKey = "0123456789abcdef"
	testSessionName       = "test-session"
)

func testAuthHandler(t *testing.T) *session.Handler {
	t.Helper()
	h, err := session.New(session.Config{
		ClientID:          "client-id",
		ClientSecret:      "client-secret",
		RedirectURL:       "https://example.com/auth/callback",
		SessionAuthKey:    testSessionAuthKey,
		SessionEncryptKey: testSessionEncryptKey,
		SessionName:       testSessionName,
		// 許可リストが空だと fail-closed で全員拒否されます。
		AllowedDomains: []string{"example.com"},
	})
	if err != nil {
		t.Fatalf("session.New failed: %v", err)
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
	m2m := oidc.New("https://example.com", nil)

	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	mw := auth.Protected(m2m, authHandler)(next)

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

func TestProtectedAccessRejectsInvalidM2MToken(t *testing.T) {
	t.Parallel()

	authHandler := testAuthHandler(t)
	m2m := oidc.New("https://example.com", []string{"sa@project.iam.gserviceaccount.com"})

	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	mw := auth.Protected(m2m, authHandler)(next)

	req := httptest.NewRequest(http.MethodGet, "/web/history", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	// 提示した資格情報が通らなかった呼び出しは、そこで確定します。ログイン画面へ
	// 送ると、JSON を求めているエージェントが HTML を受け取ることになります。
	if called {
		t.Error("next handler was called, want the invalid token to be rejected")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (invalid_token)", rec.Code, http.StatusUnauthorized)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Error("WWW-Authenticate が無い（RFC 9110 §15.5.2）")
	}
	if got := rec.Header().Get("Location"); got != "" {
		t.Errorf("Location = %q, want empty（ログイン画面へ送らない）", got)
	}
}

// TestCSRFTokenReachesTemplates は、認証を通った GET で CSRF トークンが
// テンプレート側から読めることを確認します。
//
// トークンの発行は gcp-kit の session.Authenticate が行います。ここで見たいのは
// handlers.CSRFTokenFromContext が同じキーを読めていることで、ずれるとフォームの
// hidden が空になり、以降の POST が全て弾かれます。
func TestCSRFTokenReachesTemplates(t *testing.T) {
	t.Parallel()

	authHandler := testAuthHandler(t)
	var gotToken string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotToken = handlers.CSRFTokenFromContext(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/web/history", nil)
	req.AddCookie(loggedInCookie(t, authHandler))
	rec := httptest.NewRecorder()
	auth.Require(authHandler)(next).ServeHTTP(rec, req)

	if gotToken == "" {
		t.Error("CSRF token was not generated and propagated to context")
	}
	if rec.Result().Cookies() == nil {
		t.Error("no session cookie was set to persist the generated CSRF token")
	}
}

// loggedInCookie は、ログイン済みセッションを表すクッキーを返します。
//
// OAuth のコールバックを経ずにログイン状態を作るため、Handler と同じ鍵で
// 組み立てたストアへ直接書き込みます。testAuthHandler と鍵・セッション名が
// ずれると Handler 側が読めないので、定数を共有しています。
func loggedInCookie(t *testing.T, _ *session.Handler) *http.Cookie {
	t.Helper()

	store := sessions.NewCookieStore([]byte(testSessionAuthKey), []byte(testSessionEncryptKey))
	store.Options = &sessions.Options{Path: "/", MaxAge: 3600, HttpOnly: true, SameSite: http.SameSiteLaxMode}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	sess, err := store.Get(req, testSessionName)
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	sess.Values[session.DefaultUserSessionKey] = "user@example.com"
	if err := sess.Save(req, rec); err != nil {
		t.Fatalf("session.Save() error = %v", err)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("セッションクッキーが生成されていない")
	}
	return cookies[0]
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
		TaskAuth: oidc.New("https://worker.example.com", []string{"tasks@example.iam.gserviceaccount.com"}),
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
		TaskAuth: oidc.New("https://worker.example.com", []string{"tasks@example.iam.gserviceaccount.com"}),
	}, "")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestNewRouterRegistersTheScriptRoutes は、台本の取得と保存の口が両方とも
// 生えていることを確認します。片方だけ生えていても画面は静かに壊れるだけで、
// ビルドもテストも通ってしまうため、経路そのものを固定します。
func TestNewRouterRegistersTheScriptRoutes(t *testing.T) {
	t.Parallel()

	router := NewRouter(&builder.AppHandlers{
		Auth: testAuthHandler(t),
		Web:  &handlers.Handler{},
	}, "")

	want := map[string]string{
		http.MethodGet: "/api/comics/{jobID}/script",
		http.MethodPut: "/api/comics/{jobID}/script",
	}
	found := map[string]bool{}
	walk := func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if want[method] == route {
			found[method] = true
		}
		return nil
	}
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatalf("router が chi.Routes ではありません: %T", router)
	}
	if err := chi.Walk(routes, walk); err != nil {
		t.Fatalf("chi.Walk failed: %v", err)
	}

	for method := range want {
		if !found[method] {
			t.Errorf("%s %s が登録されていません", method, want[method])
		}
	}
}

// バージョン付きの vendor と、URL が変わらない自前アセットで Cache-Control を分けること。
func TestStaticCacheControlSeparatesVendorFromOwnAssets(t *testing.T) {
	t.Parallel()

	router := NewRouter(nil, "")

	tests := []struct {
		target string
		want   string
	}{
		{"/static/vendor/bootstrap-5.3.8/bootstrap.min.css", vendorCacheControl},
		{"/static/css/app.css", ownAssetCacheControl},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.target, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("%s = %d, want 200", tt.target, rec.Code)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.want {
				t.Errorf("Cache-Control = %q, want %q", got, tt.want)
			}
		})
	}
}

// CSP が全レスポンスに付き、script-src が緩められていないこと。
func TestResponsesCarryContentSecurityPolicy(t *testing.T) {
	t.Parallel()

	router := NewRouter(nil, "")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	policy := rec.Header().Get("Content-Security-Policy")
	if policy == "" {
		t.Fatal("Content-Security-Policy が付いていない")
	}
	for _, want := range []string{"default-src 'self'", "script-src 'self'", "object-src 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(policy, want) {
			t.Errorf("CSP に %q が無い: %s", want, policy)
		}
	}
	// script-src の 'unsafe-inline' はインラインスクリプト禁止（assets の
	// TestTemplatesHaveNoInlineScripts）を無意味にします。style-src 側は許容しているので
	// 区間を限って見ます。
	scriptSrc := cspDirective(policy, "script-src")
	if scriptSrc == "" {
		t.Fatalf("script-src が無い: %s", policy)
	}
	if strings.Contains(scriptSrc, "unsafe-inline") || strings.Contains(scriptSrc, "unsafe-eval") {
		t.Errorf("script-src が緩められています: %s", scriptSrc)
	}
	// 漫画とキャラクターの画像は署名付き URL へ 302 するため、img-src が送り先を許す必要があります。
	if !strings.Contains(cspDirective(policy, "img-src"), "https://storage.googleapis.com") {
		t.Errorf("img-src が署名付き URL のホストを許していない: %s", policy)
	}
}

// cspDirective は CSP から 1 ディレクティブ分を取り出します。無ければ空文字を返します。
func cspDirective(policy, name string) string {
	for directive := range strings.SplitSeq(policy, ";") {
		directive = strings.TrimSpace(directive)
		if after, ok := strings.CutPrefix(directive, name+" "); ok {
			return after
		}
	}
	return ""
}

// 圧縮が効いていること。画面は日本語 UTF-8（1 文字 3 バイト）でよく縮むのに、
// これまで無圧縮で配信していました。
func TestCompressibleResponsesAreCompressed(t *testing.T) {
	t.Parallel()

	router := NewRouter(nil, "")
	req := httptest.NewRequest(http.MethodGet, "/static/vendor/bootstrap-5.3.8/bootstrap.min.css", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}

	reader, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	defer func() { _ = reader.Close() }()

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("解凍できない: %v", err)
	}
	if !strings.Contains(string(body), "Bootstrap") {
		t.Error("解凍した中身が Bootstrap の CSS でない")
	}
	if len(body) <= rec.Body.Len() {
		t.Errorf("圧縮後 %d バイトが元の %d バイトを下回っていない", rec.Body.Len(), len(body))
	}
}

// CSP 以外の防御ヘッダーも全レスポンスに付くこと。どれも 1 行で入る割に、
// 抜けても画面は正常に見えるため気付けません。
func TestResponsesCarrySecurityHeaders(t *testing.T) {
	t.Parallel()

	router := NewRouter(nil, "")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	want := map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"Referrer-Policy":           "same-origin",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
	}
	for name, value := range want {
		if got := rec.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	// autoplay は塞ぎません（メディア再生が壊れます）。
	policy := rec.Header().Get("Permissions-Policy")
	if policy == "" {
		t.Error("Permissions-Policy が付いていない")
	}
	if strings.Contains(policy, "autoplay") {
		t.Errorf("Permissions-Policy が autoplay を塞いでいます: %s", policy)
	}
}
