package builder

import (
	"testing"

	"github.com/shouni/gcp-kit/auth/oidc"
	"github.com/shouni/gcp-kit/worker"

	"github.com/shouni/ap-story/internal/domain"
)

// Validate rejects a build where only one of TaskAuth and Worker is configured.
//
// router.go skips route registration based only on whether TaskAuth is present, so letting
// this mismatch through would silently turn /tasks/generate into a 404. A request cannot
// tell a missing setting from an implementation bug, so startup fails instead.
func TestAppHandlersValidateRejectsHalfConfiguredWorker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		h       *AppHandlers
		wantErr bool
	}{
		{name: "both nil (web role)", h: &AppHandlers{}},
		{
			name:    "only TaskAuth",
			h:       &AppHandlers{TaskAuth: mustOIDC(t, "https://worker.example.test", []string{"runner@example.iam.gserviceaccount.com"})},
			wantErr: true,
		},
		{
			name:    "only Worker",
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

// mustOIDC builds a configured verifier for tests (New returns an error when settings are missing).
func mustOIDC(t *testing.T, audience string, allowed []string) *oidc.Verifier {
	t.Helper()
	v, err := oidc.New(audience, allowed)
	if err != nil {
		t.Fatalf("oidc.New() error = %v", err)
	}
	return v
}
