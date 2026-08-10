package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// newRoleTestConfig は、役割ごとの検証だけを見たいときの土台になる設定を返します。
// Web 面に固有の設定（OAuth・認可リスト・キュー）はあえて空にしてあり、
// 各テストが必要に応じて埋めます。
func newRoleTestConfig(role ServerRole) *Config {
	cfg := &Config{}
	cfg.Server.ServiceURL = "https://ap-story.example.run.app"
	cfg.Server.Role = role
	cfg.GCP.ProjectID = "test-project"
	cfg.GCP.LocationID = "asia-northeast1"
	cfg.Tasks.CallerServiceAccountEmail = "caller@test-project.iam.gserviceaccount.com"
	cfg.Tasks.AllowedServiceAccounts = []string{"web-runner@test-project.iam.gserviceaccount.com"}
	cfg.Tasks.TaskAudienceURL = "https://ap-story-worker.example.run.app"
	cfg.Storage.GCSBucket = "ap-story"
	// 画像モデルはどの役割でも必須。テキストモデルは台本を作る worker だけ。
	cfg.AI.ImageModels = []string{"image-test"}
	cfg.AI.GeminiModels = []string{"gemini-test"}
	return cfg
}

func withWebConfig(cfg *Config) *Config {
	cfg.Tasks.QueueID = "story-queue"
	cfg.Auth.GoogleClientID = "client-id"
	cfg.Auth.GoogleClientSecret = "client-secret"
	cfg.Auth.SessionSecret = "0123456789abcdef"
	cfg.Auth.SessionEncryptKey = "0123456789abcdef"
	cfg.Auth.AllowedEmails = []string{"user@example.com"}
	cfg.Auth.AllowedM2MServiceAccounts = []string{"ap-mcp-runner@test-project.iam.gserviceaccount.com"}
	return cfg
}

// TestParseServerRole は、SERVER_ROLE の明示を必須にしていることを確認します。
//
// 未設定が both に落ちると、本番の環境変数が 1 つ欠けただけで公開 web に
// /tasks/generate が復活します。ここが退行すると、その設定漏れが黙って通ります。
func TestParseServerRole(t *testing.T) {
	t.Run("有効な値", func(t *testing.T) {
		tests := []struct {
			raw  string
			want ServerRole
		}{
			{raw: "web", want: ServerRoleWeb},
			{raw: "worker", want: ServerRoleWorker},
			{raw: "both", want: ServerRoleBoth},
			// 大文字と前後の空白は正規化して受け付ける。
			{raw: " WEB ", want: ServerRoleWeb},
		}

		for _, tt := range tests {
			got, err := ParseServerRole(tt.raw)
			require.NoError(t, err, "raw=%q", tt.raw)
			require.Equal(t, tt.want, got, "raw=%q", tt.raw)
		}
	})

	t.Run("空文字と未知の値はエラー", func(t *testing.T) {
		for _, raw := range []string{"", "   ", "wrker", "all", "true"} {
			_, err := ParseServerRole(raw)
			require.Error(t, err, "raw=%q が受理されている", raw)
		}
	})
}

func TestServerRolePredicates(t *testing.T) {
	tests := []struct {
		role       ServerRole
		servesWeb  bool
		servesWork bool
	}{
		{ServerRoleBoth, true, true},
		{ServerRoleWeb, true, false},
		{ServerRoleWorker, false, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			require.Equal(t, tt.servesWeb, tt.role.ServesWeb())
			require.Equal(t, tt.servesWork, tt.role.ServesWorker())
		})
	}
}

// TestValidateEssentialConfigSkipsWebRequirementsForWorker は、Worker 専用プロセスが
// OAuth 設定なしで起動できることを確認します。これが成り立たないと、
// 使いもしない認証情報へのアクセス権を Worker のサービスアカウントに与える必要が生じます。
func TestValidateEssentialConfigSkipsWebRequirementsForWorker(t *testing.T) {
	cfg := newRoleTestConfig(ServerRoleWorker)

	require.NoError(t, cfg.ValidateEssentialConfig())
}

func TestValidateEssentialConfigRequiresWebSettings(t *testing.T) {
	for _, role := range []ServerRole{ServerRoleWeb, ServerRoleBoth} {
		t.Run(string(role), func(t *testing.T) {
			cfg := newRoleTestConfig(role)

			require.Error(t, cfg.ValidateEssentialConfig())
			require.NoError(t, withWebConfig(cfg).ValidateEssentialConfig())
		})
	}
}

// TestValidateEssentialConfigQueueIsWebOnly は、CLOUD_TASKS_QUEUE_ID が Web 面だけの
// 要件であることを確認します。タスクを投入するのは Web 面だけで、
// Worker はキュー名を知る必要がありません。
func TestValidateEssentialConfigQueueIsWebOnly(t *testing.T) {
	t.Run("worker はキュー名なしで起動できる", func(t *testing.T) {
		cfg := newRoleTestConfig(ServerRoleWorker)
		cfg.Tasks.QueueID = ""

		require.NoError(t, cfg.ValidateEssentialConfig())
	})

	t.Run("web はキュー名が必須", func(t *testing.T) {
		cfg := withWebConfig(newRoleTestConfig(ServerRoleWeb))
		cfg.Tasks.QueueID = ""

		err := cfg.ValidateEssentialConfig()
		require.Error(t, err)
		require.Contains(t, err.Error(), "CLOUD_TASKS_QUEUE_ID")
	})
}

// TestValidateEssentialConfigRequiresTaskAudienceForWorker は、Worker が
// audience 未設定のまま起動しないことを確認します。未設定だと OIDC 検証器が
// fail-closed になり、全タスクが 500 で失敗し続けます。
func TestValidateEssentialConfigRequiresTaskAudienceForWorker(t *testing.T) {
	cfg := newRoleTestConfig(ServerRoleWorker)
	cfg.Tasks.TaskAudienceURL = ""

	err := cfg.ValidateEssentialConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "TASK_AUDIENCE_URL")
}

// TestValidateEssentialConfigServiceAccountIsWebOnly は、caller SA が
// 「タスクを投入する側」＝ web 面の要件であることを確認します。
// worker が受け付ける許可リストは ALLOWED_TASK_SERVICE_ACCOUNTS で別に指定するため、
// worker 専用プロセスは caller SA を持たずに済みます。
func TestValidateEssentialConfigServiceAccountIsWebOnly(t *testing.T) {
	t.Run("web は caller SA が必須", func(t *testing.T) {
		cfg := withWebConfig(newRoleTestConfig(ServerRoleWeb))
		cfg.Tasks.CallerServiceAccountEmail = ""

		err := cfg.ValidateEssentialConfig()
		require.Error(t, err)
		require.Contains(t, err.Error(), "TASK_CALLER_SERVICE_ACCOUNT_EMAIL")
	})

	t.Run("worker は許可リストを明示すれば caller SA 不要", func(t *testing.T) {
		cfg := newRoleTestConfig(ServerRoleWorker)
		cfg.Tasks.CallerServiceAccountEmail = ""
		cfg.Tasks.AllowedServiceAccounts = []string{"ap-story-web-runner@test-project.iam.gserviceaccount.com"}

		require.NoError(t, cfg.ValidateEssentialConfig())
	})
}

// TestValidateEssentialConfigRequiresAllowlistForWorker は、受け付ける発行元が
// 1 件も無いまま Worker が起動しないことを確認します。許可リストが空だと
// OIDC 検証器は fail-closed になり、全タスクが失敗し続けます。
func TestValidateEssentialConfigRequiresAllowlistForWorker(t *testing.T) {
	cfg := newRoleTestConfig(ServerRoleWorker)
	cfg.Tasks.AllowedServiceAccounts = nil

	err := cfg.ValidateEssentialConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "ALLOWED_TASK_SERVICE_ACCOUNTS")
}

func TestLoadConfigNormalizesServerRole(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    ServerRole
		wantErr bool
	}{
		{name: "both", raw: "both", want: ServerRoleBoth},
		// 未設定を both に落とすと、本番の環境変数が 1 つ欠けただけで
		// 公開 web に /tasks/generate が復活します。
		{name: "未設定は拒否", raw: "", wantErr: true},
		{name: "web", raw: "web", want: ServerRoleWeb},
		{name: "worker", raw: "worker", want: ServerRoleWorker},
		{name: "大文字と空白を許容", raw: " Worker ", want: ServerRoleWorker},
		{name: "未知の値は拒否", raw: "batch", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setDefaultURLConfigEnv(t)
			t.Setenv("SERVER_ROLE", tt.raw)

			cfg, err := LoadConfig()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "SERVER_ROLE")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, cfg.Server.Role)
		})
	}
}

// TestTaskCallerServiceAccount は、caller SA の解決順を確認します。
//
// 新しい TASK_CALLER_SERVICE_ACCOUNT_EMAIL を優先し、無ければ旧 SERVICE_ACCOUNT_EMAIL に
// フォールバックします。後者は Terraform を切り替えるまでの移行用で、適用後に削除します。
func TestTaskCallerServiceAccount(t *testing.T) {
	tests := []struct {
		name   string
		caller string
		want   string
	}{
		{
			name:   "新しい変数があればそれを使う",
			caller: "caller@test-project.iam.gserviceaccount.com",
			want:   "caller@test-project.iam.gserviceaccount.com",
		},
		{
			name:   "前後の空白は落とす",
			caller: "  caller@test-project.iam.gserviceaccount.com  ",
			want:   "caller@test-project.iam.gserviceaccount.com",
		},
		{name: "未設定なら空"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Tasks.CallerServiceAccountEmail = tt.caller

			require.Equal(t, tt.want, cfg.TaskCallerServiceAccount())
		})
	}
}
