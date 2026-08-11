package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-remote-io/remoteio"
	"github.com/shouni/go-utils/slogctx"

	"github.com/shouni/ap-story/internal/domain"
)

// Dependencies は Runner の実行に必要な依存の束です。New が必須依存の欠落を
// 生成時に検証するため、実行時まで依存不足が分からない状態を防ぎます。
type Dependencies struct {
	Ops    *ports.Operations
	Reader ports.ContentReader
	Writer remoteio.Writer
	// Bucket はジョブ成果物を格納する GCS バケット名です（gs:// は付けない）。
	Bucket string
	// Planner はコマンドごとの実行計画を決定します。
	// 未設定の場合はゼロ値の DefaultPlanner を使います（任意）。
	Planner Planner
	// Notifier はジョブ完了・失敗を通知します（任意）。未設定の場合は何もしません。
	Notifier domain.Notifier
	// Timeout はタスク1件の実行時間の上限です。0 以下は無制限を意味します（任意）。
	Timeout time.Duration
	// JobStatus はジョブ進行状況の記録先です。未設定の場合は状態記録と
	// 再実行ガードが無効になります（任意）。
	JobStatus domain.JobStatusStore
}

// noopNotifier は Notifier 未設定時に使う何もしない実装です。
type noopNotifier struct{}

func (noopNotifier) NotifyComplete(context.Context, domain.Task) error     { return nil }
func (noopNotifier) NotifyError(context.Context, domain.Task, error) error { return nil }

// Runner は domain.Task をコマンドに応じたステップ列へ順番に流す Worker パイプラインです。
type Runner struct {
	deps Dependencies
}

// New は依存を検証して Runner を生成します。必須依存が欠けている場合はエラーを返し、
// 実行時ではなく起動時に構成ミスを検出します。
func New(deps Dependencies) (*Runner, error) {
	// 一括生成（Ops.Panel.GenerateAllPanels / Ops.Page.ComposeAllPages）はステップから直接呼ぶため、欠けていれば
	// ジョブ実行中ではなく起動時に落とす。workflow.New は常に設定するので、ここに
	// 引っかかるのは Operations を手組みしたときだけ。
	var ops = deps.Ops
	var missing []string
	for _, dep := range []struct {
		name string
		ok   bool
	}{
		{"Ops", ops != nil},
		{"Ops.Panel", ops != nil && ops.Panel != nil},
		{"Ops.Page", ops != nil && ops.Page != nil},
		{"Reader", deps.Reader != nil},
		{"Writer", deps.Writer != nil},
		{"Bucket", deps.Bucket != ""},
	} {
		if !dep.ok {
			missing = append(missing, dep.name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("pipeline runner missing required dependencies: %s", strings.Join(missing, ", "))
	}
	if deps.Planner == nil {
		deps.Planner = DefaultPlanner{}
	}
	if deps.Notifier == nil {
		deps.Notifier = noopNotifier{}
	}
	return &Runner{deps: deps}, nil
}

// Execute は gcp-kit/worker.TaskExecutor に適合するためのエントリーポイントです。
func (r *Runner) Execute(ctx context.Context, task domain.Task) error {
	// 以降このジョブから出るログすべてに job_id / command を載せ、
	// 各ステップのログを 1 ジョブ単位で追えるようにする。
	ctx = slogctx.With(ctx,
		slog.String("job_id", task.JobID),
		slog.String("command", string(task.Command)),
	)

	// 画像生成が応答しなくなった場合に Cloud Run のインスタンスを占有し続けないよう、
	// タスク1件に上限を設ける。打ち切られたタスクは Cloud Tasks の再試行で作り直せる。
	if r.deps.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.deps.Timeout)
		defer cancel()
	}

	// Cloud Tasks の再配信で完了済みジョブを作り直さないためのガード。
	// 通知の失敗などで一度エラーを返しただけでも再配信されるため、ここで打ち切らないと
	// 画像生成コストがそのまま二重に発生します。
	status := newStatusRecorder(r.deps.JobStatus)
	done, err := status.alreadySucceeded(ctx, task.JobID)
	if err != nil {
		// 状態を読めない。判断できないので再配信に委ねる。
		return err
	}
	if done {
		slog.InfoContext(ctx, "skipping already completed job")
		return nil
	}

	return r.run(ctx, &task, status)
}

// Run はタスクのコマンドに応じたステップ列を順に実行します。成功時は完了通知、
// 失敗時はエラー通知を送ります（通知自体の失敗はログに残すのみでジョブの成否には影響しません）。
func (r *Runner) Run(ctx context.Context, task *domain.Task) error {
	return r.run(ctx, task, newStatusRecorder(r.deps.JobStatus))
}

// run はステップ列の実行本体です。status が有効なら各段階の状態を記録します。
func (r *Runner) run(ctx context.Context, task *domain.Task, status statusRecorder) (err error) {
	// 記録も通知も呼び出し元の context から切り離して行います。理由は
	// savePartialResults と同じで、打ち切りこそが失敗理由である場面
	// （PIPELINE_TIMEOUT の発火、Cloud Tasks の dispatch deadline 超過による
	// リクエストのキャンセル）では ctx は既に期限切れだからです。そのまま使うと
	// 状態は running のまま固着し、story-queue は max_attempts = 1 なので再試行も
	// 来ません。記録失敗は Recorder が握り潰すため、ジョブが黙って消えたように見えます。
	defer func() {
		if err != nil {
			reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), failureReportTimeout)
			defer cancel()

			status.markFailed(reportCtx, task, err)
		}
	}()
	defer func() {
		if err != nil {
			reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), failureReportTimeout)
			defer cancel()

			if notifyErr := r.deps.Notifier.NotifyError(reportCtx, *task, err); notifyErr != nil {
				slog.ErrorContext(ctx, "failed to send error notification",
					"job_id", task.JobID, "original_error", err, "notification_error", notifyErr)
			}
		}
	}()

	if err = task.ValidateSubmission(); err != nil {
		return fmt.Errorf("invalid task: %w", err)
	}

	outputDir, err := domain.JobOutputDir(r.deps.Bucket, task.JobID)
	if err != nil {
		return fmt.Errorf("resolve output dir: %w", err)
	}
	designJobOutputDir, err := domain.DesignJobOutputDir(r.deps.Bucket, task.JobID)
	if err != nil {
		return fmt.Errorf("resolve design job output dir: %w", err)
	}

	status.markRunning(ctx, task, outputDir)

	pc := &Context{
		State: State{
			Task:               task,
			OutputDir:          outputDir,
			DesignJobOutputDir: designJobOutputDir,
			CharacterOutputDir: domain.CharacterAssetDir(r.deps.Bucket),
		},
		Services: Services{
			Ops:    r.deps.Ops,
			Reader: r.deps.Reader,
			Writer: r.deps.Writer,
		},
	}

	steps, planErr := r.deps.Planner.Plan(task)
	if planErr != nil {
		err = planErr
		return err
	}
	for _, step := range steps {
		if stepErr := step.Execute(ctx, pc); stepErr != nil {
			err = fmt.Errorf("step %s: %w", step.Name(), stepErr)
			r.savePartialResults(ctx, pc)
			return err
		}
	}

	// 台本生成済みならタイトルが埋まっている。UI が一覧で表示するために記録する。
	title := ""
	if pc.Manga != nil {
		title = pc.Manga.Title
	}
	status.markSucceeded(ctx, task, title)

	if notifyErr := r.deps.Notifier.NotifyComplete(ctx, *task); notifyErr != nil {
		slog.WarnContext(ctx, "success notification failed", "job_id", task.JobID, "error", notifyErr)
	}
	return nil
}

// partialSaveTimeout は、失敗時の保存に許す時間です。
// 呼び出し元の context から切り離して使うため、短く区切ります。
const partialSaveTimeout = 30 * time.Second

// failureReportTimeout は、失敗の記録と通知に許す時間です。
// partialSaveTimeout と同じく、呼び出し元の context から切り離して使います。
const failureReportTimeout = 30 * time.Second

// savePartialResults は、ステップが失敗した時点までの成果を保存します。
//
// go-comic-kit の一括生成は部分的な成功も state に記録して返すため、ここで保存して
// おかないと、生成済みの画像が state から参照されないまま GCS に取り残されます。
// Cloud Tasks の再試行でも最初から作り直しになり、画像生成のコストがそのまま二重に
// かかります。保存しておけば、再実行は未生成のコマ・ページだけで済みます。
//
// 保存は呼び出し元から切り離した context で行います。PIPELINE_TIMEOUT による打ち切りは
// まさに成果を残したい場面で、その ctx は既に期限切れだからです。保存自体の失敗は
// 警告に留め、本来の失敗理由を上書きしません。
func (r *Runner) savePartialResults(ctx context.Context, pc *Context) {
	if pc.Manga == nil {
		return
	}

	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), partialSaveTimeout)
	defer cancel()

	if err := (SaveStateStep{}).Execute(saveCtx, pc); err != nil {
		slog.WarnContext(ctx, "failed to save partial results", "error", err)
		return
	}
	slog.InfoContext(ctx, "saved partial results before reporting failure")
}
