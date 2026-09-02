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
	// SERVER_ROLE は明示が必須。役割に関心のないテストはローカル開発と同じ both で読む。
	t.Setenv("SERVER_ROLE", string(serverrole.Both))
}

// WORKER_URL / SERVICE_URL からの導出は worker サービスの URL までを返し、
// タスクのパスは付けません。継ぎ足すのは投入の直前（domain.WorkerTaskURL）です。
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

// モデル名に既定値はありません。既定値へ黙って落ちると、古いモデルを使い続けたまま
// 気付けないためです。画風は設定ではなく assets/prompts/styles.json が持ちます。
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

	// モデル名さえ与えれば go-comic-kit の必須項目を満たすこと。キットが必須項目を
	// 増やしたときに、本番の起動時ではなくここで落ちるようにするための番人。
	// 順序は workflow.New と同じ（ApplyDefaults → Validate）にします。
	//
	// 画風はキットの必須項目ではありません（プリセットから呼び出しごとに渡します）。
	kit := cfg.AI.KitConfig()
	kit.ApplyDefaults()
	require.NoError(t, kit.Validate())
}

// モデル一覧は先頭が既定で、残りはフォームの選択肢になります。
func TestLoadConfigKeepsExplicitAISettings(t *testing.T) {
	setDefaultURLConfigEnv(t)
	t.Setenv("GEMINI_MODELS", " gemini-explicit , gemini-alt ")
	t.Setenv("IMAGE_MODELS", "image-explicit,image-alt")

	cfg, err := LoadConfig()
	require.NoError(t, err)

	require.Equal(t, []string{"gemini-explicit", "gemini-alt"}, cfg.AI.GeminiModels)
	require.Equal(t, []string{"image-explicit", "image-alt"}, cfg.AI.ImageModels)
}

// キットへ渡すのは実行制御だけです。モデル・画風・比率・解像度は作品ごとに変わる値なので、
// 設定ではなく生成の呼び出しごとに渡します（渡し先は pipeline のステップ）。
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
	// 三段のタイムアウトはデプロイ設定が決めるため、アプリは既定値を持ちません。
	t.Setenv("TASK_DISPATCH_DEADLINE", "30m")
	t.Setenv("PIPELINE_TIMEOUT", "25m")
}

func TestValidateEssentialConfigPassesWithAllRequiredFields(t *testing.T) {
	essentialConfigEnv(t)
	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.NoError(t, cfg.ValidateEssentialConfig())
}

// モデル一覧は Web 面だけの要件です。フォームの選択肢と投入時の許可リストに使い、
// worker は読みません（ジョブが必ず自分のモデル名を運ぶため）。
func TestValidateEssentialConfigRequiresModelsOnWebOnly(t *testing.T) {
	for _, name := range []string{"IMAGE_MODELS", "GEMINI_MODELS"} {
		t.Run(name+" は Web 面で必須", func(t *testing.T) {
			for _, role := range []serverrole.Role{serverrole.Web, serverrole.Both} {
				essentialConfigEnv(t)
				t.Setenv("SERVER_ROLE", string(role))
				t.Setenv(name, "")
				cfg, err := LoadConfig()
				require.NoError(t, err)
				require.ErrorContains(t, cfg.ValidateEssentialConfig(), name, "role=%s", role)
			}
		})

		t.Run(name+" は worker では不要", func(t *testing.T) {
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

// バケット「名」であって URI ではありません。コンソールから貼ると `gs://story/`
// の形になり、素通しすると成果物の URI が `gs://gs://story//...` になります。
func TestLoadConfigNormalizesGCSBucket(t *testing.T) {
	setDefaultURLConfigEnv(t)
	t.Setenv("STORY_BUCKET", " gs://story-bucket/ ")

	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "story-bucket", cfg.Storage.GCSBucket)
}
