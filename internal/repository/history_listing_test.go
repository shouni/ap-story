package repository

import (
	"context"
	"errors"
	"io"
	"iter"
	"slices"
	"sync"
	"testing"

	kitports "github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-remote-io/remoteio/memio"

	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
)

// memStore satisfies remoteio.InputReader / remoteio.OutputWriter as an in-memory fake.
// buildHistoriesConcurrently calls Open/GetState concurrently, so it is protected by mu.
// memStore is a storage fake wrapping memio.
//
// Storage behaviour such as folding listings, how absence is returned and the unit of deletion belongs
// to memio (which passes the same conformance suite as the real handlers). All that is left here is the
// record of calls: "what was opened" and "what was scanned".
//
// This fake used to implement Delete as a prefix delete. The real Delete
// removes a single object, so things that would not be deleted in production
// were deleted in tests.
type memStore struct {
	remoteio.Store
	h *memio.Handler

	mu      sync.Mutex
	deleted []string
	// opens records the paths opened (for assertions on the read count).
	opens []string
	// lists records the prefixes listed (for assertions on the scan count).
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

// Sub delegates to the library's Sub. Using the Sub promoted from the embedding as is would make the
// scope's base the embedded Store, and the recording above would be bypassed.
func (m *memStore) Sub(prefix string) remoteio.Store { return remoteio.Sub(m, prefix) }

// put places a precondition object.
func (m *memStore) put(uri, body string) {
	if err := m.h.Seed(uri, []byte(body)); err != nil {
		panic(err)
	}
}

// get returns the stored content.
func (m *memStore) get(t *testing.T, uri string) []byte {
	t.Helper()
	data, err := remoteio.ReadAll(context.Background(), m.Store, uri)
	if err != nil {
		t.Fatalf("read(%s) error = %v", uri, err)
	}
	return data
}

// has reports whether the target is stored.
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
	// Artifacts under a job are folded into a pseudo-directory, so a job is not counted twice
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
	// In the legacy format, a standalone job's state was also saved in comics/.
	// In the current format (design-jobs/) it does not appear in the listing at all, but even if legacy
	// data remains, it is excluded from display by whether it has chapters.
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
	// A standalone job's state in the current format is under design-jobs/, so it is not listed
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
	// Paging uses only the ID listing; the state is read only for the selected page
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
	// The fallback value on a read failure has ChapterCount == 0, so the /history filter excludes it.
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
		t.Errorf("Title = %q, want %q", state.Title, "夜明けのデプロイ")
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

	// Put it in the cache, delete it, and check the cache is cleared too
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

// countLists returns the number of List calls on the given prefix.
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

// The listing uses a List with a delimiter, so a job does not appear once per artifact but
// gathers into one pseudo-directory. One work has dozens of panel and page images, so if this
// breaks, the listing scan costs that many times over.
func TestListJobIDsFoldsJobArtifactsIntoOneEntry(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	putState(store, "job-1", `{"version":1,"id":"job-1","title":"作品","chapters":[{"id":"ch01"}],"panels":[]}`)
	store.put("gs://test-bucket/comics/job-1/images/panel_1.png", "binary")
	store.put("gs://test-bucket/comics/job-1/images/comic_page_1.png", "binary")
	putState(store, "job-2", `{"version":1,"id":"job-2","title":"作品2","chapters":[{"id":"ch01"}],"panels":[]}`)
	// Objects directly under comics/ are not job directories
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

// The history list scans the whole bucket, so the job ID list is cached and List is not
// run again for the second and later page views.
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
		t.Errorf("List calls on comics/ = %d, want 1 (the job ID list is not cached)", got)
	}
}

// DeleteHistory discards the cached job ID list so that a deleted job does not stay in the list
// for the TTL. Otherwise a row whose summary alone is gone appears in the list.
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
		t.Errorf("len(items) after delete = %d, want 0 (the list cache was not discarded)", len(page.Items))
	}
	if got := store.countLists("gs://test-bucket/comics/"); got != 2 {
		t.Errorf("List calls on comics/ = %d, want 2 (not rescanned after the delete)", got)
	}
}

// It tells apart a state that does not exist yet from one that exists but cannot be read.
//
// Both used to be collapsed into "not found" with the causing error discarded, so a
// permission error and a GCS outage alike showed on screen as 「作品がありません」, with no
// reason in the logs. The two call for opposite decisions (go ahead / wait).
func TestGetStateSeparatesMissingFromUnreadable(t *testing.T) {
	t.Parallel()

	const jobID = "c20260718-000000-aaaa1111"

	t.Run("state does not exist yet", func(t *testing.T) {
		t.Parallel()

		_, err := newTestRepository(newMemStore()).GetState(context.Background(), jobID)
		if !errors.Is(err, domain.ErrStateNotFound) {
			t.Fatalf("GetState() error = %v, want ErrStateNotFound", err)
		}
		if errors.Is(err, domain.ErrStateUnavailable) {
			t.Error("an unrecorded state also matches \"unreadable\"")
		}
	})

	t.Run("exists but unreadable", func(t *testing.T) {
		t.Parallel()

		store := newMemStore()
		// Broken JSON is on the "exists but cannot be read" side. Treating it like a missing state would
		// treat the corruption as a finished generation without anyone noticing.
		store.put("gs://test-bucket/comics/"+jobID+"/comic_state.json", "{ broken")

		_, err := newTestRepository(store).GetState(context.Background(), jobID)
		if !errors.Is(err, domain.ErrStateUnavailable) {
			t.Fatalf("GetState() error = %v, want ErrStateUnavailable", err)
		}
		if errors.Is(err, domain.ErrStateNotFound) {
			t.Error("a merely unreadable state also matches \"missing\"")
		}
		// Keep the cause. Discarding it leaves no trace of why it cannot be read.
		// Check the sentinel go-comic-kit's store.Load wraps a parse failure in, not its wording.
		if !errors.Is(err, kitports.ErrInvalidRequest) {
			t.Errorf("the cause was lost: %v", err)
		}
	})
}
