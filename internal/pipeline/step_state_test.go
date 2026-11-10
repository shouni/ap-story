package pipeline

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/shouni/go-comic-kit/ports"
)

// unreachableReader represents reads that are failing temporarily.
type unreachableReader struct{}

func (unreachableReader) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("connection reset")
}

// TestLoadStateStepsStopOnReadFailure checks that a failure to read an existing state does not
// move on as "not there yet". Moving on would have OutlineStep create a fresh state and
// SaveStateStep overwrite the saved script and generated images.
func TestLoadStateStepsStopOnReadFailure(t *testing.T) {
	t.Parallel()

	for name, step := range map[string]Step{
		"LoadStateStepOptional": LoadStateStepOptional{},
		"LoadStateIfExistsStep": LoadStateIfExistsStep{},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pc := &Context{}
			pc.OutputDir = "gs://b/works/w1"
			pc.DesignJobOutputDir = "gs://b/design-jobs/w1"
			pc.Reader = unreachableReader{}

			err := step.Execute(context.Background(), pc)
			if !errors.Is(err, ports.ErrGeneration) {
				t.Fatalf("Execute() error = %v, want errors.Is(..., ports.ErrGeneration)", err)
			}
			if pc.Manga != nil {
				t.Error("Manga was set even though it could not be read")
			}
		})
	}
}

// TestLoadStateStepsTreatMissingAsNew checks that a missing state is still the normal path.
func TestLoadStateStepsTreatMissingAsNew(t *testing.T) {
	t.Parallel()

	for name, step := range map[string]Step{
		"LoadStateStepOptional": LoadStateStepOptional{},
		"LoadStateIfExistsStep": LoadStateIfExistsStep{},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pc := &Context{}
			pc.OutputDir = "gs://b/works/w1"
			pc.DesignJobOutputDir = "gs://b/design-jobs/w1"
			pc.Reader = newMemStore()

			if err := step.Execute(context.Background(), pc); err != nil {
				t.Fatalf("Execute() error = %v, want nil for a missing state", err)
			}
			if pc.Manga != nil {
				t.Error("a state that should not exist was set")
			}
		})
	}
}
