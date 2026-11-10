package pipeline

import (
	"context"
	"runtime/pprof"
	"testing"

	"github.com/shouni/ap-story/internal/domain"
)

// TestExecuteLabelsGoroutineWithJob checks that job_id and command are put on the pprof goroutine
// labels.
//
// **This is wiring for panic tracebacks, not for logs.** Since Go 1.27 the labels
// appear in the traceback's heading line, so when generation crashes, the stack alone
// identifies the job. Correlation through slogctx does not work for a panic.
//
// Labels are inherited by child goroutines, so a crash inside the parallel generation of panels or
// pages carries the same job_id. Here it checks only that they can be read inside a step.
func TestExecuteLabelsGoroutineWithJob(t *testing.T) {
	got := map[string]string{}

	runner := newStatusTestRunner(t, nil, labelProbeStep{got: got})
	if err := runner.Execute(context.Background(), statusTestTask()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got["job_id"] != statusTestJobID {
		t.Errorf("job_id label = %q, want %q", got["job_id"], statusTestJobID)
	}
	if want := string(domain.TaskCommandComposeComic); got["command"] != want {
		t.Errorf("command label = %q, want %q", got["command"], want)
	}
}

// labelProbeStep is a test step that writes out the goroutine labels at run time.
type labelProbeStep struct{ got map[string]string }

func (labelProbeStep) Name() string { return "label-probe" }

func (s labelProbeStep) Execute(ctx context.Context, _ *Context) error {
	pprof.ForLabels(ctx, func(key, value string) bool {
		s.got[key] = value
		return true
	})
	return nil
}
