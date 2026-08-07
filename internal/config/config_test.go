package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func setDefaultURLConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SERVICE_URL", "http://localhost:8080")
	t.Setenv("WORKER_URL", "")
	t.Setenv("TASK_AUDIENCE_URL", "")
}

func TestLoadConfigNormalizesWorkerURL(t *testing.T) {
	tests := []struct {
		name       string
		serviceURL string
		workerURL  string
		want       string
	}{
		{
			name: "uses service URL fallback",
			want: "http://localhost:8080/tasks/generate",
		},
		{
			name:      "trims explicit worker URL",
			workerURL: " https://worker.example.com/tasks/generate ",
			want:      "https://worker.example.com/tasks/generate",
		},
		{
			name:       "joins query and fragment safely",
			serviceURL: " https://service.example.com/base?debug=true#worker ",
			want:       "https://service.example.com/base/tasks/generate?debug=true#worker",
		},
		{
			name:       "trims trailing slash before fallback",
			serviceURL: " https://service.example.com/ ",
			want:       "https://service.example.com/tasks/generate",
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

// TestLoadConfigParsesAllowedTaskServiceAccounts は、カンマ区切りの許可リストが
// 前後の空白を落として読み込まれることを確認します。
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

func TestAIConfigKitConfigMapsFields(t *testing.T) {
	ai := AIConfig{
		GeminiModel:         "gemini-x",
		ImageStandardModel:  "image-std",
		ImageQualityModel:   "image-quality",
		StyleSuffix:         "style",
		DesignStyleSuffix:   "design-style",
		MaxConcurrency:      3,
		MaxChapters:         5,
		MaxPanelsPerChapter: 6,
		MaxPanelsPerPage:    4,
	}

	kit := ai.KitConfig()
	require.Equal(t, "gemini-x", kit.GeminiModel)
	require.Equal(t, "image-std", kit.ImageStandardModel)
	require.Equal(t, "image-quality", kit.ImageQualityModel)
	require.Equal(t, "style", kit.StyleSuffix)
	require.Equal(t, "design-style", kit.DesignStyleSuffix)
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
	t.Setenv("SESSION_SECRET", "session-secret-32-bytes-long!!!!")
	t.Setenv("SESSION_ENCRYPT_KEY", "0123456789abcdef")
	t.Setenv("ALLOWED_EMAILS", "user@example.com")
	t.Setenv("ALLOWED_M2M_SERVICE_ACCOUNTS", "sa@project.iam.gserviceaccount.com")
	t.Setenv("GCP_PROJECT_ID", "proj")
	t.Setenv("GCP_LOCATION_ID", "asia-northeast1")
	t.Setenv("CLOUD_TASKS_QUEUE_ID", "queue")
	t.Setenv("SERVICE_ACCOUNT_EMAIL", "runner@project.iam.gserviceaccount.com")
	t.Setenv("STORY_BUCKET", "bucket")
	t.Setenv("CHARACTERS_JSON_PATH", "gs://bucket/characters.json")
}

func TestValidateEssentialConfigPassesWithAllRequiredFields(t *testing.T) {
	essentialConfigEnv(t)
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateEssentialConfig())
}

func TestValidateEssentialConfigRequiresHTTPSServiceURL(t *testing.T) {
	essentialConfigEnv(t)
	t.Setenv("SERVICE_URL", "http://service.example.com")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.ErrorContains(t, cfg.ValidateEssentialConfig(), "HTTPS")
}

func TestValidateEssentialConfigRequiresM2MServiceAccounts(t *testing.T) {
	essentialConfigEnv(t)
	t.Setenv("ALLOWED_M2M_SERVICE_ACCOUNTS", "")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.ErrorContains(t, cfg.ValidateEssentialConfig(), "ALLOWED_M2M_SERVICE_ACCOUNTS")
}

func TestValidateEssentialConfigRequiresGoogleOAuthFields(t *testing.T) {
	for _, env := range []string{"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "SESSION_SECRET"} {
		t.Run(env, func(t *testing.T) {
			essentialConfigEnv(t)
			t.Setenv(env, "")
			cfg, err := LoadConfig()
			require.NoError(t, err)
			require.ErrorContains(t, cfg.ValidateEssentialConfig(), "google OAuth")
		})
	}
}

func TestValidateEssentialConfigRequiresAllowedEmailsOrDomains(t *testing.T) {
	essentialConfigEnv(t)
	t.Setenv("ALLOWED_EMAILS", "")
	t.Setenv("ALLOWED_DOMAINS", "")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.ErrorContains(t, cfg.ValidateEssentialConfig(), "認可リスト")
}

func TestValidateEssentialConfigValidatesSessionEncryptKeyLength(t *testing.T) {
	essentialConfigEnv(t)
	t.Setenv("SESSION_ENCRYPT_KEY", "too-short")
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.ErrorContains(t, cfg.ValidateEssentialConfig(), "SESSION_ENCRYPT_KEY")
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
	// 警告はログにしか出ないため、ここでは「呼んでも落ちない」ことだけを担保する。
	// 組み合わせの意味づけはコメントと README に書いてあり、挙動は go-comic-kit 側の責務。
	cases := map[string]AIConfig{
		"並列とレート制限が両立していない": {MaxConcurrency: 4, RateInterval: 10 * time.Second},
		"タイムアウトが短すぎる":      {RequestTimeout: 30 * time.Second},
		"既定値":              {MaxConcurrency: 1, RateInterval: 10 * time.Second, RequestTimeout: 5 * time.Minute},
		"ゼロ値":              {},
	}
	for name, ai := range cases {
		t.Run(name, func(*testing.T) {
			cfg := &Config{AI: ai}
			cfg.WarnOnContradictoryGenerationSettings()
		})
	}
}
