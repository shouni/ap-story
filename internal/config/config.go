// Package config loads and validates the application configuration from environment variables.
package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-serve-kit/serverrole"
	"github.com/shouni/go-utils/strlist"
)

// This package holds no art-style wording: panels, pages and design sheets alike now
// pick a preset from assets/prompts/styles.json by style mode
// (sheets have a separate design_style with the staging removed).
//
// Model IDs have no defaults either. They are replaced on Google's schedule, so
// GEMINI_MODELS / IMAGE_MODELS must be set, and leaving them unset fails startup.

// ServerConfig is the HTTP server configuration.
type ServerConfig struct {
	ServiceURL string `env:"SERVICE_URL" envDefault:"http://localhost:8080"`
	Port       string `env:"PORT" envDefault:"8080"`
	// Role is the role this process plays. It must be set explicitly; unset is a startup error.
	// The graceful-shutdown grace period is left to the kit's default (a value within Cloud Run's SIGKILL grace).
	Role serverrole.Role `env:"SERVER_ROLE"`
}

// GCPConfig is the Google Cloud Platform configuration.
type GCPConfig struct {
	ProjectID  string `env:"GCP_PROJECT_ID"`
	LocationID string `env:"GCP_LOCATION_ID"`
}

// TasksConfig configures enqueueing onto the Cloud Tasks queue and OIDC verification on receipt.
// These settings are specific to Cloud Tasks, neither general GCP nor HTTP server settings,
// so they are gathered here, as in the sister projects.
type TasksConfig struct {
	QueueID         string `env:"CLOUD_TASKS_QUEUE_ID"`
	WorkerURL       string `env:"WORKER_URL"`
	TaskAudienceURL string `env:"TASK_AUDIENCE_URL"`
	// CallerServiceAccountEmail is the caller SA put in an outgoing task's
	// oidcToken.serviceAccountEmail. Cloud Tasks mints and attaches the token; this
	// process signs nothing. A setting for the enqueuing side, i.e. the web face, only.
	CallerServiceAccountEmail string `env:"TASK_CALLER_SERVICE_ACCOUNT_EMAIL"`
	// AllowedServiceAccounts is the allowlist (comma-separated) of caller SAs the worker accepts.
	// Empty makes the verifier fail closed, so it is required on the worker.
	AllowedServiceAccounts []string `env:"ALLOWED_TASK_SERVICE_ACCOUNTS"`
	// DispatchDeadline is the response deadline stamped onto an outgoing task.
	//
	// It is not "how long to wait" but **the effective cap on the worker's run time**. Past it,
	// Cloud Tasks stops waiting even while the worker is still working, and with the queue at
	// max_attempts = 1 no retry follows. Raising Cloud Run's timeout does not move this cap.
	// It is an env var rather than a constant so that the infrastructure (Terraform) can be its
	// single source. As a constant the infrastructure would keep a copy and nobody would notice drift.
	//
	// **It has no default.** The three-tier timeouts depend on the deployment target, so their
	// source is kept to one place in Terraform. A default in the app would put the same number in
	// two places, and a missing setting would run on "a value nobody chose".
	DispatchDeadline time.Duration `env:"TASK_DISPATCH_DEADLINE"`
}

// StorageConfig is the storage configuration.
type StorageConfig struct {
	GCSBucket          string `env:"STORY_BUCKET"`
	CharactersJSONPath string `env:"CHARACTERS_JSON_PATH"`
}

// NotificationConfig is the notification configuration.
type NotificationConfig struct {
	SlackWebhookURL string `env:"SLACK_WEBHOOK_URL"`
}

// AIConfig configures the AI models and execution control. It maps onto go-comic-kit's ports.Config.
type AIConfig struct {
	// The model lists are comma-separated, and the head is the default model. There is no
	// default value; empty fails startup in ValidateEssentialConfig (web face only).
	//
	// ImageModels is used for design sheets, panels and pages alike, and GeminiModels for
	// the script (chapters and chapter scripts). The head of each is the default, and the list
	// itself is the form's options and the allowlist at enqueue. There is no mechanism for
	// splitting models by purpose.
	GeminiModels []string `env:"GEMINI_MODELS"`
	ImageModels  []string `env:"IMAGE_MODELS"`

	// Unset, the aspect ratio and resolution fall back to the kit's defaults (3:4 / panels 1K /
	// pages and sheets 2K). These match the old fixed values, so leaving them unset changes nothing.
	// Resolution affects the cost of every panel, so the deployment can choose it.
	AspectRatio    string `env:"IMAGE_ASPECT_RATIO"`
	PanelImageSize string `env:"PANEL_IMAGE_SIZE"`
	PageImageSize  string `env:"PAGE_IMAGE_SIZE"`

	MaxConcurrency      int `env:"MAX_CONCURRENCY"`
	MaxChapters         int `env:"MAX_CHAPTERS"`
	MaxPanelsPerChapter int `env:"MAX_PANELS_PER_CHAPTER"`
	MaxPanelsPerPage    int `env:"MAX_PANELS_PER_PAGE"`

	// RateInterval is the minimum interval between AI calls (0 means unlimited).
	// Throughput is capped by 1/RATE_INTERVAL rather than by MAX_CONCURRENCY, so to get
	// parallel generation set this to 0 or a small enough value.
	RateInterval time.Duration `env:"RATE_INTERVAL" envDefault:"10s"`

	// RequestTimeout caps a single external AI call.
	// One image takes tens of seconds, so too short a value cuts generation itself off
	// (5 minutes, matching go-comic-kit's default).
	RequestTimeout time.Duration `env:"REQUEST_TIMEOUT" envDefault:"5m"`

	// PipelineTimeout caps the run time of one worker task.
	// Whereas REQUEST_TIMEOUT caps a single external API call, this caps the whole
	// step sequence (script → panels → pages).
	//
	// Keep it shorter than TaskDispatchDeadline. Equal is not enough: unless the app gives up
	// first, Cloud Tasks cuts it off with neither a failure record nor a Slack notice
	// (on the worker, validatePipelineTimeout refuses it at startup).
	PipelineTimeout time.Duration `env:"PIPELINE_TIMEOUT"`
}

// applyDefaults normalises the model lists. The art style is held by the presets
// (assets/prompts/styles.json), so there is nothing to fill in here. Concurrency, timeouts and
// limits are left to the kit's ApplyDefaults (they are values that keep the kit working, so the kit owns their defaults).
func (a *AIConfig) applyDefaults() {
	a.GeminiModels = strlist.Normalize(a.GeminiModels)
	a.ImageModels = strlist.Normalize(a.ImageModels)
}

// KitConfig converts to go-comic-kit's ports.Config.
func (a AIConfig) KitConfig() ports.Config {
	return ports.Config{
		MaxConcurrency:      a.MaxConcurrency,
		RateInterval:        a.RateInterval,
		MaxChapters:         a.MaxChapters,
		MaxPanelsPerChapter: a.MaxPanelsPerChapter,
		MaxPanelsPerPage:    a.MaxPanelsPerPage,
		RequestTimeout:      a.RequestTimeout,
	}
}

// AuthConfig is the authentication and authorisation configuration.
// It covers both Google OAuth for the browser web UI and service-account OIDC
// verification for server-to-server (M2M) calls (the same two-track setup as the sister projects).
type AuthConfig struct {
	GoogleClientID     string `env:"GOOGLE_CLIENT_ID"`
	GoogleClientSecret string `env:"GOOGLE_CLIENT_SECRET"`
	// SessionDatabase is the Firestore database that holds sessions.
	//
	// It is separate from the one for job status. A database name is an identifier that cannot be
	// changed later, so sharing one would leave its name wrong for one of the two uses.
	SessionDatabase string `env:"SESSION_FIRESTORE_DATABASE" envDefault:"sessions"`
	// SessionCollection is the collection that holds sessions.
	SessionCollection string   `env:"SESSION_FIRESTORE_COLLECTION" envDefault:"sessions"`
	AllowedEmails     []string `env:"ALLOWED_EMAILS"`
	AllowedDomains    []string `env:"ALLOWED_DOMAINS"`
	// AllowedM2MServiceAccounts lists the service account emails (comma-separated) allowed
	// to call the API server-to-server (OIDC Bearer token).
	AllowedM2MServiceAccounts []string `env:"ALLOWED_M2M_SERVICE_ACCOUNTS"`
}

// Config is the application configuration.
type Config struct {
	Server       ServerConfig
	GCP          GCPConfig
	Tasks        TasksConfig
	Storage      StorageConfig
	Notification NotificationConfig
	AI           AIConfig
	Auth         AuthConfig
}

// LoadConfig loads the configuration from environment variables.
func LoadConfig() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, err
	}

	if err := cfg.normalize(); err != nil {
		return nil, fmt.Errorf("failed to normalize config: %w", err)
	}
	if cfg.Tasks.TaskAudienceURL == "" {
		cfg.Tasks.TaskAudienceURL = cfg.Server.ServiceURL
	}

	return &cfg, nil
}

func (c *Config) normalize() error {
	// Environment variable names are the app's concern, so the context is added to the kit's error here.
	role, err := serverrole.Parse(string(c.Server.Role))
	if err != nil {
		return fmt.Errorf("SERVER_ROLE: %w", err)
	}
	c.Server.Role = role

	c.Server.ServiceURL = strings.TrimSpace(c.Server.ServiceURL)
	workerURL, err := normalizeWorkerURL(c.Server.Role, c.Tasks.WorkerURL, c.Server.ServiceURL)
	if err != nil {
		return err
	}
	c.Tasks.WorkerURL = workerURL
	c.Tasks.TaskAudienceURL = strings.TrimSpace(c.Tasks.TaskAudienceURL)

	// A bucket *name*, not a URI. Passing through a `gs://...` pasted from the console
	// makes artifact URIs `gs://gs://...`.
	c.Storage.GCSBucket = remoteio.NormalizeBucketName(c.Storage.GCSBucket)

	// env only splits on commas, so surrounding whitespace and duplicates are dropped here.
	c.Auth.AllowedEmails = strlist.Normalize(c.Auth.AllowedEmails)
	c.Auth.AllowedDomains = strlist.Normalize(c.Auth.AllowedDomains)
	c.Auth.AllowedM2MServiceAccounts = strlist.Normalize(c.Auth.AllowedM2MServiceAccounts)
	c.Tasks.AllowedServiceAccounts = strlist.Normalize(c.Tasks.AllowedServiceAccounts)

	c.AI.applyDefaults()
	return nil
}

// normalizeWorkerURL tidies the worker service URL tasks are delivered to. It returns only
// the service's URL; the task path is appended just before enqueueing (internal/builder).
//
// Only a process that serves the worker face derives it from SERVICE_URL. On a split
// deployment SERVICE_URL is pinned to the public side, so deriving it on web-only would 404 every task.
func normalizeWorkerURL(role serverrole.Role, workerURL string, serviceURL string) (string, error) {
	workerURL = strings.TrimSpace(workerURL)
	if workerURL != "" {
		if _, err := url.Parse(workerURL); err != nil {
			return "", fmt.Errorf("invalid worker URL %q: %w", workerURL, err)
		}
		return workerURL, nil
	}
	if !role.ServesWorker() {
		return "", nil
	}

	serviceURL = strings.TrimSpace(serviceURL)
	if serviceURL == "" {
		return "", nil
	}
	if _, err := url.Parse(serviceURL); err != nil {
		return "", fmt.Errorf("invalid service URL %q: %w", serviceURL, err)
	}
	return serviceURL, nil
}

// TaskCallerServiceAccount returns the caller SA put on outgoing tasks.
// The value is read from env as is, so only surrounding whitespace is dropped.
func (c *Config) TaskCallerServiceAccount() string {
	return strings.TrimSpace(c.Tasks.CallerServiceAccountEmail)
}
