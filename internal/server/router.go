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
	"github.com/shouni/gcp-kit/cloudrun"
	"github.com/shouni/gcp-kit/secureheaders"

	"github.com/shouni/ap-story/assets"
	"github.com/shouni/ap-story/internal/builder"
	"github.com/shouni/gcp-kit/auth"
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
	r.Use(secureheaders.New(secureheaders.Config{
		ImageSources: []string{gcsOrigin},
	}))
}

// compressionLevel は gzip の圧縮レベルです。
const compressionLevel = 5

// gcsOrigin は、画像の実体である GCS のオリジンです。画面は同一オリジンの
// エンドポイントを指しますが、そこから署名付き URL へ 302 するため、送り先を明示します。
const gcsOrigin = "https://storage.googleapis.com"

// setupRoutes は、各コンポーネントのハンドラーをルーティングに登録します。
func setupRoutes(r chi.Router, h *builder.AppHandlers) {
	// --- 1. 公開ルート (ヘルスチェック) ---
	// "/healthz" は Cloud Run のデフォルトドメイン (*.run.app) 側で予約パス的に扱われ、
	// コンテナまでリクエストが届かず GFE の汎用 404 に置き換えられるため使わない。
	// パスの選択理由（"/healthz" を使わない）は cloudrun.HealthPath を参照。
	r.Get(cloudrun.HealthPath, cloudrun.Health)
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

		// ブラウザセッション認証(Cookie+CSRF)、またはM2M呼び出し元はOIDC Bearerトークンで認証。
		// 人向けの方式を最後に置くと、どれも成立しなかったときログイン画面へ送られます
		// （JSON を求めている相手には 401 が返ります）。
		r.Use(auth.Protected(h.M2M, h.Auth))

		if h.Web != nil {
			// 未認証アクセスは session.Handler が /auth/login へリダイレクトする。
			r.Get("/", h.Web.Home)
			r.Get("/compose", h.Web.ComposeForm)
			r.Post("/compose", h.Web.EnqueueComicForm)
			r.Get("/design-sheets", h.Web.DesignSheetForm)
			r.Post("/design-sheets", h.Web.EnqueueDesignSheetForm)

			// 人と機械が同じものを見る経路です。ルートは 1 本で、表現は Accept が
			// 決めます（handlers/negotiated.go）。
			r.Route("/characters", func(r chi.Router) {
				r.Get("/", h.Web.Characters)
				r.Get("/{characterID}", h.Web.Character)
				r.Get("/{characterID}/history", h.Web.ServeCharacterHistory)
			})
			r.Route("/history", func(r chi.Router) {
				r.Get("/", h.Web.Comics)
				r.Get("/{jobID}", h.Web.Comic)
			})

			// 機械にしか無い操作です。対応する画面がありません。
			r.Post("/api/design-sheets", h.Web.EnqueueDesignSheet)
			r.Route("/api/comics", func(r chi.Router) {
				r.Post("/", h.Web.EnqueueComic)
				r.Delete("/{jobID}", h.Web.DeleteComic)
				r.Get("/{jobID}/script", h.Web.GetComicScript)
				r.Put("/{jobID}/script", h.Web.UpdateComicScript)
				r.Post("/{jobID}/regenerate", h.Web.RegenerateComic)
				r.Get("/{jobID}/images/*", h.Web.RedirectComicImage)
				r.Get("/{jobID}/status", h.Web.JobStatus)
			})
			r.Get("/api/comic-options", h.Web.ComicOptions)
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

		// Cloud Tasks からの OIDC トークンを検証 (セッション不要)。
		// 失敗はセッションへフォールバックせず、その場で止まります。
		r.Use(auth.Require(h.TaskAuth))
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
