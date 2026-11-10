// Package handlers provides the HTTP handlers for the web UI (forms, generation, history and so on).
package handlers

import (
	"context"

	"github.com/shouni/gcp-kit/auth/session"
)

// CSRFTokenFromContext returns the CSRF token stored in the context.
//
// It is stored by gcp-kit/auth's CSRFContextMiddleware, so reading it must use the same key.
// This delegates only to keep the name the template-rendering side uses
// in the handlers package.
func CSRFTokenFromContext(ctx context.Context) string {
	return session.CSRFTokenFromContext(ctx)
}
