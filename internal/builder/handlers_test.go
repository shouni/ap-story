package builder

import (
	"testing"

	"github.com/shouni/gcp-kit/auth"
	"github.com/shouni/gcp-kit/worker"
	"github.com/stretchr/testify/require"

	"github.com/shouni/ap-story/internal/domain"
)

// TaskAuth と Worker の片方だけが構成された形を Validate が弾くこと。
//
// router.go は TaskAuth の有無だけを見てルート登録を省くため、この不整合を通すと
// /tasks/generate が黙って 404 になります。設定漏れなのか実装バグなのかが
// リクエストからは区別できないので、起動時に落とします。
func TestAppHandlersValidateRejectsHalfConfiguredWorker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		h       *AppHandlers
		wantErr bool
	}{
		{name: "どちらも nil (web ロール)", h: &AppHandlers{}},
		{
			name:    "TaskAuth だけある",
			h:       &AppHandlers{TaskAuth: auth.NewTaskVerifier("https://worker.example.test", []string{"runner@example.iam.gserviceaccount.com"})},
			wantErr: true,
		},
		{
			name:    "Worker だけある",
			h:       &AppHandlers{Worker: worker.NewHandler[domain.Task](nil)},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.h.Validate()
			if gotErr := err != nil; gotErr != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// M2M 検証器は、audience と許可リストの両方が揃ってはじめて機能します。
//
// 片方でも欠けると ProtectedMiddleware は毎回セッション認証へフォールバックし、
// ブラウザは正常なまま ap-mcp からの呼び出しだけがログイン画面の HTML を受け取ります。
// リクエストからは設定漏れだと分からないので、起動時に落ちることを固定します。
func TestNewM2MVerifierRejectsIncompleteConfiguration(t *testing.T) {
	t.Parallel()

	const serviceURL = "https://service.example.com"
	allowed := []string{"ap-mcp-runner@test-project.iam.gserviceaccount.com"}

	tests := map[string]struct {
		serviceURL string
		allowed    []string
		wantErr    bool
	}{
		"両方そろっていれば構成できる":         {serviceURL: serviceURL, allowed: allowed},
		"許可リストが空なら起動を止める":        {serviceURL: serviceURL, allowed: nil, wantErr: true},
		"SERVICE_URL が空なら起動を止める": {serviceURL: "", allowed: allowed, wantErr: true},
		"どちらも空なら起動を止める":          {serviceURL: "", allowed: nil, wantErr: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := newM2MVerifier(tt.serviceURL, tt.allowed)
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "ALLOWED_M2M_SERVICE_ACCOUNTS")
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.True(t, got.Configured())
		})
	}
}
