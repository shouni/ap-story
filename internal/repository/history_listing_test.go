package repository

import (
	"context"
	"errors"
	"io"
	"iter"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/memio"

	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
)

// memStore は remoteio.InputReader / remoteio.OutputWriter を満たすインメモリ fake です。
// buildHistoriesConcurrently が並行して Open/GetState を呼ぶため、mu で保護します。
// memStore は memio を包んだストレージのフェイクです。
//
// 一覧の畳み込み・不在の返し方・削除の単位といったストレージの振る舞いは memio が
// 受け持ちます（本物のハンドラと同じ適合性スイートを通っています）。ここに残しているのは
// 「どこを開いたか」「どこを走査したか」という呼び出しの記録だけです。
//
// 以前はこのフェイクが Delete をプレフィックス一括削除として実装していました。
// 本物の Delete は単一オブジェクトなので、production では消えないものが
// テストでは消えていました。
type memStore struct {
	remoteio.Store
	h *memio.Handler

	mu      sync.Mutex
	deleted []string
	// opens は Open されたパスの記録です（読み込み回数のアサーション用）。
	opens []string
	// lists は List されたプレフィックスの記録です（一覧走査回数のアサーション用）。
	lists []string
}

func newMemStore() *memStore {
	m := &memStore{h: memio.New(memio.WithScheme(remoteio.SchemeGCS))}
	m.Store = remoteio.NewStore(m.h)
	return m
}

func (m *memStore) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	m.mu.Lock()
	m.opens = append(m.opens, name)
	m.mu.Unlock()
	return m.Store.Open(ctx, name)
}

func (m *memStore) List(ctx context.Context, name string, opts ...remoteio.ListOption) iter.Seq2[remoteio.Entry, error] {
	m.mu.Lock()
	m.lists = append(m.lists, name)
	m.mu.Unlock()
	return m.Store.List(ctx, name, opts...)
}

func (m *memStore) Delete(ctx context.Context, name string) error {
	m.mu.Lock()
	m.deleted = append(m.deleted, name)
	m.mu.Unlock()
	return m.Store.Delete(ctx, name)
}

// Sub をライブラリの Sub へ委譲します。埋め込みから昇格した Sub をそのまま使うと、
// スコープの土台が埋め込まれた Store になり、上の記録が素通しされます。
func (m *memStore) Sub(prefix string) remoteio.Store { return remoteio.Sub(m, prefix) }

// put は前提となるオブジェクトを置きます。
func (m *memStore) put(uri, body string) {
	if err := m.h.Seed(uri, []byte(body)); err != nil {
		panic(err)
	}
}

// get は保存されている内容を返します。
func (m *memStore) get(t *testing.T, uri string) []byte {
	t.Helper()
	data, err := remoteio.ReadAll(context.Background(), m.Store, uri)
	if err != nil {
		t.Fatalf("read(%s) error = %v", uri, err)
	}
	return data
}

// has は対象が保存されているかを返します。
func (m *memStore) has(uri string) bool {
	ok, err := m.Exists(context.Background(), uri)
	return err == nil && ok
}

func newTestRepository(store *memStore) *ComicRepository {
	return NewComicRepository(config.StorageConfig{GCSBucket: "test-bucket"}, store, NewHistoryCache())
}

func putState(store *memStore, jobID, body string) {
	store.put("gs://test-bucket/comics/"+jobID+"/comic_state.json", body)
}

func TestListHistoryPageBuildsSummariesFromState(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	putState(store, "job-1", `{"version":1,"id":"job-1","title":"夜明けのデプロイ","chapters":[{"id":"ch01"}],"panels":[{"id":"p1"},{"id":"p2"}]}`)
	// ジョブ配下の成果物は疑似ディレクトリへ畳まれ、ジョブが二重に数えられない
	store.put("gs://test-bucket/comics/job-1/images/panel_1.png", "binary")

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
	store.put("gs://test-bucket/design-jobs/job-design/comic_state.json", `{"version":1,"id":"job-design","title":"単体生成","panels":[]}`)

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
	store.put("gs://test-bucket/comics/job-broken/comic_state.json", `not json`)

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
	if store.has("gs://test-bucket/comics/job-1/comic_state.json") {
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
	store.put("gs://test-bucket/comics/job-1/images/panel_1.png", "binary")
	store.put("gs://test-bucket/comics/job-1/images/comic_page_1.png", "binary")
	putState(store, "job-2", `{"version":1,"id":"job-2","title":"作品2","chapters":[{"id":"ch01"}],"panels":[]}`)
	// comics/ 直下のオブジェクトはジョブディレクトリではない
	store.put("gs://test-bucket/comics/stray.json", "{}")

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

// state がまだ無いのか、あるのに読めないのかを区別して返すこと。
//
// 以前はどちらも「見つかりません」に潰し、原因のエラーも捨てていたため、
// 権限エラーも GCS 障害も画面には「作品がありません」と出て、ログにも理由が
// 残りませんでした。両者は取るべき判断が正反対（進んでよい／待つべき）です。
func TestGetStateSeparatesMissingFromUnreadable(t *testing.T) {
	t.Parallel()

	const jobID = "c20260718-000000-aaaa1111"

	t.Run("state がまだ無い", func(t *testing.T) {
		t.Parallel()

		_, err := newTestRepository(newMemStore()).GetState(context.Background(), jobID)
		if !errors.Is(err, domain.ErrStateNotFound) {
			t.Fatalf("GetState() error = %v, want ErrStateNotFound", err)
		}
		if errors.Is(err, domain.ErrStateUnavailable) {
			t.Error("未記録が「読めない」にも一致している")
		}
	})

	t.Run("あるのに読めない", func(t *testing.T) {
		t.Parallel()

		store := newMemStore()
		// 壊れた JSON は「存在するが読めない」側。未存在と同じ扱いにすると、
		// 破損に気づかないまま生成が終わったものとして扱われます。
		store.put("gs://test-bucket/comics/"+jobID+"/comic_state.json", "{ broken")

		_, err := newTestRepository(store).GetState(context.Background(), jobID)
		if !errors.Is(err, domain.ErrStateUnavailable) {
			t.Fatalf("GetState() error = %v, want ErrStateUnavailable", err)
		}
		if errors.Is(err, domain.ErrStateNotFound) {
			t.Error("読めないだけの state が「無い」にも一致している")
		}
		// 原因を捨てないこと。捨てると、なぜ読めないのかがどこにも残りません。
		if !strings.Contains(err.Error(), "パース") {
			t.Errorf("原因が失われている: %v", err)
		}
	})
}
