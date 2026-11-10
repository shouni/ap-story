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

	"github.com/shouni/ap-story/internal/builder"
	"github.com/shouni/ap-story/internal/server/handlers"
	"github.com/shouni/gcp-kit/auth"
)

const testSessionName = "test-session"

func testAuthHandler(t *testing.T) *session.Handler {
	t.Helper()
	h, err := session.New(session.Config{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		ServiceURL:   "https://example.com",
		SessionName:  testSessionName,
		Store:        session.NewMemoryStore(),
		// An empty allowlist fails closed and rejects everyone.
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
	m2m := mustOIDC(t, "https://example.com", []string{"sa@project.iam.gserviceaccount.com"})

	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	mw := auth.Protected(m2m, authHandler)(next)

	req := httptest.NewRequest(http.MethodGet, "/web/history", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	// There is no M2M token, so it falls back to session auth as not logged in
	// and is redirected to the login page (next is never reached).
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
	m2m := mustOIDC(t, "https://example.com", []string{"sa@project.iam.gserviceaccount.com"})

	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	mw := auth.Protected(m2m, authHandler)(next)

	req := httptest.NewRequest(http.MethodGet, "/web/history", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	// A call whose presented credentials fail is settled right there. Sending it to the login
	// page would hand HTML to an agent that asked for JSON.
	if called {
		t.Error("next handler was called, want the invalid token to be rejected")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d (invalid_token)", rec.Code, http.StatusUnauthorized)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Error("no WWW-Authenticate (RFC 9110, Section 15.5.2)")
	}
	if got := rec.Header().Get("Location"); got != "" {
		t.Errorf("Location = %q, want empty (not sent to the login page)", got)
	}
}

// TestCSRFTokenReachesTemplates checks that on an authenticated GET the CSRF token can be
// read from the template side.
//
// The token is issued by gcp-kit's session.Authenticate. What this looks at is that
// handlers.CSRFTokenFromContext reads the same key; if they drift, the form's hidden
// field goes empty and every later POST is rejected.
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

// loggedInCookie creates a logged-in session with h and returns its cookie.
//
// It creates the logged-in state without going through the OAuth callback. The cookie is baked by h
// itself: the session's substance lives in the store, so one assembled by hand could not be read.
func loggedInCookie(t *testing.T, h *session.Handler) *http.Cookie {
	t.Helper()

	rec := httptest.NewRecorder()
	if err := h.IssueSession(rec, httptest.NewRequest(http.MethodGet, "/", nil), "user@example.com"); err != nil {
		t.Fatalf("IssueSession() error = %v", err)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie was created")
	}
	return cookies[0]
}

// TestNewRouterOmitsWorkerRoutesWithoutTaskAuth checks that /tasks/generate is not registered
// in a SERVER_ROLE=web process.
//
// What it looks for is "404, no route" rather than "rejected with 401". The point of the split is
// to remove the task intake from the public service, so it must confirm that no room is left
// for the app's code to answer.
func TestNewRouterOmitsWorkerRoutesWithoutTaskAuth(t *testing.T) {
	t.Parallel()

	// A web-face-only configuration: TaskAuth and Worker are both nil.
	router := NewRouter(&builder.AppHandlers{Auth: testAuthHandler(t)}, "")
	req := httptest.NewRequest(http.MethodPost, "/tasks/generate", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (worker route must not be registered for the web role)", rec.Code, http.StatusNotFound)
	}
}

// TestNewRouterOmitsWebRoutesWithoutWebHandler checks that the web-face routes are not registered
// in a SERVER_ROLE=worker process.
func TestNewRouterOmitsWebRoutesWithoutWebHandler(t *testing.T) {
	t.Parallel()

	// A worker-face-only configuration: Auth and Web are both nil.
	router := NewRouter(&builder.AppHandlers{
		TaskAuth: mustOIDC(t, "https://worker.example.com", []string{"tasks@example.iam.gserviceaccount.com"}),
	}, "")

	for _, path := range []string{"/", "/jobs"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want %d (web routes must not be registered for the worker role)", path, rec.Code, http.StatusNotFound)
		}
	}
}

// TestNewRouterKeepsHealthzForWorkerRole checks that the health check remains even in a
// worker-face-only configuration. Cloud Run uses it to decide that startup succeeded.
func TestNewRouterKeepsHealthzForWorkerRole(t *testing.T) {
	t.Parallel()

	router := NewRouter(&builder.AppHandlers{
		TaskAuth: mustOIDC(t, "https://worker.example.com", []string{"tasks@example.iam.gserviceaccount.com"}),
	}, "")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestNewRouterRegistersTheScriptRoutes checks that both the script read and save routes
// exist. With only one of them the screen just breaks quietly while the build and the tests
// pass, so the routes themselves are pinned.
func TestNewRouterRegistersTheScriptRoutes(t *testing.T) {
	t.Parallel()

	router := NewRouter(&builder.AppHandlers{
		Auth: testAuthHandler(t),
		Web:  &handlers.Handler{},
	}, "")

	want := map[string]string{
		http.MethodGet: "/jobs/{jobID}/script",
		http.MethodPut: "/jobs/{jobID}/script",
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
		t.Fatalf("router is not a chi.Routes: %T", router)
	}
	if err := chi.Walk(routes, walk); err != nil {
		t.Fatalf("chi.Walk failed: %v", err)
	}

	for method := range want {
		if !found[method] {
			t.Errorf("%s %s is not registered", method, want[method])
		}
	}
}

// The CSP is on every response, and script-src has not been loosened.
func TestResponsesCarryContentSecurityPolicy(t *testing.T) {
	t.Parallel()

	router := NewRouter(nil, "")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	policy := rec.Header().Get("Content-Security-Policy")
	if policy == "" {
		t.Fatal("no Content-Security-Policy")
	}
	for _, want := range []string{"default-src 'self'", "script-src 'self'", "object-src 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(policy, want) {
			t.Errorf("CSP lacks %q: %s", want, policy)
		}
	}
	// 'unsafe-inline' in script-src would make the ban on inline scripts (assets'
	// TestTemplatesHaveNoInlineScripts) meaningless. style-src does allow it, so only that
	// directive is examined.
	scriptSrc := cspDirective(policy, "script-src")
	if scriptSrc == "" {
		t.Fatalf("no script-src: %s", policy)
	}
	if strings.Contains(scriptSrc, "unsafe-inline") || strings.Contains(scriptSrc, "unsafe-eval") {
		t.Errorf("script-src has been loosened: %s", scriptSrc)
	}
	// Comic and character images 302 to a signed URL, so img-src must allow the destination.
	if !strings.Contains(cspDirective(policy, "img-src"), "https://storage.googleapis.com") {
		t.Errorf("img-src does not allow the signed URL's host: %s", policy)
	}
}

// cspDirective extracts one directive from the CSP. It returns the empty string if absent.
func cspDirective(policy, name string) string {
	for directive := range strings.SplitSeq(policy, ";") {
		directive = strings.TrimSpace(directive)
		if after, ok := strings.CutPrefix(directive, name+" "); ok {
			return after
		}
	}
	return ""
}

// Compression is in effect. The screens are Japanese UTF-8 (3 bytes per character) and shrink well,
// yet they used to be served uncompressed.
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
		t.Fatalf("cannot decompress: %v", err)
	}
	if !strings.Contains(string(body), "Bootstrap") {
		t.Error("the decompressed content is not Bootstrap's CSS")
	}
	if len(body) <= rec.Body.Len() {
		t.Errorf("compressed %d bytes is not below the original %d bytes", rec.Body.Len(), len(body))
	}
}

// The defensive headers other than CSP are on every response too. Each is one line, yet
// the screens look fine without them, so a missing one goes unnoticed.
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
	// autoplay is not blocked (it would break media playback).
	policy := rec.Header().Get("Permissions-Policy")
	if policy == "" {
		t.Error("no Permissions-Policy")
	}
	if strings.Contains(policy, "autoplay") {
		t.Errorf("Permissions-Policy blocks autoplay: %s", policy)
	}
}

// mustOIDC builds a configured verifier for tests (New returns an error when settings are missing).
func mustOIDC(t *testing.T, audience string, allowed []string) *oidc.Verifier {
	t.Helper()
	v, err := oidc.New(audience, allowed)
	if err != nil {
		t.Fatalf("oidc.New() error = %v", err)
	}
	return v
}
