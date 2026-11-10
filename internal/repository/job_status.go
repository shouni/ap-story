package repository

import (
	"github.com/shouni/go-job-kit/jobstatus"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
)

var _ domain.JobStatusStore = (*jobstatus.Store[domain.JobStatus])(nil)

// NewJobStatusRepository builds GCS-backed reads and writes of job progress.
//
// The storage format, job ID normalisation and cache suppression (no-store) are go-job-kit's jobstatus's job.
// All this provides is the placement: "under comics/{jobID}/, with the artifacts".
//
// domain.JobStatusStore has the same signature as jobstatus.Store, so no wrapping type is needed.
// The status file always holds only the latest generation and is updated by overwriting.
func NewJobStatusRepository(
	storage config.StorageConfig,
	store remoteio.Store,
) *jobstatus.Store[domain.JobStatus] {
	bucket := storage.GCSBucket
	locate := func(jobID string) (string, error) {
		relPath, err := domain.JobStatusPath(jobID)
		if err != nil {
			return "", err
		}
		return remoteio.BuildURI(remoteio.SchemeGCS, bucket, relPath), nil
	}

	return jobstatus.NewStore[domain.JobStatus](store, locate)
}
