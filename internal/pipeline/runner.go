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
	// Layout は画像の比率と解像度です。キットは設定として持たず呼び出しごとに受け取るため、
	// 既定の出どころはアプリ側になります。ゼロ値ならキット既定（3:4 / 1K / 2K）です。
	Layout ImageLayout
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
// 終端状態（succeeded / failed）の記録と通知は recordOutcome が一手に行います。
func (r *Runner) Execute(ctx context.Context, task domain.Task) error {
	// 以降このジョブから出るログすべてに job_id / command を載せ、
	// 各ステップのログを 1 ジョブ単位で追えるようにする。
	ctx = slogctx.With(ctx,
		slog.String("job_id", task.JobID),
		slog.String("command", string(task.Command)),
	)

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

	// 入力検証より前に記録する。全試行が Attempts に載り、どのジョブも必ず
	// running を経由して終端に至る。OutputDir は検証に依存しないので先に解決する。
	outputDir, dirErr := domain.JobOutputDir(r.deps.Bucket, task.JobID)
	if dirErr == nil {
		status.markRunning(ctx, &task, outputDir)
	}

	title, err := r.run(ctx, &task)
	return r.recordOutcome(ctx, &task, status, title, err)
}

// Run はタスクのコマンドに応じたステップ列を順に実行し、成果物だけを残します。
//
// 状態の記録・通知・再配信ガードは行いません。それらは Cloud Tasks worker の入口である
// Execute の担当で、Run は生成そのものを同期実行して結果を確かめたい場合の本体メソッドです。
func (r *Runner) Run(ctx context.Context, task *domain.Task) error {
	_, err := r.run(ctx, task)
	return err
}

// run はステップ列の実行本体です。成功時は記録用のタイトルを返します。
//
// 実行時間の上限は、画像生成が応答しなくなった場合に Cloud Run のインスタンスを
// 占有し続けないためのものです。締切はステップの実行にだけ被せ、呼び出し元の ctx には
// 影響させません。ctx を上書きすると、打ち切られた直後の終端記録まで期限切れの
// context で行うことになり、いちばん記録が要る場面で残りません。
func (r *Runner) run(ctx context.Context, task *domain.Task) (string, error) {
	runCtx := ctx
	if r.deps.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, r.deps.Timeout)
		defer cancel()
	}

	if err := task.ValidateSubmission(); err != nil {
		return "", fmt.Errorf("invalid task: %w", err)
	}

	outputDir, err := domain.JobOutputDir(r.deps.Bucket, task.JobID)
	if err != nil {
		return "", fmt.Errorf("resolve output dir: %w", err)
	}
	designJobOutputDir, err := domain.DesignJobOutputDir(r.deps.Bucket, task.JobID)
	if err != nil {
		return "", fmt.Errorf("resolve design job output dir: %w", err)
	}

	pc := &Context{
		Task:               task,
		OutputDir:          outputDir,
		DesignJobOutputDir: designJobOutputDir,
		CharacterOutputDir: domain.CharacterAssetDir(r.deps.Bucket),
		Ops:                r.deps.Ops,
		Reader:             r.deps.Reader,
		Writer:             r.deps.Writer,
		Layout:             r.deps.Layout,
	}

	steps, err := r.deps.Planner.Plan(task)
	if err != nil {
		return "", err
	}
	for _, step := range steps {
		if stepErr := step.Execute(runCtx, pc); stepErr != nil {
			r.savePartialResults(runCtx, pc)
			return "", fmt.Errorf("step %s: %w", step.Name(), stepErr)
		}
	}

	// 台本生成済みならタイトルが埋まっている。UI が一覧で表示するために記録する。
	if pc.Manga != nil {
		return pc.Manga.Title, nil
	}
	return "", nil
}

// recordOutcome は終端状態の記録と通知を、成功・失敗の別なく同じ経路で行います。
//
// 記録も通知も呼び出し元の context から切り離して行います。理由は savePartialResults と
// 同じで、打ち切りこそが終端の理由である場面（PIPELINE_TIMEOUT の発火、Cloud Tasks の
// dispatch deadline 超過によるリクエストのキャンセル）では ctx は既に期限切れだからです。
// そのまま使うと状態は running のまま固着し、story-queue は max_attempts = 1 なので
// 再試行も来ません。記録失敗は Recorder が握り潰すため、ジョブが黙って消えたように
// 見えます。これは失敗だけでなく、期限や切断と前後して完了したジョブの「成功」の
// 記録にもそのまま当てはまります。
func (r *Runner) recordOutcome(
	ctx context.Context,
	task *domain.Task,
	status statusRecorder,
	title string,
	cause error,
) error {
	outcomeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), outcomeReportTimeout)
	defer cancel()

	if cause != nil {
		status.markFailed(outcomeCtx, task, cause)
		if notifyErr := r.deps.Notifier.NotifyError(outcomeCtx, *task, cause); notifyErr != nil {
			slog.ErrorContext(ctx, "failed to send error notification",
				"job_id", task.JobID, "original_error", cause, "notification_error", notifyErr)
		}
		return cause
	}

	// 記録 → 通知の順。記録できていないジョブの成功を人に知らせない。
	status.markSucceeded(outcomeCtx, task, title)
	if notifyErr := r.deps.Notifier.NotifyComplete(outcomeCtx, *task); notifyErr != nil {
		slog.WarnContext(ctx, "success notification failed", "job_id", task.JobID, "error", notifyErr)
	}
	return nil
}

// partialSaveTimeout は、失敗時の保存に許す時間です。
// 呼び出し元の context から切り離して使うため、短く区切ります。
const partialSaveTimeout = 30 * time.Second

// outcomeReportTimeout は、終端状態（succeeded / failed）の記録と通知に許す時間です。
// partialSaveTimeout と同じく、呼び出し元の context から切り離して使います。
const outcomeReportTimeout = 30 * time.Second

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
