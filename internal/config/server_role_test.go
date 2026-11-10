package config

import (
	"reflect"
	"testing"
	"time"

	"github.com/shouni/go-serve-kit/serverrole"

	"github.com/stretchr/testify/require"
)

// newRoleTestConfig returns a base configuration for tests that only look at per-role validation.
// Web-face settings (OAuth, the allowlist, the queue) are deliberately empty, and
// each test fills in what it needs.
func newRoleTestConfig(role serverrole.Role) *Config {
	cfg := &Config{}
	cfg.Server.ServiceURL = "https://ap-story.example.run.app"
	cfg.Server.Role = role
	cfg.GCP.ProjectID = "test-project"
	cfg.GCP.LocationID = "asia-northeast1"
	cfg.Tasks.CallerServiceAccountEmail = "caller@test-project.iam.gserviceaccount.com"
	cfg.Tasks.AllowedServiceAccounts = []string{"web-runner@test-project.iam.gserviceaccount.com"}
	cfg.Tasks.TaskAudienceURL = "https://ap-story-worker.example.run.app"
	cfg.Storage.GCSBucket = "ap-story"
	// The image models are required for every role; the text models only for the worker, which writes the script.
	cfg.AI.ImageModels = []string{"image-test"}
	cfg.AI.GeminiModels = []string{"gemini-test"}
	// env defaults do not reach a bare struct, so set the same state as the real configuration.
	// Left at 0, the worker validation would fail on "unlimited".
	cfg.AI.PipelineTimeout = 25 * time.Minute
	// env defaults do not reach a bare struct, so set the same state as the real configuration.
	cfg.Tasks.DispatchDeadline = testDispatchDeadline
	return cfg
}

func withWebConfig(cfg *Config) *Config {
	cfg.Tasks.QueueID = "story-queue"
	cfg.Tasks.WorkerURL = "https://ap-story-worker.example.run.app"
	cfg.Auth.GoogleClientID = "client-id"
	cfg.Auth.GoogleClientSecret = "client-secret"
	cfg.Auth.AllowedEmails = []string{"user@example.com"}
	cfg.Auth.AllowedM2MServiceAccounts = []string{"mcp-runner@test-project.iam.gserviceaccount.com"}
	return cfg
}

// TestValidateEssentialConfigSkipsWebRequirementsForWorker checks that a worker-only process
// starts without OAuth settings. Otherwise the worker's service account would need access to
// credentials it never uses.
func TestValidateEssentialConfigSkipsWebRequirementsForWorker(t *testing.T) {
	cfg := newRoleTestConfig(serverrole.Worker)

	require.NoError(t, cfg.ValidateEssentialConfig())
}

func TestValidateEssentialConfigRequiresWebSettings(t *testing.T) {
	for _, role := range []serverrole.Role{serverrole.Web, serverrole.Both} {
		t.Run(string(role), func(t *testing.T) {
			cfg := newRoleTestConfig(role)

			require.Error(t, cfg.ValidateEssentialConfig())
			require.NoError(t, withWebConfig(cfg).ValidateEssentialConfig())
		})
	}
}

// TestValidateEssentialConfigQueueIsWebOnly checks that CLOUD_TASKS_QUEUE_ID is a web-face
// requirement only. Only the web face enqueues tasks, and the worker does not need to
// know the queue name.
func TestValidateEssentialConfigQueueIsWebOnly(t *testing.T) {
	t.Run("worker starts without a queue name", func(t *testing.T) {
		cfg := newRoleTestConfig(serverrole.Worker)
		cfg.Tasks.QueueID = ""

		require.NoError(t, cfg.ValidateEssentialConfig())
	})

	t.Run("web requires a queue name", func(t *testing.T) {
		cfg := withWebConfig(newRoleTestConfig(serverrole.Web))
		cfg.Tasks.QueueID = ""

		err := cfg.ValidateEssentialConfig()
		require.Error(t, err)
		require.Contains(t, err.Error(), "CLOUD_TASKS_QUEUE_ID")
	})
}

// TestValidateEssentialConfigRequiresTaskAudienceForWorker checks that the worker does not
// start without an audience. Unset, the OIDC verifier fails closed and every task keeps
// failing with 500.
func TestValidateEssentialConfigRequiresTaskAudienceForWorker(t *testing.T) {
	cfg := newRoleTestConfig(serverrole.Worker)
	cfg.Tasks.TaskAudienceURL = ""

	err := cfg.ValidateEssentialConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "TASK_AUDIENCE_URL")
}

// TestValidateEssentialConfigServiceAccountIsWebOnly checks that the caller SA is a requirement
// of "the side that enqueues tasks", i.e. the web face. The allowlist the worker accepts is
// set separately with ALLOWED_TASK_SERVICE_ACCOUNTS, so a worker-only process needs
// no caller SA.
func TestValidateEssentialConfigServiceAccountIsWebOnly(t *testing.T) {
	t.Run("web requires a caller SA", func(t *testing.T) {
		cfg := withWebConfig(newRoleTestConfig(serverrole.Web))
		cfg.Tasks.CallerServiceAccountEmail = ""

		err := cfg.ValidateEssentialConfig()
		require.Error(t, err)
		require.Contains(t, err.Error(), "TASK_CALLER_SERVICE_ACCOUNT_EMAIL")
	})

	t.Run("worker needs no caller SA when the allowlist is explicit", func(t *testing.T) {
		cfg := newRoleTestConfig(serverrole.Worker)
		cfg.Tasks.CallerServiceAccountEmail = ""
		cfg.Tasks.AllowedServiceAccounts = []string{"ap-story-web-runner@test-project.iam.gserviceaccount.com"}

		require.NoError(t, cfg.ValidateEssentialConfig())
	})
}

// TestValidateEssentialConfigRequiresAllowlistForWorker checks that the worker does not start
// with no accepted issuer at all. With an empty allowlist the OIDC verifier fails closed and
// every task keeps failing.
func TestValidateEssentialConfigRequiresAllowlistForWorker(t *testing.T) {
	cfg := newRoleTestConfig(serverrole.Worker)
	cfg.Tasks.AllowedServiceAccounts = nil

	err := cfg.ValidateEssentialConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "ALLOWED_TASK_SERVICE_ACCOUNTS")
}

func TestLoadConfigNormalizesServerRole(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		want      serverrole.Role
		wantErr   bool
		wantInErr string
	}{
		{name: "both", raw: "both", want: serverrole.Both},
		// Falling back to both when unset would bring the worker routes back on the public web
		// as soon as one production env var went missing. With no value the decoder does not call
		// UnmarshalText, so normalize returns the error with the env var name.
		{name: "unset is rejected", raw: "", wantErr: true, wantInErr: "SERVER_ROLE"},
		{name: "web", raw: "web", want: serverrole.Web},
		{name: "worker", raw: "worker", want: serverrole.Worker},
		{name: "case and whitespace are tolerated", raw: " Worker ", want: serverrole.Worker},
		// An unknown value is rejected when env decodes it (serverrole.Role implements
		// encoding.TextUnmarshaler). env wraps the error, so the field name appears rather
		// than the env var name, but what is wrong and what would pass are kept.
		{name: "unknown value is rejected", raw: "batch", wantErr: true, wantInErr: `"batch"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setDefaultURLConfigEnv(t)
			t.Setenv("SERVER_ROLE", tt.raw)

			cfg, err := LoadConfig()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantInErr)
				// On either path, the error must show which values pass.
				require.Contains(t, err.Error(), `"web"`)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, cfg.Server.Role)
		})
	}
}

// TestTaskCallerServiceAccount checks how the caller SA is resolved.
//
// The new TASK_CALLER_SERVICE_ACCOUNT_EMAIL wins, falling back to the old SERVICE_ACCOUNT_EMAIL.
// The latter is for the migration until Terraform switches over and is removed after it is applied.
func TestTaskCallerServiceAccount(t *testing.T) {
	tests := []struct {
		name   string
		caller string
		want   string
	}{
		{
			name:   "uses the new variable when set",
			caller: "caller@test-project.iam.gserviceaccount.com",
			want:   "caller@test-project.iam.gserviceaccount.com",
		},
		{
			name:   "drops surrounding whitespace",
			caller: "  caller@test-project.iam.gserviceaccount.com  ",
			want:   "caller@test-project.iam.gserviceaccount.com",
		},
		{name: "empty when unset"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Tasks.CallerServiceAccountEmail = tt.caller

			require.Equal(t, tt.want, cfg.TaskCallerServiceAccount())
		})
	}
}

// TestValidateEssentialConfigRequiresPipelineTimeoutUnderDispatchDeadline checks that the
// worker starts only with settings where it gives up on its own before Cloud Tasks does.
//
// Equal, longer and unlimited are all the same in that the cut comes from Cloud Tasks. Then
// the whole process is stopped, so neither the failure handler nor the Slack notice runs, and with
// max_attempts = 1 and no retry the job stays at running. It also pins that the defaults satisfy this relation.
func TestValidateEssentialConfigRequiresPipelineTimeoutUnderDispatchDeadline(t *testing.T) {
	// There is no default. The source is the deployment configuration (Terraform), in one place.
	t.Run("has no default", func(t *testing.T) {
		field, ok := reflect.TypeFor[AIConfig]().FieldByName("PipelineTimeout")
		require.True(t, ok)
		require.Empty(t, field.Tag.Get("envDefault"), "do not give it a default")

		require.NoError(t, newRoleTestConfig(serverrole.Worker).ValidateEssentialConfig())
	})

	for _, tt := range []struct {
		name    string
		timeout time.Duration
	}{
		{name: "fails when equal to the cutoff", timeout: testDispatchDeadline},
		{name: "fails when longer than the cutoff", timeout: testDispatchDeadline + time.Minute},
		{name: "fails when unlimited", timeout: 0},
		{name: "fails when negative (unlimited)", timeout: -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newRoleTestConfig(serverrole.Worker)
			cfg.AI.PipelineTimeout = tt.timeout

			err := cfg.ValidateEssentialConfig()
			require.Error(t, err)
			require.Contains(t, err.Error(), "PIPELINE_TIMEOUT")
		})
	}

	// The web face builds no pipeline, so it is outside this check.
	t.Run("web starts even when unlimited", func(t *testing.T) {
		cfg := withWebConfig(newRoleTestConfig(serverrole.Web))
		cfg.AI.PipelineTimeout = 0

		require.NoError(t, cfg.ValidateEssentialConfig())
	})
}

// testDispatchDeadline is the cutoff used in tests. The app holds no default, so it is set
// explicitly, as in the real deployment configuration.
const testDispatchDeadline = 30 * time.Minute
