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
	cfg.GCP.ServiceAccountEmail = "tasks@test-project.iam.gserviceaccount.com"
	cfg.GCP.TaskAudienceURL = "https://ap-story-worker.example.run.app"
	cfg.Storage.GCSBucket = "ap-story"
	return cfg
}

func withWebConfig(cfg *Config) *Config {
	cfg.GCP.QueueID = "story-queue"
	cfg.Auth.GoogleClientID = "client-id"
	cfg.Auth.GoogleClientSecret = "client-secret"
	cfg.Auth.SessionSecret = "0123456789abcdef"
	cfg.Auth.SessionEncryptKey = "0123456789abcdef"
	cfg.Auth.AllowedEmails = []string{"user@example.com"}
	cfg.Auth.AllowedM2MServiceAccounts = []string{"ap-mcp-runner@test-project.iam.gserviceaccount.com"}
	return cfg
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
		cfg.GCP.QueueID = ""

		require.NoError(t, cfg.ValidateEssentialConfig())
	})

	t.Run("web はキュー名が必須", func(t *testing.T) {
		cfg := withWebConfig(newRoleTestConfig(ServerRoleWeb))
		cfg.GCP.QueueID = ""

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
	cfg.GCP.TaskAudienceURL = ""

	err := cfg.ValidateEssentialConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "TASK_AUDIENCE_URL")
}

// TestValidateEssentialConfigRequiresServiceAccountForBothRoles は、
// SERVICE_ACCOUNT_EMAIL が投入側では署名者、受信側では許可リストとして
// 両方の役割で必須であることを確認します。
func TestValidateEssentialConfigRequiresServiceAccountForBothRoles(t *testing.T) {
	for _, role := range []ServerRole{ServerRoleWeb, ServerRoleWorker} {
		t.Run(string(role), func(t *testing.T) {
			cfg := withWebConfig(newRoleTestConfig(role))
			cfg.GCP.ServiceAccountEmail = ""

			err := cfg.ValidateEssentialConfig()
			require.Error(t, err)
			require.Contains(t, err.Error(), "SERVICE_ACCOUNT_EMAIL")
		})
	}
}

func TestLoadConfigNormalizesServerRole(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    ServerRole
		wantErr bool
	}{
		{name: "未設定は両方", raw: "", want: ServerRoleBoth},
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
