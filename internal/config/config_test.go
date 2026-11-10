package config

import (
	"testing"
	"time"

	"github.com/shouni/go-serve-kit/serverrole"

	"github.com/stretchr/testify/require"
)

func setDefaultURLConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SERVICE_URL", "http://localhost:8080")
	t.Setenv("WORKER_URL", "")
	t.Setenv("TASK_AUDIENCE_URL", "")
	// SERVER_ROLE must be set explicitly. Tests that do not care about the role read as both, like local development.
	t.Setenv("SERVER_ROLE", string(serverrole.Both))
}

// Deriving from WORKER_URL / SERVICE_URL returns only the worker service's URL and
// adds no task path. The path is appended just before enqueueing (tasks.Config.WorkerPath).
func TestLoadConfigNormalizesWorkerURL(t *testing.T) {
	tests := []struct {
		name       string
		serviceURL string
		workerURL  string
		want       string
	}{
		{
			name: "uses service URL fallback",
			want: "http://localhost:8080",
		},
		{
			name:      "trims explicit worker URL",
			workerURL: " https://worker.example.com ",
			want:      "https://worker.example.com",
		},
		{
			name:       "keeps query and fragment untouched",
			serviceURL: " https://service.example.com/base?debug=true#worker ",
			want:       "https://service.example.com/base?debug=true#worker",
		},
		{
			name:       "trims surrounding spaces on fallback",
			serviceURL: " https://service.example.com/ ",
			want:       "https://service.example.com/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setDefaultURLConfigEnv(t)
			if tt.serviceURL != "" {
				t.Setenv("SERVICE_URL", tt.serviceURL)
			}
			if tt.workerURL != "" {
				t.Setenv("WORKER_URL", tt.workerURL)
			}

			cfg, err := LoadConfig()
			require.NoError(t, err)
			require.Equal(t, tt.want, cfg.Tasks.WorkerURL)
		})
	}
}

func TestLoadConfigRejectsInvalidServiceURLWhenWorkerURLIsDerived(t *testing.T) {
	setDefaultURLConfigEnv(t)
	t.Setenv("SERVICE_URL", "https://service.example.com/%zz")

	cfg, err := LoadConfig()

	require.Nil(t, cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to normalize config")
	require.Contains(t, err.Error(), "invalid service URL")
}

func TestLoadConfigUsesNormalizedServiceURLForDefaultTaskAudience(t *testing.T) {
	setDefaultURLConfigEnv(t)
	t.Setenv("SERVICE_URL", " https://service.example.com/ ")

	cfg, err := LoadConfig()
	require.NoError(t, err)

	require.Equal(t, "https://service.example.com/", cfg.Tasks.TaskAudienceURL)
}

func TestLoadConfigParsesAllowedM2MServiceAccounts(t *testing.T) {
	setDefaultURLConfigEnv(t)
	t.Setenv("ALLOWED_M2M_SERVICE_ACCOUNTS", " sa-a@project.iam.gserviceaccount.com , sa-b@project.iam.gserviceaccount.com ")

	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, []string{"sa-a@project.iam.gserviceaccount.com", "sa-b@project.iam.gserviceaccount.com"}, cfg.Auth.AllowedM2MServiceAccounts)
}

// TestLoadConfigParsesAllowedTaskServiceAccounts checks that a comma-separated allowlist
// is read with surrounding whitespace dropped.
func TestLoadConfigParsesAllowedTaskServiceAccounts(t *testing.T) {
	setDefaultURLConfigEnv(t)
	t.Setenv("ALLOWED_TASK_SERVICE_ACCOUNTS", " web@test.iam.gserviceaccount.com , worker@test.iam.gserviceaccount.com ")

	cfg, err := LoadConfig()
	require.NoError(t, err)

	require.Equal(t, []string{
		"web@test.iam.gserviceaccount.com",
		"worker@test.iam.gserviceaccount.com",
	}, cfg.Tasks.AllowedServiceAccounts)
}

// Model names have no default. Silently falling back to a default would keep an old model
// in use unnoticed. The art style is not configuration; assets/prompts/styles.json holds it.
func TestLoadConfigAppliesAIDefaults(t *testing.T) {
	setDefaultURLConfigEnv(t)

	cfg, err := LoadConfig()
	require.NoError(t, err)

	require.Empty(t, cfg.AI.GeminiModels)
	require.Empty(t, cfg.AI.ImageModels)
}

func TestLoadConfigProducesValidKitConfig(t *testing.T) {
	setDefaultURLConfigEnv(t)
	t.Setenv("GEMINI_MODELS", "gemini-test")
	t.Setenv("IMAGE_MODELS", "image-test")

	cfg, err := LoadConfig()
	require.NoError(t, err)

	// Giving just the model names must satisfy go-comic-kit's required fields. This is the guard
	// that fails here, rather than at production startup, when the kit adds a required field.
	// The order matches workflow.New (ApplyDefaults → Validate).
	//
	// The art style is not a required field of the kit (it is passed per call from the preset).
	kit := cfg.AI.KitConfig()
	kit.ApplyDefaults()
	require.NoError(t, kit.Validate())
}

// The head of a model list is the default and the rest become the form's options.
func TestLoadConfigKeepsExplicitAISettings(t *testing.T) {
	setDefaultURLConfigEnv(t)
	t.Setenv("GEMINI_MODELS", " gemini-explicit , gemini-alt ")
	t.Setenv("IMAGE_MODELS", "image-explicit,image-alt")

	cfg, err := LoadConfig()
	require.NoError(t, err)

	require.Equal(t, []string{"gemini-explicit", "gemini-alt"}, cfg.AI.GeminiModels)
	require.Equal(t, []string{"image-explicit", "image-alt"}, cfg.AI.ImageModels)
}

// Only execution control goes to the kit. Model, art style, aspect ratio and resolution vary
// per work, so they are passed per generation call rather than configured (to the pipeline steps).
func TestAIConfigKitConfigMapsFields(t *testing.T) {
	ai := AIConfig{
		MaxConcurrency:      3,
		MaxChapters:         5,
		MaxPanelsPerChapter: 6,
		MaxPanelsPerPage:    4,
	}

	kit := ai.KitConfig()
	require.Equal(t, 3, kit.MaxConcurrency)
	require.Equal(t, 5, kit.MaxChapters)
	require.Equal(t, 6, kit.MaxPanelsPerChapter)
	require.Equal(t, 4, kit.MaxPanelsPerPage)
}

func essentialConfigEnv(t *testing.T) {
	t.Helper()
	setDefaultURLConfigEnv(t)
	t.Setenv("SERVICE_URL", "https://service.example.com")
	t.Setenv("GOOGLE_CLIENT_ID", "client-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "client-secret")
	t.Setenv("ALLOWED_EMAILS", "user@example.com")
	t.Setenv("ALLOWED_M2M_SERVICE_ACCOUNTS", "sa@project.iam.gserviceaccount.com")
	t.Setenv("GCP_PROJECT_ID", "proj")
	t.Setenv("GCP_LOCATION_ID", "asia-northeast1")
	t.Setenv("CLOUD_TASKS_QUEUE_ID", "queue")
	t.Setenv("TASK_CALLER_SERVICE_ACCOUNT_EMAIL", "caller@project.iam.gserviceaccount.com")
	t.Setenv("ALLOWED_TASK_SERVICE_ACCOUNTS", "caller@project.iam.gserviceaccount.com")
	t.Setenv("STORY_BUCKET", "bucket")
	t.Setenv("CHARACTERS_JSON_PATH", "gs://bucket/characters.json")
	t.Setenv("GEMINI_MODELS", "gemini-test")
	t.Setenv("IMAGE_MODELS", "image-test")
	// The three-tier timeouts are decided by the deployment, so the app holds no defaults.
	t.Setenv("TASK_DISPATCH_DEADLINE", "30m")
	t.Setenv("PIPELINE_TIMEOUT", "25m")
}

func TestValidateEssentialConfigPassesWithAllRequiredFields(t *testing.T) {
	essentialConfigEnv(t)
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateEssentialConfig())
}

// The model lists are a web-face requirement only: they feed the form's options and the
// allowlist at enqueue, and the worker does not read them (every job carries its own model names).
func TestValidateEssentialConfigRequiresModelsOnWebOnly(t *testing.T) {
	for _, name := range []string{"IMAGE_MODELS", "GEMINI_MODELS"} {
		t.Run(name+" is required on the web face", func(t *testing.T) {
			for _, role := range []serverrole.Role{serverrole.Web, serverrole.Both} {
				essentialConfigEnv(t)
				t.Setenv("SERVER_ROLE", string(role))
				t.Setenv(name, "")
				cfg, err := LoadConfig()
				require.NoError(t, err)
				require.ErrorContains(t, cfg.ValidateEssentialConfig(), name, "role=%s", role)
			}
		})

		t.Run(name+" is not needed on the worker", func(t *testing.T) {
			essentialConfigEnv(t)
			t.Setenv("SERVER_ROLE", string(serverrole.Worker))
			t.Setenv(name, "")
			cfg, err := LoadConfig()
			require.NoError(t, err)
			require.NoError(t, cfg.ValidateEssentialConfig())
		})
	}
}

func TestValidateEssentialConfigRequiresHTTPSServiceURL(t *testing.T) {
	essentialConfigEnv(t)
	t.Setenv("SERVICE_URL", "http://service.example.com")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.ErrorContains(t, cfg.ValidateEssentialConfig(), "HTTPS")
}

func TestValidateEssentialConfigRequiresGoogleOAuthFields(t *testing.T) {
	for _, env := range []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"} {
		t.Run(env, func(t *testing.T) {
			essentialConfigEnv(t)
			t.Setenv(env, "")
			cfg, err := LoadConfig()
			require.NoError(t, err)
			require.ErrorContains(t, cfg.ValidateEssentialConfig(), "Google OAuth")
		})
	}
}

func TestValidateEssentialConfigRequiresAllowedEmailsOrDomains(t *testing.T) {
	essentialConfigEnv(t)
	t.Setenv("ALLOWED_EMAILS", "")
	t.Setenv("ALLOWED_DOMAINS", "")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.ErrorContains(t, cfg.ValidateEssentialConfig(), "allowlist")
}

func TestValidateEssentialConfigRequiresStorageFields(t *testing.T) {
	cases := map[string]string{
		"STORY_BUCKET": "STORY_BUCKET",
	}
	for env, wantMsg := range cases {
		t.Run(env, func(t *testing.T) {
			essentialConfigEnv(t)
			t.Setenv(env, "")
			cfg, err := LoadConfig()
			require.NoError(t, err)
			require.ErrorContains(t, cfg.ValidateEssentialConfig(), wantMsg)
		})
	}
}

func TestValidateEssentialConfigAllowsEmptyCharactersJSONPath(t *testing.T) {
	essentialConfigEnv(t)
	t.Setenv("CHARACTERS_JSON_PATH", "")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateEssentialConfig())
}

func TestWarnOnContradictoryGenerationSettings(t *testing.T) {
	// Warnings only reach the log, so all this guarantees is that calling it does not fail.
	// What each combination means is written in the comments and README; the behaviour is go-comic-kit's job.
	cases := map[string]AIConfig{
		"concurrency and rate limit conflict": {MaxConcurrency: 4, RateInterval: 10 * time.Second},
		"timeout too short":                   {RequestTimeout: 30 * time.Second},
		"defaults":                            {MaxConcurrency: 1, RateInterval: 10 * time.Second, RequestTimeout: 5 * time.Minute},
		"zero value":                          {},
	}
	for name, ai := range cases {
		t.Run(name, func(*testing.T) {
			cfg := &Config{AI: ai}
			cfg.WarnOnContradictoryGenerationSettings()
		})
	}
}

// A bucket *name*, not a URI. Pasting from the console gives the form `gs://story/`, and
// passing it through makes artifact URIs `gs://gs://story//...`.
func TestLoadConfigNormalizesGCSBucket(t *testing.T) {
	setDefaultURLConfigEnv(t)
	t.Setenv("STORY_BUCKET", " gs://story-bucket/ ")

	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "story-bucket", cfg.Storage.GCSBucket)
}
