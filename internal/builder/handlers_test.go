package builder

import (
	"testing"

	"github.com/shouni/gcp-kit/auth/oidc"
	"github.com/shouni/gcp-kit/worker"

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
			h:       &AppHandlers{TaskAuth: mustOIDC(t, "https://worker.example.test", []string{"runner@example.iam.gserviceaccount.com"})},
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

// mustOIDC は、テスト用に構成済みの検証器を作ります（New は設定が欠けるとエラーを返します）。
func mustOIDC(t *testing.T, audience string, allowed []string) *oidc.Verifier {
	t.Helper()
	v, err := oidc.New(audience, allowed)
	if err != nil {
		t.Fatalf("oidc.New() error = %v", err)
	}
	return v
}
