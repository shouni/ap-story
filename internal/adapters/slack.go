package adapters

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-notify/notify"
	"github.com/shouni/go-notify/slack"

	"github.com/shouni/ap-story/internal/domain"
)

// slackSuccessTitle はコマンドに応じた完了通知のタイトルを返します。
func slackSuccessTitle(command domain.TaskCommand) string {
	switch command {
	case domain.TaskCommandGenerateDesignSheet:
		return "🎨 デザインシート生成が完了しました"
	case domain.TaskCommandRegenerateChapterScript, domain.TaskCommandRegeneratePanel, domain.TaskCommandRegeneratePage:
		return "🔁 再生成ジョブが完了しました"
	default:
		return "🎬 漫画生成ジョブが完了しました"
	}
}

// slackErrorTitle はコマンドに応じた失敗通知のタイトルを返します。
func slackErrorTitle(command domain.TaskCommand) string {
	if command == domain.TaskCommandGenerateDesignSheet {
		return "❌ デザインシート生成でエラーが発生しました"
	}
	return "❌ 漫画生成ジョブでエラーが発生しました"
}

// SlackAdapter は Slack Incoming Webhook 経由でジョブ完了・失敗を通知するアダプターです。
// domain.Notifier を実装します。
type SlackAdapter struct {
	pipeline   *notify.Pipeline
	serviceURL string
}

var _ domain.Notifier = (*SlackAdapter)(nil)

// NewSlackAdapter は Slack 通知アダプターを構築します。webhookURL が空文字の場合は
// 通知が無効化された（呼び出しても常に成功しスキップする）アダプターを返します。
func NewSlackAdapter(httpClient httpkit.Poster, webhookURL, serviceURL string) (*SlackAdapter, error) {
	notifier, err := slack.NewNotifier(httpClient, webhookURL)
	if err != nil {
		return nil, fmt.Errorf("slack クライアントの初期化に失敗しました: %w", err)
	}

	return &SlackAdapter{
		// 見出しはコマンドごとに変わるため、ここでは設定せず
		// 送信時に WithTitles で差し替えます。
		pipeline:   notify.NewPipeline(notifier, notify.Titles{}),
		serviceURL: serviceURL,
	}, nil
}

// NotifyComplete はジョブ完了を Slack に通知します。
func (s *SlackAdapter) NotifyComplete(ctx context.Context, task domain.Task) error {
	if !s.pipeline.Enabled() {
		slog.InfoContext(ctx, "Slack通知が無効化されているためスキップします", "job_id", task.JobID)
		return nil
	}

	err := s.pipeline.
		WithTitles(notify.Titles{Success: slackSuccessTitle(task.Command)}).
		Success(ctx, s.buildContent(task))
	if err != nil {
		return fmt.Errorf("slackへの完了通知に失敗しました: %w", err)
	}

	slog.Info("Slack に完了通知を送信しました", "job_id", task.JobID, "command", task.Command)
	return nil
}

// NotifyError はジョブ失敗を、原因（cause）とともに Slack に通知します。
func (s *SlackAdapter) NotifyError(ctx context.Context, task domain.Task, cause error) error {
	if !s.pipeline.Enabled() {
		slog.InfoContext(ctx, "Slack通知が無効化されているためスキップします", "job_id", task.JobID, "error", cause)
		return nil
	}

	// エラー内容は Pipeline が本文の末尾に追記します。
	err := s.pipeline.
		WithTitles(notify.Titles{Failure: slackErrorTitle(task.Command)}).
		Failure(ctx, s.buildContent(task), cause)
	if err != nil {
		return fmt.Errorf("slackへのエラー通知に失敗しました: %w", err)
	}

	slog.Info("Slack にエラー通知を送信しました", "job_id", task.JobID, "error", cause)
	return nil
}

// buildContent はコマンド・ジョブID・結果確認リンク・入力ソースを含む通知本文を組み立てます。
// 値が空の項目は notify.Body が行ごと省くため、ここでの存在チェックは不要です。
func (s *SlackAdapter) buildContent(task domain.Task) *notify.Body {
	body := notify.NewBody().
		Code("Command", string(task.Command)).
		Code("Job ID", task.JobID)

	if resultURL, label := s.resultPageURL(task); resultURL != "" {
		body.Link(label, resultURL, resultURL)
	}

	return body.Field("Source", task.SourceURL)
}

// resultPageURL は、ジョブの結果を確認できる Web 画面の URL とリンクラベルを返します。
// 漫画生成・再生成は作品詳細画面（/jobs/{jobID}）、デザインシート単体生成は
// state が comics/ に存在しないため、生成結果が表示されるキャラクターページ
// （/characters/{characterID}、複数キャラクター合成時は一覧）へ誘導します。
func (s *SlackAdapter) resultPageURL(task domain.Task) (string, string) {
	if s.serviceURL == "" || task.JobID == "" {
		return "", ""
	}

	if task.Command == domain.TaskCommandGenerateDesignSheet {
		pagePath := "/characters"
		if len(task.CharacterIDs) == 1 {
			pagePath = "/characters/" + task.CharacterIDs[0]
		}
		if resultURL := notify.JoinURL(s.serviceURL, pagePath); resultURL != "" {
			return resultURL, "Character Page"
		}
		return "", ""
	}

	if resultURL := notify.JoinURL(s.serviceURL, "/jobs", task.JobID); resultURL != "" {
		return resultURL, "History Detail"
	}
	return "", ""
}
