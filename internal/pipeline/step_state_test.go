package pipeline

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/shouni/go-comic-kit/ports"
)

// unreachableReader は、読み取りが一時的に失敗している状態を表します。
type unreachableReader struct{}

func (unreachableReader) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("connection reset")
}

// TestLoadStateStepsStopOnReadFailure は、既存 state の読み取りに失敗したときに、
// 「まだ無い」として先へ進まないことを確認します。進んでしまうと OutlineStep が
// 新品の state を作り、SaveStateStep が保存済みの台本と生成済み画像を上書きします。
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
				t.Error("読めなかったのに Manga が設定されています")
			}
		})
	}
}

// TestLoadStateStepsTreatMissingAsNew は、state が無いことは正常系のままであることを確認します。
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
				t.Error("無いはずの state が設定されています")
			}
		})
	}
}
