package pipeline

import "context"

// Step is the interface for one step of the worker pipeline.
type Step interface {
	Name() string
	Execute(ctx context.Context, pc *Context) error
}
