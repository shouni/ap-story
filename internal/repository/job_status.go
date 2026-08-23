package repository

import (
	"github.com/shouni/go-job-kit/jobstatus"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
)

var _ domain.JobStatusStore = (*jobstatus.Store[domain.JobStatus])(nil)

// NewJobStatusRepository は、GCS を裏付けとしたジョブ進行状況の読み書きを構築します。
//
// 保存形式・ジョブ ID の正規化・キャッシュ抑止（no-store）は go-job-kit の jobstatus が
// 担います。ここが与えるのは「成果物と同じ comics/{jobID}/ 配下に置く」という配置だけです。
//
// domain.JobStatusStore は jobstatus.Store と同じシグネチャなので、包む型は要りません。
// 状態ファイルは常に最新の1世代だけを保持し、上書きで更新します。
func NewJobStatusRepository(
	storage config.StorageConfig,
	reader remoteio.InputReader,
	writer remoteio.OutputWriter,
) *jobstatus.Store[domain.JobStatus] {
	bucket := storage.GCSBucket
	locate := func(jobID string) (string, error) {
		relPath, err := domain.JobStatusPath(jobID)
		if err != nil {
			return "", err
		}
		return remoteio.BuildGCSURI(bucket, relPath), nil
	}

	return jobstatus.NewStore[domain.JobStatus](reader, writer, locate)
}
