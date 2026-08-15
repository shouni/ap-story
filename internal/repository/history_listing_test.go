package repository

import (
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/config"
)

// memStore は remoteio.InputReader / remoteio.OutputWriter を満たすインメモリ fake です。
// buildHistoriesConcurrently が並行して Open/GetState を呼ぶため、mu で保護します。
type memStore struct {
	mu      sync.Mutex
	files   map[string][]byte
	deleted []string
	// opens は Open されたパスの記録です（読み込み回数のアサーション用）。
	opens []string
	// lists は List されたプレフィックスの記録です（一覧走査回数のアサーション用）。
	lists []string
}

func newMemStore() *memStore { return &memStore{files: map[string][]byte{}} }

func (m *memStore) Open(_ context.Context, path string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.opens = append(m.opens, path)
	data, ok := m.files[path]
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// List は GCS の一覧を模倣します。
//
// 区切り文字の扱いまで写しているのは、本番の一覧が WithDelimiter に乗っているためです。
// フェイクが区切り文字を無視して全件返すと、疑似ディレクトリの組み立てが素通りしてしまい、
// 一覧のテストが何も確かめないものになります。
func (m *memStore) List(_ context.Context, prefix string, callback func(path string) error, opts ...remoteio.ListOption) error {
	settings := remoteio.NewListSettings(opts...)

	m.mu.Lock()
	m.lists = append(m.lists, prefix)
	prefix = remoteio.ListPrefix(prefix, settings)
	paths := make([]string, 0, len(m.files))
	for path := range m.files {
		if strings.HasPrefix(path, prefix) {
			paths = append(paths, path)
		}
	}
	m.mu.Unlock()

	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		entry := path
		if settings.Delimiter != "" {
			rest := strings.TrimPrefix(path, prefix)
			if idx := strings.Index(rest, settings.Delimiter); idx >= 0 {
				// 区切り文字より先は疑似ディレクトリへ畳まれます。
				entry = prefix + rest[:idx] + settings.Delimiter
			}
		}
		if seen[entry] {
			continue
		}
		seen[entry] = true
		if err := callback(entry); err != nil {
			return err
		}
	}
	return nil
}

func (m *memStore) Exists(_ context.Context, path string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.files[path]
	return ok, nil
}

func (m *memStore) Write(_ context.Context, path string, r io.Reader, _ ...remoteio.WriteOption) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path] = data
	return nil
}

func (m *memStore) Delete(_ context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleted = append(m.deleted, path)
	for p := range m.files {
		if strings.HasPrefix(p, path) {
			delete(m.files, p)
		}
	}
	return nil
}

func newTestRepository(store *memStore) *ComicRepository {
	return NewComicRepository(config.StorageConfig{GCSBucket: "test-bucket"}, store, store, NewHistoryCache())
}

func putState(store *memStore, jobID, body string) {
	store.files["gs://test-bucket/comics/"+jobID+"/comic_state.json"] = []byte(body)
}

func TestListHistoryPageBuildsSummariesFromState(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	putState(store, "job-1", `{"version":1,"id":"job-1","title":"夜明けのデプロイ","chapters":[{"id":"ch01"}],"panels":[{"id":"p1"},{"id":"p2"}]}`)
	// ジョブ配下の成果物は疑似ディレクトリへ畳まれ、ジョブが二重に数えられない
	store.files["gs://test-bucket/comics/job-1/images/panel_1.png"] = []byte("binary")

	repo := newTestRepository(store)
	page, err := repo.ListHistoryPage(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("ListHistoryPage failed: %v", err)
	}

	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("page = %+v, want 1 item", page)
	}
	item := page.Items[0]
	if item.JobID != "job-1" || item.Title != "夜明けのデプロイ" || item.ChapterCount != 1 || item.PanelCount != 2 {
		t.Errorf("job-1 summary = %+v, unexpected", item)
	}
}

func TestListHistoryPageExcludesLegacyJobsWithoutChapters(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	// 旧形式では単体生成ジョブの state も comics/ に保存されていた。
	// 現行形式（design-jobs/）では列挙自体に現れないが、旧データが残っていても
	// 章立ての有無で表示から除外される。
	putState(store, "job-design-sheet-only", `{"version":1,"id":"job-design-sheet-only","title":"単体生成","panels":[]}`)

	repo := newTestRepository(store)
	page, err := repo.ListHistoryPage(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("ListHistoryPage failed: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("Items = %+v, want empty (legacy design-only job has no chapters)", page.Items)
	}
}

func TestListHistoryPageIgnoresDesignJobsPrefix(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	putState(store, "job-comic", `{"version":1,"id":"job-comic","title":"作品","chapters":[{"id":"ch01"}],"panels":[]}`)
	// 現行形式の単体生成ジョブ state は design-jobs/ 配下のため列挙対象外
	store.files["gs://test-bucket/design-jobs/job-design/comic_state.json"] = []byte(
		`{"version":1,"id":"job-design","title":"単体生成","panels":[]}`)

	repo := newTestRepository(store)
	page, err := repo.ListHistoryPage(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("ListHistoryPage failed: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].JobID != "job-comic" {
		t.Errorf("page = %+v, want only job-comic", page)
	}
}

func TestListHistoryPageLoadsOnlySelectedPageStates(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	for _, id := range []string{"job-1", "job-2", "job-3", "job-4", "job-5"} {
		putState(store, id, `{"version":1,"id":"`+id+`","title":"t","chapters":[{"id":"ch01"}],"panels":[]}`)
	}

	repo := newTestRepository(store)
	store.opens = nil
	page, err := repo.ListHistoryPage(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("ListHistoryPage failed: %v", err)
	}
	if len(page.Items) != 2 || page.Total != 5 {
		t.Fatalf("page = %+v, want 2 items of total 5", page)
	}
	// ページングは ID の列挙だけで行い、state を読むのは選択されたページの分だけ
	if len(store.opens) != 2 {
		t.Errorf("opened %d state files %v, want 2 (only the selected page)", len(store.opens), store.opens)
	}
}

func TestListHistoryPageExcludesUnreadableState(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	store.files["gs://test-bucket/comics/job-broken/comic_state.json"] = []byte(`not json`)

	repo := newTestRepository(store)
	page, err := repo.ListHistoryPage(context.Background(), 1, 20)
	if err != nil {
		t.Fatalf("ListHistoryPage failed: %v", err)
	}
	// 読み込み失敗時のフォールバック値は ChapterCount == 0 のため、/history のフィルタで除外される。
	if len(page.Items) != 0 {
		t.Errorf("Items = %+v, want empty (unreadable state falls back to ChapterCount 0)", page.Items)
	}
}

func TestGetStateRoundTrip(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	putState(store, "job-1", `{"version":1,"id":"job-1","title":"夜明けのデプロイ","panels":[]}`)
	repo := newTestRepository(store)

	state, err := repo.GetState(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("GetState failed: %v", err)
	}
	if state.Title != "夜明けのデプロイ" {
		t.Errorf("Title = %q, want 夜明けのデプロイ", state.Title)
	}
}

func TestGetStateRejectsInvalidJobID(t *testing.T) {
	t.Parallel()

	repo := newTestRepository(newMemStore())
	if _, err := repo.GetState(context.Background(), "../escape"); err == nil {
		t.Error("GetState with invalid job id succeeded, want error")
	}
}

func TestGetStateNotFound(t *testing.T) {
	t.Parallel()

	repo := newTestRepository(newMemStore())
	if _, err := repo.GetState(context.Background(), "missing-job"); err == nil {
		t.Error("GetState for missing job succeeded, want error")
	}
}

func TestDeleteHistoryRemovesStateAndCache(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	putState(store, "job-1", `{"version":1,"id":"job-1","title":"t","panels":[]}`)
	repo := newTestRepository(store)

	// キャッシュに載せてから削除し、キャッシュも消えることを確認する
	if _, err := repo.buildHistory(context.Background(), "job-1"); err != nil {
		t.Fatalf("buildHistory failed: %v", err)
	}
	if _, ok := repo.getCachedHistory("job-1"); !ok {
		t.Fatal("history was not cached as precondition")
	}

	if err := repo.DeleteHistory(context.Background(), "job-1"); err != nil {
		t.Fatalf("DeleteHistory failed: %v", err)
	}
	if _, ok := store.files["gs://test-bucket/comics/job-1/comic_state.json"]; ok {
		t.Error("state file was not deleted")
	}
	if _, ok := repo.getCachedHistory("job-1"); ok {
		t.Error("cache entry was not invalidated after delete")
	}
}

// countLists は指定プレフィックスへの List 回数を返します。
func (m *memStore) countLists(prefix string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, p := range m.lists {
		if p == prefix {
			n++
		}
	}
	return n
}

// 一覧は区切り文字付きの List に乗るため、1 ジョブが成果物の数だけ重複して現れず、
// ジョブの疑似ディレクトリ 1 件として集まること。1 作品でパネル・ページ画像が数十枚に
// なるため、ここが崩れると一覧走査がその倍数で効きます。
func TestListJobIDsFoldsJobArtifactsIntoOneEntry(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	putState(store, "job-1", `{"version":1,"id":"job-1","title":"作品","chapters":[{"id":"ch01"}],"panels":[]}`)
	store.files["gs://test-bucket/comics/job-1/images/panel_1.png"] = []byte("binary")
	store.files["gs://test-bucket/comics/job-1/images/comic_page_1.png"] = []byte("binary")
	putState(store, "job-2", `{"version":1,"id":"job-2","title":"作品2","chapters":[{"id":"ch01"}],"panels":[]}`)
	// comics/ 直下のオブジェクトはジョブディレクトリではない
	store.files["gs://test-bucket/comics/stray.json"] = []byte("{}")

	repo := newTestRepository(store)
	jobIDs, err := repo.listJobIDs(context.Background())
	if err != nil {
		t.Fatalf("listJobIDs() error = %v", err)
	}

	slices.Sort(jobIDs)
	want := []string{"job-1", "job-2"}
	if !slices.Equal(jobIDs, want) {
		t.Errorf("listJobIDs() = %v, want %v", jobIDs, want)
	}
}

// 履歴一覧はバケット全体の走査になるため、ジョブ ID 一覧はキャッシュされ、
// 2 回目以降のページ表示で List が再実行されないこと。
func TestListHistoryPageCachesJobIDList(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	putState(store, "c-20260726-120000-abcd12345678",
		`{"version":1,"title":"作品","chapters":[{"title":"第1話"}]}`)
	repo := newTestRepository(store)

	for range 3 {
		if _, err := repo.ListHistoryPage(context.Background(), 1, 10); err != nil {
			t.Fatalf("ListHistoryPage() error = %v", err)
		}
	}

	if got := store.countLists("gs://test-bucket/comics/"); got != 1 {
		t.Errorf("comics/ への List 回数 = %d, want 1（ジョブ ID 一覧がキャッシュされていません）", got)
	}
}

// 削除したジョブが TTL のあいだ一覧に残らないよう、DeleteHistory がジョブ ID 一覧の
// キャッシュを破棄すること。破棄されないと、サマリ本体だけ消えた行が一覧に現れます。
func TestDeleteHistoryInvalidatesJobIDList(t *testing.T) {
	t.Parallel()

	const jobID = "c-20260726-120000-abcd12345678"
	store := newMemStore()
	putState(store, jobID, `{"version":1,"title":"作品","chapters":[{"title":"第1話"}]}`)
	repo := newTestRepository(store)

	page, err := repo.ListHistoryPage(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("ListHistoryPage() error = %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(page.Items))
	}

	if err := repo.DeleteHistory(context.Background(), jobID); err != nil {
		t.Fatalf("DeleteHistory() error = %v", err)
	}

	page, err = repo.ListHistoryPage(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("ListHistoryPage() error = %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("削除後の len(items) = %d, want 0（一覧キャッシュが破棄されていません）", len(page.Items))
	}
	if got := store.countLists("gs://test-bucket/comics/"); got != 2 {
		t.Errorf("comics/ への List 回数 = %d, want 2（削除後に再走査されていません）", got)
	}
}
