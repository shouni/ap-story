package pipeline

import "context"

// Step は、Worker パイプラインの1ステップを表すインターフェースです。
type Step interface {
	Name() string
	Execute(ctx context.Context, pc *Context) error
}
