// Package server は、HTTPルーティングとミドルウェアを構成します。
package server

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/shouni/gcp-kit/cloudlog"

	"github.com/shouni/ap-story/assets"
	"github.com/shouni/ap-story/internal/builder"
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
	// 画面は日本語 UTF-8（1 文字 3 バイト）なので圧縮がよく効くが、これまで無圧縮で
	// 配信していた。静的ファイルも同じ経路に乗る（vendor は immutable なので再圧縮は稀）。
	r.Use(middleware.Compress(compressionLevel))
	r.Use(securityHeaders)
}

// compressionLevel は gzip の圧縮レベルです。
const compressionLevel = 5

// contentSecurityPolicy は全レスポンスに付ける CSP です。
//
// 外部オリジンを 1 つも許可しないのは、Bootstrap を CDN から自前配信へ移したためです
// （assets/static/vendor）。CDN を allowlist に載せる形だと、jsDelivr は npm の全パッケージを
// 配信しているため「任意の npm パッケージの読み込みを許可する」に等しく、既知の
// CSP バイパス・ガジェットを持ち込まれます。'self' だけにできるのが自前配信の主目的です。
//
// インラインの <script> と on* 属性はテンプレートに 1 つも無く、
// TestRenderedPagesHaveNoInlineScripts が全画面で固定しているので、
// script-src は 'self' だけで足ります。
//
// style-src にだけ 'unsafe-inline' が要ります。Bootstrap の JS（collapse / tab）が
// 遷移中にインラインスタイルを当てるためです。
//
// img-src の storage.googleapis.com は漫画とキャラクターの画像です。画面が指すのは
// 同一オリジンのパスですが、いずれも GCS の署名付き URL へ 302 します。リダイレクト先を
// CSP がどう扱うかはブラウザ実装に幅があるため、送り先を明示して依存しないようにしています。
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: https://storage.googleapis.com; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"frame-ancestors 'none'; " +
	"form-action 'self'"

// securityHeaders は、全レスポンスに付ける防御的なヘッダー群です。
//
// hstsMaxAge は 1 年です。Cloud Run は HTTPS でしか受けないので現状の実害はありませんが、
// 独自ドメインを当てたときに平文へ降格させないための宣言です。preload は付けません
// （撤回にブラウザベンダーへの申請が要るうえ、得るものが少ないため）。
//
// Referrer-Policy を same-origin まで絞れるのは、外部オリジンへの参照を 1 つも持たないため
// です（Bootstrap を CDN から自前配信へ移した結果）。唯一の越境は署名付き URL への 302 で、
// GCS は Referer を見ません。
//
// Permissions-Policy は使っていない機能だけを塞ぎます。autoplay はメディア再生に関わるため
// 入れません。
const hstsMaxAge = "max-age=31536000; includeSubDomains"

var securityHeaderValues = map[string]string{
	"Content-Security-Policy":   contentSecurityPolicy,
	"Strict-Transport-Security": hstsMaxAge,
	// MIME スニッフィングを止めます。
	"X-Content-Type-Options": "nosniff",
	"Referrer-Policy":        "same-origin",
	"Permissions-Policy":     "geolocation=(), camera=(), microphone=(), payment=(), usb=()",
}

// securityHeaders は、全レスポンスに securityHeaderValues を付けます。
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		for name, value := range securityHeaderValues {
			header.Set(name, value)
		}
		next.ServeHTTP(w, r)
	})
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
		r.Use(h.Auth.ProtectedMiddleware(h.M2M))

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
				r.Get("/{jobID}/script", h.Web.GetComicScript)
				r.Put("/{jobID}/script", h.Web.UpdateComicScript)
				r.Post("/{jobID}/regenerate", h.Web.RegenerateComic)
				r.Get("/{jobID}/images/*", h.Web.RedirectComicImage)
				r.Get("/{jobID}/status", h.Web.JobStatus)
			})
			r.Get("/api/comic-options", h.Web.ComicOptions)
			r.Get("/api/characters", h.Web.ListCharacters)
			r.Get("/api/characters/{characterID}", h.Web.GetCharacterDetail)
			r.Get("/api/characters/images/*", h.Web.RedirectCharacterImage)
			r.Get("/api/characters/reference/*", h.Web.RedirectCharacterReferenceImage)
			r.Delete("/api/characters/{characterID}/images/{jobID}", h.Web.DeleteCharacterDesign)
		}
	})

	// --- 4. Cloud Tasks 専用ルート (Worker 用) ---
	// SERVER_ROLE=web のプロセスでは TaskAuth も Worker も nil になるため、
	// このグループごと登録されず /tasks/generate は公開されません。
	// 片方だけが nil になる形は builder.AppHandlers.Validate が起動時に弾くので、
	// ここでは TaskAuth の有無だけを見れば足ります。
	r.Group(func(r chi.Router) {
		if h.TaskAuth == nil {
			return
		}

		// Cloud Tasks からの OIDC トークンを検証 (セッション不要)
		r.Use(h.TaskAuth.Middleware)
		r.Post("/tasks/generate", h.Worker.ProcessTask)
	})
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
		w.Header().Set("Cache-Control", cacheControlFor(r.URL.Path))
		fileServer.ServeHTTP(w, r)
	}))
}

// vendorPathPrefix より下は第三者製の配布物で、パスにバージョンが入っています
// （assets/static/vendor/bootstrap-5.3.8 など）。更新すれば必ず別の URL になるので、
// 再検証させる理由がありません。
const vendorPathPrefix = "/static/vendor/"

const (
	// ownAssetCacheControl は自前の CSS / JS 用です。URL を変えずに中身が変わるため短命にします。
	ownAssetCacheControl = "public, max-age=300, must-revalidate"
	// vendorCacheControl は vendorPathPrefix 配下用です。
	vendorCacheControl = "public, max-age=31536000, immutable"
)

// cacheControlFor は、静的ファイルのパスに応じた Cache-Control を返します。
//
// //go:embed した FileServer は Last-Modified も ETag も出せない（embed の ModTime が
// ゼロ値のため net/http が両方を省く）ので、期限が切れた時点で必ず全体を取り直します。
// バージョン付きの vendor を分けているのは、その再取得を無くすためです。
func cacheControlFor(path string) string {
	if strings.HasPrefix(path, vendorPathPrefix) {
		return vendorCacheControl
	}
	return ownAssetCacheControl
}
