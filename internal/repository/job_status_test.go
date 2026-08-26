package repository

import (
	"context"
	"testing"

	"github.com/shouni/go-job-kit/jobstatus"

	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
)

// 保存形式そのもの（未記録・破損 JSON・パストラバーサル・上書き）の検証は
// go-job-kit の jobstatus 側にあります。ここは ap-story 固有の点だけを見ます。

const statusTestJobID = "c20260726-120000-abcd1234"

func newStatusRepo(store *memStore) *jobstatus.Store[domain.JobStatus] {
	return NewJobStatusRepository(config.StorageConfig{GCSBucket: "bucket"}, store, store)
}

// 状態は成果物と同じ comics/{jobID}/ 配下に置き、履歴削除（プレフィックス一括削除）で
// 自動的に片付くようにする。履歴一覧は comic_state.json だけを拾うため混ざらない。
func TestSaveWritesInsideJobDirectory(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	repo := newStatusRepo(store)

	err := repo.Save(context.Background(), statusTestJobID, domain.JobStatus{
		JobID:   statusTestJobID,
		Command: "compose_comic",
		State:   domain.JobStateQueued,
	})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	want := "gs://bucket/comics/" + statusTestJobID + "/status.json"
	if _, ok := store.files[want]; !ok {
		t.Fatalf("status.json が %q に書かれていない。書き込み済み: %v", want, keysOf(store))
	}
}

// ap-story 固有フィールド（OutputDir）が往復すること。
// jobstatus.Status の埋め込みで JSON がフラットに保たれることの確認を兼ねます。
func TestSaveAndGetRoundTrip(t *testing.T) {
	t.Parallel()

	repo := newStatusRepo(newMemStore())
	original := domain.JobStatus{
		JobID:     statusTestJobID,
		Command:   "compose_comic",
		State:     domain.JobStateSucceeded,
		Title:     "テスト作品",
		Attempts:  3,
		OutputDir: "gs://bucket/comics/" + statusTestJobID,
	}
	if err := repo.Save(context.Background(), original.JobID, original); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := repo.Get(context.Background(), statusTestJobID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.State != domain.JobStateSucceeded {
		t.Errorf("State = %q", got.State)
	}
	if got.Title != "テスト作品" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", got.Attempts)
	}
	if got.OutputDir != original.OutputDir {
		t.Errorf("OutputDir = %q", got.OutputDir)
	}
	// Save 側で必ず打刻されること（呼び出し側が設定し忘れても記録が残るように）。
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt が設定されていない")
	}
}

// keysOf は memStore に書き込まれたパス一覧を返します。
func keysOf(store *memStore) []string {
	store.mu.Lock()
	defer store.mu.Unlock()

	paths := make([]string, 0, len(store.files))
	for path := range store.files {
		paths = append(paths, path)
	}
	return paths
}
