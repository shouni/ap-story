package builder

import (
	"errors"
	"fmt"

	"github.com/shouni/gcp-kit/auth/oidc"
	"github.com/shouni/gcp-kit/auth/session"
	"github.com/shouni/gcp-kit/worker"

	"github.com/shouni/ap-story/internal/adapters/prompts"
	"github.com/shouni/ap-story/internal/app"
	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
	"github.com/shouni/ap-story/internal/server/handlers"
)

const defaultSessionName = "ap-story-session"

// AppHandlers holds every HTTP handler that was built.
// The server package takes this struct and does the routing.
type AppHandlers struct {
	Auth   *session.Handler
	Web    *handlers.Handler
	Worker *worker.Handler[domain.Task]
	M2M    *oidc.Verifier
	// TaskAuth verifies OIDC from Cloud Tasks. Unlike Auth it needs no OAuth settings,
	// so it can be built in a worker process that has no web face.
	TaskAuth *oidc.Verifier
}

// Validate checks that the assembled result is a coherent shape for the role.
//
// TaskAuth and Worker are a pair ("verify Cloud Tasks" and "what comes after it"), and
// only one of them being nil is a DI mismatch. router.go skips route registration on nil,
// so left alone /tasks/generate would just silently 404, and a request could not tell whether
// the cause is configuration or implementation. Startup fails before the router can answer 404.
func (h *AppHandlers) Validate() error {
	if (h.TaskAuth == nil) != (h.Worker == nil) {
		return errors.New("TaskAuth and Worker must be configured together")
	}
	return nil
}

// BuildHandlers assembles each handler's dependencies according to SERVER_ROLE and
// returns the AppHandlers struct. Handlers for a face the role does not serve stay nil,
// so the router skips registering their routes altogether.
func BuildHandlers(appCtx *app.Container) (*AppHandlers, error) {
	if appCtx.Config.Server.ServiceURL == "" {
		return nil, fmt.Errorf("ServiceURL must be set for the auth redirect")
	}

	h := &AppHandlers{}
	role := appCtx.Config.Server.Role

	if role.ServesWeb() {
		if err := buildWebHandlers(appCtx, h); err != nil {
			return nil, err
		}
	}

	if role.ServesWorker() {
		// Verification always fails unless both the audience and the allowed caller SAs are set
		// (fail-closed), so the configuration is checked at startup.
		taskAuth, err := oidc.New(appCtx.Config.Tasks.TaskAudienceURL, appCtx.Config.Tasks.AllowedServiceAccounts)
		if err != nil {
			return nil, fmt.Errorf("cannot configure Cloud Tasks OIDC verification: TASK_AUDIENCE_URL and ALLOWED_TASK_SERVICE_ACCOUNTS are required: %w", err)
		}
		h.TaskAuth = taskAuth
		h.Worker = worker.NewHandler[domain.Task](appCtx.Pipeline)
	}

	if err := h.Validate(); err != nil {
		return nil, err
	}

	return h, nil
}

// buildWebHandlers builds the handlers for the web face (OAuth, Web/API, M2M verification).
func buildWebHandlers(appCtx *app.Container, h *AppHandlers) error {
	// 1. Initialise the auth handler (Google OAuth + Cloud Tasks OIDC verification).
	authHandler, err := createAuthHandler(appCtx.Config, appCtx.SessionStore)
	if err != nil {
		return fmt.Errorf("failed to initialise the auth handler: %w", err)
	}

	// 2. Initialise the Web/API handler.
	// The script-mode and style-mode options come from the same templates (assets/prompts)
	// used at generation time. If the web face kept its own list, a mode shown on screen
	// could be missing on the worker.
	scriptPrompts, err := prompts.NewScriptPrompts()
	if err != nil {
		return fmt.Errorf("failed to load the script prompts: %w", err)
	}
	styles, err := prompts.NewStyles()
	if err != nil {
		return fmt.Errorf("failed to load the art-style presets: %w", err)
	}

	webHandler, err := handlers.NewHandler(handlers.HandlerOptions{
		TaskQueue:    appCtx.TaskQueue,
		Repository:   appCtx.Repository,
		JobStatus:    appCtx.JobStatus,
		Signer:       appCtx.Store,
		Bucket:       appCtx.Config.Storage.GCSBucket,
		Characters:   appCtx.Characters,
		ImageModels:  appCtx.Config.AI.ImageModels,
		GeminiModels: appCtx.Config.AI.GeminiModels,
		ScriptModes:  scriptPrompts.ModeInfos(),
		StyleModes:   styles.ModeInfos(),
	})
	if err != nil {
		return fmt.Errorf("failed to initialise the web handler: %w", err)
	}

	h.Auth = authHandler
	h.Web = webHandler
	m2m, err := newM2MVerifier(appCtx.Config.Server.ServiceURL, appCtx.Config.Auth.AllowedM2MServiceAccounts)
	if err != nil {
		return err
	}
	h.M2M = m2m

	return nil
}

// createAuthHandler initialises and returns the auth handler.
func createAuthHandler(cfg *config.Config, store session.Store) (*session.Handler, error) {
	return session.New(session.Config{
		ClientID:       cfg.Auth.GoogleClientID,
		ClientSecret:   cfg.Auth.GoogleClientSecret,
		ServiceURL:     cfg.Server.ServiceURL,
		SessionName:    defaultSessionName,
		Store:          store,
		AllowedEmails:  cfg.Auth.AllowedEmails,
		AllowedDomains: cfg.Auth.AllowedDomains,
	})
}

// newM2MVerifier configures the OIDC verifier for M2M (server-to-server) calls.
//
// ProtectedMiddleware cannot disable M2M. Even with the allowlist or the audience missing
// the path stays alive, verification always fails, and the request falls back to session
// auth. A missing setting would therefore only show up as "the browser works fine but the
// M2M client receives the login page's HTML". With no way to tell a deliberate disable from
// a missing setting, empty can only mean the latter, so startup refuses it as TaskVerifier does.
//
// Whether it can be configured is asked of the verifier rather than of config, because
// gcp-kit is what knows which settings are required. Checking only for an empty allowlist in
// config would miss a missing audience (SERVICE_URL) and would not follow if the kit adds requirements.
func newM2MVerifier(serviceURL string, allowedServiceAccounts []string) (*oidc.Verifier, error) {
	m2m, err := oidc.New(serviceURL, allowedServiceAccounts)
	if err != nil {
		return nil, fmt.Errorf("cannot configure M2M OIDC verification (SERVICE_URL and ALLOWED_M2M_SERVICE_ACCOUNTS): %w", err)
	}
	return m2m, nil
}
