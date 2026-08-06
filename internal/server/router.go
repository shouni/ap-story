// Package server は、HTTPルーティングとミドルウェアを構成します。
package server

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/shouni/gcp-kit/auth"
	"github.com/shouni/gcp-kit/cloudlog"

	"github.com/shouni/ap-story/assets"
	"github.com/shouni/ap-story/internal/builder"
	"github.com/shouni/ap-story/internal/server/handlers"
)

// NewRouter は、ミドルウェアとルーティングを統合した http.Handler を構築します。
// projectID は Cloud Logging のトレース相関にのみ使用し、空なら相関を行いません。
func NewRouter(h *builder.AppHandlers, projectID string) http.Handler {
	r := chi.NewRouter()
	setupCommonMiddleware(r, projectID)
	setupRoutes(r, h)

	return r
}

// setupCommonMiddleware は、標準的なミドルウェアを構成します。
func setupCommonMiddleware(r *chi.Mux, projectID string) {
	// トレース相関はログ出力より先に効かせる必要があるため最初に登録する。
	r.Use(cloudlog.TraceMiddleware(projectID))
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.CleanPath)
}

// setupRoutes は、各コンポーネントのハンドラーをルーティングに登録します。
func setupRoutes(r chi.Router, h *builder.AppHandlers) {
	// --- 1. 公開ルート (ヘルスチェック) ---
	// "/healthz" は Cloud Run のデフォルトドメイン (*.run.app) 側で予約パス的に扱われ、
	// コンテナまでリクエストが届かず GFE の汎用 404 に置き換えられるため使わない。
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	setupStaticRoutes(r)

	if h == nil {
		slog.Warn("AppHandlers is nil, skipping application routes registration")
		return
	}

	// --- 2. 認証関連エンドポイント (OAuth2 フロー) ---
	if h.Auth != nil {
		r.Route("/auth", func(r chi.Router) {
			r.Get("/login", h.Auth.Login)
			r.Get("/callback", h.Auth.Callback)
		})
	}

	// --- 3. 認証が必要なルート (API 本体 + Web UI 画面。画面は Home から段階的に追加していく) ---
	r.Group(func(r chi.Router) {
		if h.Auth == nil {
			if h.Web != nil {
				slog.Error("Auth handler is nil, skipping protected API routes")
			}
			return
		}

		// ブラウザセッション認証(Cookie+CSRF)、またはM2M呼び出し元はOIDC Bearerトークンで認証
		r.Use(protectedAccessMiddleware(h.Auth, h.M2M))

		if h.Web != nil {
			// 未認証アクセスは auth.Handler.Middleware が /auth/login へリダイレクトする。
			r.Get("/", h.Web.Home)
			r.Get("/compose", h.Web.ComposeForm)
			r.Post("/compose", h.Web.EnqueueComicForm)
			r.Get("/design-sheets", h.Web.DesignSheetForm)
			r.Post("/design-sheets", h.Web.EnqueueDesignSheetForm)
			r.Post("/api/design-sheets", h.Web.EnqueueDesignSheet)
			r.Route("/characters", func(r chi.Router) {
				r.Get("/", h.Web.ServeCharacters)
				r.Get("/{characterID}", h.Web.ServeCharacterDetail)
				r.Get("/{characterID}/history", h.Web.ServeCharacterHistory)
			})
			r.Route("/history", func(r chi.Router) {
				r.Get("/", h.Web.ServeHistory)
				r.Get("/{jobID}", h.Web.ServeDetails)
			})

			r.Route("/api/comics", func(r chi.Router) {
				r.Post("/", h.Web.EnqueueComic)
				r.Get("/", h.Web.ListComics)
				r.Get("/{jobID}", h.Web.GetComic)
				r.Delete("/{jobID}", h.Web.DeleteComic)
				r.Post("/{jobID}/regenerate", h.Web.RegenerateComic)
				r.Get("/{jobID}/images/*", h.Web.RedirectComicImage)
				r.Get("/{jobID}/status", h.Web.JobStatus)
			})
			r.Get("/api/characters", h.Web.ListCharacters)
			r.Get("/api/characters/{characterID}", h.Web.GetCharacterDetail)
			r.Get("/api/characters/images/*", h.Web.RedirectCharacterImage)
			r.Get("/api/characters/reference/*", h.Web.RedirectCharacterReferenceImage)
			r.Delete("/api/characters/{characterID}/images/{jobID}", h.Web.DeleteCharacterDesign)
		}
	})

	// --- 4. Cloud Tasks 専用ルート (Worker 用) ---
	r.Group(func(r chi.Router) {
		if h.Auth == nil {
			if h.Worker != nil {
				slog.Error("Auth handler is nil, skipping worker routes")
			}
			return
		}

		// Cloud Tasks からの OIDC トークンを検証 (セッション不要)
		r.Use(h.Auth.TaskOIDCVerificationMiddleware)

		if h.Worker != nil {
			r.Post("/tasks/generate", h.Worker.ProcessTask)
		}
	})
}

// protectedAccessMiddleware は、有効なM2M(OIDC Bearer)トークンを持つリクエストは
// セッション認証・CSRF検証をバイパスし、それ以外はブラウザセッション認証+CSRF検証にフォールバックします。
// /web/* と /api/* の両方をこのミドルウェア一枚で保護します（ap-comp と同一方式）。
func protectedAccessMiddleware(authHandler *auth.Handler, m2m *auth.M2MVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		sessionChain := authHandler.Middleware(csrfAutoGenMiddleware(authHandler)(next))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			payload, err := m2m.Verify(r)
			if err == nil {
				slog.Debug("M2M認証成功", "email", payload.Claims["email"], "path", r.URL.Path)
				next.ServeHTTP(w, r)
				return
			}
			// ブラウザセッションなどM2Mを試みていないリクエストはノイズになるためログしない。
			if !errors.Is(err, auth.ErrM2MNotAttempted) {
				slog.Info("M2M認証失敗、セッション認証にフォールバック", "error", err, "path", r.URL.Path)
			}
			sessionChain.ServeHTTP(w, r)
		})
	}
}

// setupStaticRoutes は、埋め込み済みの静的ファイル（CSS/JS）を /static/* で配信します。
func setupStaticRoutes(r chi.Router) {
	staticFS, err := fs.Sub(assets.StaticFiles, "static")
	if err != nil {
		slog.Error("static assets are unavailable", "error", err)
		return
	}

	fileServer := http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))
	r.Handle("/static/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
		fileServer.ServeHTTP(w, r)
	}))
}

// csrfAutoGenMiddleware は、CSRFトークンがなければ自動生成してセッションに保存します。
func csrfAutoGenMiddleware(authHandler *auth.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			csrfToken := authHandler.GetCSRFTokenFromSession(r)
			if csrfToken == "" && r.Method == http.MethodGet {
				token, err := authHandler.GenerateAndSaveCSRFToken(w, r)
				if err != nil {
					slog.Error("Failed to auto-generate CSRF token", "error", err)
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}
				csrfToken = token
			}
			r = r.WithContext(handlers.WithCSRFToken(r.Context(), csrfToken))
			next.ServeHTTP(w, r)
		})
	}
}
