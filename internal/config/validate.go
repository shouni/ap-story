package config

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/shouni/netarmor/securenet"
)

// IsSecureServiceURL reports whether the configured ServiceURL uses a secure scheme (such as HTTPS).
func (c *Config) IsSecureServiceURL() bool {
	return securenet.IsSecureServiceURL(c.Server.ServiceURL)
}

// WarnOnContradictoryGenerationSettings warns when generation-control settings do not fit
// together. Not bad enough to stop startup, but these combinations silently behave worse than
// expected, and this makes the operator notice.
func (c *Config) WarnOnContradictoryGenerationSettings() {
	// Batch throughput is capped by 1/RATE_INTERVAL, so raising only the concurrency while
	// keeping the interval does not speed anything up. The person who set it is the least likely to notice.
	if c.AI.MaxConcurrency > 1 && c.AI.RateInterval > 0 {
		slog.Warn("raising MAX_CONCURRENCY has no parallel effect because RATE_INTERVAL limits the call rate",
			"max_concurrency", c.AI.MaxConcurrency,
			"rate_interval", c.AI.RateInterval,
			"effective_calls_per_minute", time.Minute/c.AI.RateInterval)
	}
	// One image takes tens of seconds, so an extremely short cap cuts generation itself off.
	if c.AI.RequestTimeout > 0 && c.AI.RequestTimeout < minSafeRequestTimeout {
		slog.Warn("REQUEST_TIMEOUT is short; image generation may be cut off before it completes",
			"request_timeout", c.AI.RequestTimeout,
			"recommended_minimum", minSafeRequestTimeout)
	}
}

// minSafeRequestTimeout is the rough minimum that one image generation fits in.
const minSafeRequestTimeout = 2 * time.Minute

// ValidateEssentialConfig validates the settings essential to running the application.
func (c *Config) ValidateEssentialConfig() error {
	if !c.IsSecureServiceURL() {
		return fmt.Errorf("SERVICE_URL ('%s') must be HTTPS in production", c.Server.ServiceURL)
	}

	if c.GCP.ProjectID == "" {
		return fmt.Errorf("GCP_PROJECT_ID is not set (required for Vertex AI)")
	}
	if c.GCP.LocationID == "" {
		return fmt.Errorf("GCP_LOCATION_ID is not set (default: asia-northeast1)")
	}
	if c.Storage.GCSBucket == "" {
		return fmt.Errorf("STORY_BUCKET is not set")
	}
	// CHARACTERS_JSON_PATH is optional. Unset, it falls back to go-character-kit's embedded
	// default character definitions (internal/adapters.LoadCharacters).

	if c.Server.Role.ServesWeb() {
		if err := c.validateWebConfig(); err != nil {
			return err
		}
	}

	if c.Server.Role.ServesWorker() {
		if c.Tasks.TaskAudienceURL == "" {
			return fmt.Errorf("TASK_AUDIENCE_URL is not set; it is required for Cloud Tasks OIDC verification")
		}
		// Empty makes the verifier fail closed and every task keeps failing with 500.
		if len(c.Tasks.AllowedServiceAccounts) == 0 {
			return fmt.Errorf("no allowed caller SA is set; set ALLOWED_TASK_SERVICE_ACCOUNTS")
		}
		if err := c.validatePipelineTimeout(); err != nil {
			return err
		}
	}

	return nil
}

// validatePipelineTimeout checks, for a role that runs the pipeline, that PIPELINE_TIMEOUT
// is strictly shorter than TASK_DISPATCH_DEADLINE.
//
// The point is that the app gives up first. In the reverse order (equality included) Cloud Tasks
// cuts the request first, neither the failure record nor the Slack notice runs, and with the queue at
// max_attempts = 1 there is no retry, so the job stays at running. Unlimited (0 or less) is refused for the same reason.
// The effective cap is the smallest of the three, so this one relation is enforced at startup.
func (c *Config) validatePipelineTimeout() error {
	if c.Tasks.DispatchDeadline <= 0 {
		return fmt.Errorf("TASK_DISPATCH_DEADLINE is not set (the three-tier timeouts are decided by the deployment, e.g. 30m)")
	}
	if c.AI.PipelineTimeout <= 0 {
		return fmt.Errorf("PIPELINE_TIMEOUT cannot be unlimited on the worker; set a value shorter than the Cloud Tasks cutoff (%s)", c.Tasks.DispatchDeadline)
	}
	if c.AI.PipelineTimeout >= c.Tasks.DispatchDeadline {
		return fmt.Errorf("PIPELINE_TIMEOUT (%s) must be shorter than the Cloud Tasks cutoff (%s); even when equal, the request is cut before the app can record the failure", c.AI.PipelineTimeout, c.Tasks.DispatchDeadline)
	}
	return nil
}

// validateWebConfig validates the settings the web face needs (OAuth login and sessions, enqueueing tasks).
// Requiring OAuth settings of a process that serves only the worker face would hand out
// access to credentials it never uses, so the checks are split by role.
func (c *Config) validateWebConfig() error {
	// The model lists are a web-face requirement only: the form's options and the allowlist at enqueue.
	// The worker does not read them, since every job now carries its own model names
	// (go-comic-kit holds no configured model default either).
	//
	// Empty fails startup because letting it through would remove the form's model picker and
	// make every model choice in the JSON API a 400. Startup itself would succeed, so as is
	// nobody would notice that nothing can be chosen until they tried.
	for _, models := range []struct {
		name string
		list []string
	}{
		{"IMAGE_MODELS", c.AI.ImageModels},
		{"GEMINI_MODELS", c.AI.GeminiModels},
	} {
		if len(models.list) == 0 {
			return fmt.Errorf("%s is not set (comma-separate several; the head is the default and they become the form's options)", models.name)
		}
	}

	// Only the web face enqueues tasks, so the queue name is a web-face requirement too.
	if c.Tasks.QueueID == "" {
		return fmt.Errorf("CLOUD_TASKS_QUEUE_ID is not set")
	}

	// The delivery target is also a requirement of the enqueuing side, i.e. the web face. For why
	// it is not derived from SERVICE_URL, see normalizeWorkerURL (deriving it always 404s).
	if c.Tasks.WorkerURL == "" {
		return fmt.Errorf("WORKER_URL is not set (pass the worker service's URL)")
	}

	// The caller SA is a requirement of the enqueuing side, i.e. the web face. The allowlist the worker
	// accepts is set separately with ALLOWED_TASK_SERVICE_ACCOUNTS, so a worker-only process needs none.
	if c.TaskCallerServiceAccount() == "" {
		return fmt.Errorf("TASK_CALLER_SERVICE_ACCOUNT_EMAIL is not set")
	}

	if c.Auth.GoogleClientID == "" || c.Auth.GoogleClientSecret == "" {
		return fmt.Errorf("missing Google OAuth settings (ClientID, ClientSecret)")
	}

	if len(c.Auth.AllowedEmails) == 0 && len(c.Auth.AllowedDomains) == 0 {
		return fmt.Errorf("no allowed email address or domain is set (the allowlist is empty)")
	}

	return nil
}
