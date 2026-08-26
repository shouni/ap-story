package pipeline

import (
	"context"
	"runtime/pprof"
	"testing"

	"github.com/shouni/ap-story/internal/domain"
)

// TestExecuteLabelsGoroutineWithJob は、job_id と command が pprof のゴルーチン
// ラベルに載ることを検証します。
//
// **これはログではなくパニックのトレースバックのための配線です。** Go 1.27 以降、
// ラベルはトレースバックの見出し行に出るため、生成中に落ちたときにどのジョブ
// だったかがスタックだけで特定できます。slogctx による相関は panic では効きません。
//
// ラベルは子ゴルーチンへ継承されるので、コマ・ページの並列生成の中で落ちても
// 同じ job_id が付きます。ここではステップの中で読めることまでを見ます。
func TestExecuteLabelsGoroutineWithJob(t *testing.T) {
	got := map[string]string{}

	runner := newStatusTestRunner(t, nil, labelProbeStep{got: got})
	if err := runner.Execute(context.Background(), statusTestTask()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got["job_id"] != statusTestJobID {
		t.Errorf("job_id ラベル = %q, want %q", got["job_id"], statusTestJobID)
	}
	if want := string(domain.TaskCommandComposeComic); got["command"] != want {
		t.Errorf("command ラベル = %q, want %q", got["command"], want)
	}
}

// labelProbeStep は、実行時のゴルーチンラベルを書き出すテスト用ステップです。
type labelProbeStep struct{ got map[string]string }

func (labelProbeStep) Name() string { return "label-probe" }

func (s labelProbeStep) Execute(ctx context.Context, _ *Context) error {
	pprof.ForLabels(ctx, func(key, value string) bool {
		s.got[key] = value
		return true
	})
	return nil
}
