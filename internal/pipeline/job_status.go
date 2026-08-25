package pipeline

import (
	"context"

	"github.com/shouni/go-job-kit/jobstatus"

	"github.com/shouni/ap-story/internal/domain"
)

// statusRecorder はジョブ状態の記録を担当します。
//
// 再実行ガード・前回記録からの引き継ぎ・記録失敗を握り潰す振る舞いは jobstatus.Recorder に
// 集約しており、ここが与えるのは ap-story 固有の状態の組み立てだけです。
// 記録先が未設定でもパイプラインは動作し、記録だけが行われません。
type statusRecorder struct {
	recorder *jobstatus.Recorder[domain.JobStatus]
}

// newStatusRecorder は domain.JobStatusStore を裏付けとした statusRecorder を構築します。
// store が nil の場合、記録は行われません。
//
// domain.JobStatusStore は jobstatus.StatusStore と同じシグネチャなので、そのまま渡せます。
func newStatusRecorder(store domain.JobStatusStore) statusRecorder {
	if store == nil {
		return statusRecorder{recorder: jobstatus.NewRecorder[domain.JobStatus](nil)}
	}
	return statusRecorder{recorder: jobstatus.NewRecorder[domain.JobStatus](store)}
}

// alreadySucceeded は、そのジョブが既に完了しているかどうかを返します。
//
// 状態を読めなかった場合はエラーを返します。呼び出し側はそのままエラーを返して
// Cloud Tasks の再配信に委ねてください。ここで「未完了」に倒すと、完了済みジョブを
// 作り直してこのガードが防ぐはずのコストを発生させ、「完了済み」に倒すと未完了の
// ジョブがタスクごと ACK されて二度と実行されません。
func (s statusRecorder) alreadySucceeded(ctx context.Context, jobID string) (bool, error) {
	return s.recorder.AlreadySucceeded(ctx, jobID)
}

// markRunning は処理開始を記録し、試行回数を 1 つ進めます。
func (s statusRecorder) markRunning(ctx context.Context, task *domain.Task, outputDir string) {
	s.record(ctx, task, domain.JobStateRunning, func(next, _ *domain.JobStatus) {
		next.OutputDir = outputDir
		next.Attempts++
	})
}

// markSucceeded は成功を記録します。
func (s statusRecorder) markSucceeded(ctx context.Context, task *domain.Task, title string) {
	s.record(ctx, task, domain.JobStateSucceeded, func(next, _ *domain.JobStatus) {
		if title != "" {
			next.Title = title
		}
	})
}

// markFailed は失敗と理由を記録します。
func (s statusRecorder) markFailed(ctx context.Context, task *domain.Task, cause error) {
	if cause == nil {
		return
	}
	s.record(ctx, task, domain.JobStateFailed, func(next, _ *domain.JobStatus) {
		next.Error = cause.Error()
	})
}

// record は今回の記録ぶんの状態を組み立てて保存します。
//
// Attempts・QueuedAt・Title は Recorder が前回の記録から引き継ぎます。OutputDir は
// jobstatus.Status に無い ap-story 固有のフィールドなので、ここで引き継ぎます。
// apply は引き継ぎの後に呼ばれるので、markRunning のように今回の値で上書きできます。
func (s statusRecorder) record(
	ctx context.Context,
	task *domain.Task,
	state domain.JobState,
	apply func(next, prev *domain.JobStatus),
) {
	if task == nil {
		return
	}

	status := domain.JobStatus{
		JobID:   task.JobID,
		Command: string(task.Command),
		State:   state,
	}

	s.recorder.Record(ctx, task.JobID, status, func(next, prev *domain.JobStatus) {
		if prev != nil {
			next.OutputDir = prev.OutputDir
		}
		if apply != nil {
			apply(next, prev)
		}
	})
}
