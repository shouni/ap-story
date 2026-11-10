// Package server configures HTTP routing and middleware.
package server

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/shouni/gcp-kit/auth"
	"github.com/shouni/gcp-kit/cloudlog"
	"github.com/shouni/gcp-kit/cloudrun"
	"github.com/shouni/go-serve-kit/secureheaders"
	"github.com/shouni/go-serve-kit/staticfiles"

	"github.com/shouni/ap-story/assets"
	"github.com/shouni/ap-story/internal/builder"
	"github.com/shouni/ap-story/internal/domain"
)

// NewRouter builds the http.Handler that combines middleware and routing.
// projectID is used only for Cloud Logging trace correlation; when empty there is no correlation.
func NewRouter(h *builder.AppHandlers, projectID string) http.Handler {
	r := chi.NewRouter()
	setupCommonMiddleware(r, projectID)
	setupRoutes(r, h)

	return r
}

// setupCommonMiddleware configures the standard middleware.
func setupCommonMiddleware(r *chi.Mux, projectID string) {
	// Trace correlation must take effect before log output, so it is registered first.
	r.Use(cloudlog.TraceMiddleware(projectID))
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.CleanPath)
	// The screens are Japanese UTF-8 (3 bytes per character), so compression works well. Static files
	// go through the same path too (vendor is immutable, so recompression is rare).
	r.Use(middleware.Compress(compressionLevel))
	r.Use(secureheaders.Middleware(secureheaders.Config{
		ImageSources: []string{gcsOrigin},
		// Because Bootstrap's JS applies inline styles during transitions.
		AllowInlineStyle: true,
	}))
}

// compressionLevel is the gzip compression level.
const compressionLevel = 5

// gcsOrigin is the origin of GCS, where the images actually live. The screens point at same-origin
// endpoints, which 302 to a signed URL, so the destination is named explicitly.
const gcsOrigin = "https://storage.googleapis.com"

// setupRoutes registers each component's handlers with the router.
func setupRoutes(r chi.Router, h *builder.AppHandlers) {
	// --- 1. Public routes (health check) ---
	// "/healthz" is treated as a reserved path on Cloud Run's default domain (*.run.app):
	// requests never reach the container and are replaced by GFE's generic 404, so it is not used.
	// For why the path was chosen (not "/healthz"), see cloudrun.HealthPath.
	r.Get(cloudrun.HealthPath, cloudrun.Health)
	setupStaticRoutes(r)

	if h == nil {
		slog.Warn("AppHandlers is nil, skipping application routes registration")
		return
	}

	// --- 2. Authentication endpoints (OAuth2 flow) ---
	if h.Auth != nil {
		r.Handle("/auth/*", h.Auth.Routes()) // login / callback / logout
	}

	// --- 3. Routes requiring authentication (the API itself + web UI screens, added step by step starting from Home) ---
	r.Group(func(r chi.Router) {
		if h.Auth == nil {
			if h.Web != nil {
				slog.Error("Auth handler is nil, skipping protected API routes")
			}
			return
		}

		// Browser session auth (cookie + CSRF), or an OIDC Bearer token for M2M callers.
		// With the human-facing method last, a request where none succeeds is sent to the login page
		// (a caller asking for JSON gets 401).
		r.Use(auth.Protected(h.M2M, h.Auth))

		if h.Web != nil {
			// Unauthenticated access is redirected to /auth/login by session.Handler.
			r.Get("/", h.Web.Home)
			// Input forms are screens with no JSON counterpart, so they sit apart from the resources.
			r.Get("/compose", h.Web.ComposeForm)
			r.Get("/compose/design-sheet", h.Web.ComposeDesignSheetForm)
			r.Get("/comic-options", h.Web.ComicOptions)

			// The job is the primary resource. A comic (compose_comic) and a design sheet
			// (generate_design_sheet) are the same record with a different command, so from enqueue to
			// delete both are addressed by the same /jobs/{jobID} (the URL naming convention in public-docs).
			// People and machines use the same route, told apart by Accept and the shape of the body.
			r.Route("/jobs", func(r chi.Router) {
				r.Post("/", h.Web.JobCreate)
				r.Get("/", h.Web.JobList)
				r.Get("/{jobID}", h.Web.Job)
				r.Delete("/{jobID}", h.Web.JobDelete)
				r.Get("/{jobID}/script", h.Web.JobScript)
				r.Put("/{jobID}/script", h.Web.JobScriptUpdate)
				r.Post("/{jobID}/regenerate", h.Web.JobRegenerate)
				r.Get("/{jobID}/images/*", h.Web.JobImage)
			})

			// Characters are a separate resource, reused across works. Design-sheet artifacts are
			// listed here. images / reference are fixed words, so chi matches them before
			// {characterID}.
			r.Route("/characters", func(r chi.Router) {
				r.Get("/", h.Web.Characters)
				r.Get("/images/*", h.Web.CharacterImage)
				r.Get("/reference/*", h.Web.CharacterReferenceImage)
				r.Get("/{characterID}", h.Web.Character)
				r.Get("/{characterID}/design-sheets", h.Web.CharacterDesignSheets)
				r.Delete("/{characterID}/design-sheets/{jobID}", h.Web.DeleteCharacterDesign)
			})
		}
	})

	// --- 4. Cloud Tasks-only routes (for the worker) ---
	// In a SERVER_ROLE=web process TaskAuth and Worker are both nil, so this whole group
	// is not registered and /tasks/generate is not exposed.
	// A shape where only one of them is nil is rejected at startup by builder.AppHandlers.Validate,
	// so checking only for TaskAuth is enough here.
	r.Group(func(r chi.Router) {
		if h.TaskAuth == nil {
			return
		}

		// Verify the OIDC token from Cloud Tasks (no session needed).
		// A failure stops right there, without falling back to the session.
		r.Use(auth.Require(h.TaskAuth))
		r.Post(domain.WorkerTaskPath, h.Worker.ProcessTask)
	})
}

// setupStaticRoutes serves the embedded static files at /static/*.
// The Cache-Control decisions (short-lived for our own, immutable for vendor) and suppressing directory
// listings belong to go-serve-kit's staticfiles.
//
// It sits outside authentication. There is no reason to require login for a stylesheet, and
// the login page, shown unauthenticated, refers to them too.
func setupStaticRoutes(r chi.Router) {
	files, err := staticfiles.New(staticfiles.Config{FS: assets.StaticFiles, Dir: "static"})
	if err != nil {
		// It is a mix-up of embedded constants, so stop before accepting any request.
		panic(fmt.Sprintf("static assets: %v", err))
	}
	r.Handle("/static/*", files)
}
