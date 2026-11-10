// Package repository serves the history list, detail reads and deletion from the comic_state.json files on GCS.
package repository

import (
	"time"

	"github.com/shouni/go-comic-kit/asset"
	"github.com/shouni/go-job-kit/cache"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
)

const defaultHistoryCacheTTL = 10 * time.Minute

// ComicRepository reads and writes comic_state.json on GCS and provides the history list, details and deletion.
type ComicRepository struct {
	bucket string
	// store is the root store that takes full URIs. The artifact directories are assembled by
	// domain and also passed to go-comic-kit, so the scope is not narrowed here.
	store        remoteio.Store
	historyCache *cache.TTL[domain.ComicHistory]
	// jobIDCache caches the job IDs from a listing scan for a short time.
	// Unlike the cache of history summaries (historyCache), without it every opening of the history page
	// would run a List over all of comics/.
	jobIDCache *cache.IDList
}

var _ domain.ComicRepository = (*ComicRepository)(nil)

// NewHistoryCache creates the history-summary cache injected into ComicRepository.
// Collecting expired entries starts on creation. Stop it with Close.
func NewHistoryCache() *cache.TTL[domain.ComicHistory] {
	return cache.NewTTL[domain.ComicHistory](defaultHistoryCacheTTL)
}

// NewComicRepository builds a ComicRepository.
func NewComicRepository(
	storage config.StorageConfig,
	store remoteio.Store,
	historyCache *cache.TTL[domain.ComicHistory],
) *ComicRepository {
	return &ComicRepository{
		bucket:       storage.GCSBucket,
		store:        store,
		historyCache: historyCache,
		jobIDCache:   cache.NewIDList(cache.DefaultIDListTTL),
	}
}

// jobOutputDir returns the GCS URI holding the given job's artifacts.
func (r *ComicRepository) jobOutputDir(jobID string) (string, error) {
	return domain.JobOutputDir(r.bucket, jobID)
}

// stateObjectPath returns the GCS URI of the given job's comic_state.json.
func (r *ComicRepository) stateObjectPath(jobID string) (string, error) {
	outputDir, err := r.jobOutputDir(jobID)
	if err != nil {
		return "", err
	}
	return asset.StatePath(outputDir)
}

// Job ID normalisation for the cache key is done in cache.TTL.
func (r *ComicRepository) getCachedHistory(jobID string) (domain.ComicHistory, bool) {
	return r.historyCache.Get(jobID)
}

func (r *ComicRepository) setCachedHistory(jobID string, history domain.ComicHistory) {
	r.historyCache.Set(jobID, history)
}

func (r *ComicRepository) deleteCachedHistory(jobID string) {
	r.historyCache.Delete(jobID)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
