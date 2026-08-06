// Package handlers は、Web UI（フォーム表示・生成・履歴閲覧等）のHTTPハンドラーを提供します。
package handlers

import "context"

type csrfTokenContextKey struct{}

// WithCSRFToken は、テンプレートに公開すべきCSRFトークンをコンテキストに保存します。
func WithCSRFToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, csrfTokenContextKey{}, token)
}

// CSRFTokenFromContext は、コンテキストに保存されたCSRFトークンを取得します。
func CSRFTokenFromContext(ctx context.Context) string {
	token, ok := ctx.Value(csrfTokenContextKey{}).(string)
	if !ok {
		return ""
	}
	return token
}
