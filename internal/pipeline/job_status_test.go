package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/shouni/ap-story/internal/domain"
	"github.com/shouni/go-job-kit/jobstatus"
)

// fakeJobStatusStore はメモリ上でジョブ状態を保持するテスト用ストアです。
type fakeJobStatusStore struct {
	mu       sync.Mutex
	statuses map[string]domain.JobStatus
	saved    []domain.JobState
}

func newFakeJobStatusStore() *fakeJobStatusStore {
	return &fakeJobStatusStore{statuses: map[string]domain.JobStatus{}}
}

func (s *fakeJobStatusStore) Save(_ context.Context, _ string, status domain.JobStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses[status.JobID] = status
	s.saved = append(s.saved, status.State)
	return nil
}

func (s *fakeJobStatusStore) Get(_ context.Context, jobID string) (domain.JobStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status, ok := s.statuses[jobID]
	if !ok {
		// 未記録は ErrNotFound を包んで返す（Store の契約）。素のエラーを返すと
		// 「読めなかった」に分類され、再実行ガードがエラーを返します。
		return domain.JobStatus{}, fmt.Errorf("%w: 未記録", jobstatus.ErrNotFound)
	}
	return status, nil
}

func (s *fakeJobStatusStore) states() []domain.JobState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.JobState(nil), s.saved...)
}

func equalStates(t *testing.T, got []domain.JobState, want ...domain.JobState) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("states = %v, want %v", got, want)
		}
	}
}

func TestExecuteRecordsRunningThenSucceeded(t *testing.T) {
	store := newFakeJobStatusStore()
	runner := newStatusTestRunner(t, store)

	if err := runner.Execute(context.Background(), statusTestTask()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	equalStates(t, store.states(), domain.JobStateRunning, domain.JobStateSucceeded)
	final := store.statuses[statusTestJobID]
	if final.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", final.Attempts)
	}
	if final.OutputDir == "" {
		t.Error("OutputDir が記録されていない")
	}
}

func TestExecuteRecordsFailureWithReason(t *testing.T) {
	store := newFakeJobStatusStore()
	runner := newStatusTestRunner(t, store, failingStep{})

	if err := runner.Execute(context.Background(), statusTestTask()); err == nil {
		t.Fatal("Execute() error = nil, want error")
	}

	equalStates(t, store.states(), domain.JobStateRunning, domain.JobStateFailed)
	if store.statuses[statusTestJobID].Error == "" {
		t.Error("失敗理由が記録されていない")
	}
}

// Cloud Tasks の再配信で、完了済みジョブの生成をやり直さないこと。
func TestExecuteSkipsAlreadySucceededJob(t *testing.T) {
	store := newFakeJobStatusStore()
	counter := &countingStep{}
	runner := newStatusTestRunner(t, store, counter)

	task := statusTestTask()
	if err := runner.Execute(context.Background(), task); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if err := runner.Execute(context.Background(), task); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if counter.calls != 1 {
		t.Fatalf("step calls = %d, want 1 (完了済みジョブが再実行されている)", counter.calls)
	}
}

// 失敗したジョブは再配信で作り直せること（ガードが効きすぎないこと）。
func TestExecuteRetriesFailedJob(t *testing.T) {
	store := newFakeJobStatusStore()

	failing := newStatusTestRunner(t, store, failingStep{})
	if err := failing.Execute(context.Background(), statusTestTask()); err == nil {
		t.Fatal("Execute() error = nil, want error")
	}

	ok := newStatusTestRunner(t, store)
	if err := ok.Execute(context.Background(), statusTestTask()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	final := store.statuses[statusTestJobID]
	if final.State != domain.JobStateSucceeded {
		t.Fatalf("State = %q, want succeeded", final.State)
	}
	if final.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2 (試行回数が引き継がれていない)", final.Attempts)
	}
}

// ストア未設定でもパイプラインは通常どおり動作すること。
func TestExecuteWorksWithoutStatusStore(t *testing.T) {
	counter := &countingStep{}
	runner := newStatusTestRunner(t, nil, counter)

	if err := runner.Execute(context.Background(), statusTestTask()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if counter.calls != 1 {
		t.Errorf("step calls = %d, want 1", counter.calls)
	}
}

// statusTestJobID はジョブ状態テストで使う固定のジョブ ID です。
const statusTestJobID = "c20260726-120000-abcd1234"

func statusTestTask() domain.Task {
	return domain.Task{
		Command:    domain.TaskCommandComposeComic,
		JobID:      statusTestJobID,
		SourceText: "元文章",
	}
}

// staticPlanner は与えたステップ列をそのまま返すテスト用 Planner です。
type staticPlanner struct{ steps []Step }

func (p staticPlanner) Plan(*domain.Task) ([]Step, error) { return p.steps, nil }

// newStatusTestRunner は、ステップ列とジョブ状態ストアだけを差し替えた Runner を返します。
// store に nil を渡すと状態記録が無効な構成になります。
func newStatusTestRunner(t *testing.T, jobStatus domain.JobStatusStore, steps ...Step) *Runner {
	t.Helper()

	store := newMemStore()
	deps := Dependencies{
		Ops:     completeOps(),
		Reader:  store,
		Writer:  store,
		Bucket:  "bucket",
		Planner: staticPlanner{steps: steps},
	}
	if jobStatus != nil {
		deps.JobStatus = jobStatus
	}
	r, err := New(deps)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return r
}

type countingStep struct{ calls int }

func (countingStep) Name() string { return "counting" }

func (s *countingStep) Execute(context.Context, *Context) error {
	s.calls++
	return nil
}

type failingStep struct{}

func (failingStep) Name() string                            { return "failing" }
func (failingStep) Execute(context.Context, *Context) error { return errors.New("boom") }
