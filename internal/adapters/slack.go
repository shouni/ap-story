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

// slackSuccessTitle returns the completion notice's title for the command.
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

// slackErrorTitle returns the failure notice's title for the command.
func slackErrorTitle(command domain.TaskCommand) string {
	if command == domain.TaskCommandGenerateDesignSheet {
		return "❌ デザインシート生成でエラーが発生しました"
	}
	return "❌ 漫画生成ジョブでエラーが発生しました"
}

// SlackAdapter notifies job completion and failure through a Slack Incoming Webhook.
// It implements domain.Notifier.
type SlackAdapter struct {
	pipeline   *notify.Pipeline
	serviceURL string
}

var _ domain.Notifier = (*SlackAdapter)(nil)

// NewSlackAdapter builds the Slack notification adapter. When webhookURL is empty it returns
// an adapter with notices disabled (every call succeeds and is skipped).
func NewSlackAdapter(httpClient httpkit.Poster, webhookURL, serviceURL string) (*SlackAdapter, error) {
	notifier, err := slack.NewNotifier(httpClient, webhookURL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialise the Slack client: %w", err)
	}

	return &SlackAdapter{
		// The heading changes per command, so it is not set here;
		// it is replaced with WithTitles at send time.
		pipeline:   notify.NewPipeline(notifier, notify.Titles{}),
		serviceURL: serviceURL,
	}, nil
}

// NotifyComplete notifies Slack that a job completed.
func (s *SlackAdapter) NotifyComplete(ctx context.Context, task domain.Task) error {
	if !s.pipeline.Enabled() {
		slog.InfoContext(ctx, "skipping: Slack notifications are disabled", "job_id", task.JobID)
		return nil
	}

	err := s.pipeline.
		WithTitles(notify.Titles{Success: slackSuccessTitle(task.Command)}).
		Success(ctx, s.buildContent(task))
	if err != nil {
		return fmt.Errorf("failed to send the completion notice to Slack: %w", err)
	}

	slog.Info("sent the completion notice to Slack", "job_id", task.JobID, "command", task.Command)
	return nil
}

// NotifyError notifies Slack that a job failed, together with its cause.
func (s *SlackAdapter) NotifyError(ctx context.Context, task domain.Task, cause error) error {
	if !s.pipeline.Enabled() {
		slog.InfoContext(ctx, "skipping: Slack notifications are disabled", "job_id", task.JobID, "error", cause)
		return nil
	}

	// The pipeline appends the error to the end of the body.
	err := s.pipeline.
		WithTitles(notify.Titles{Failure: slackErrorTitle(task.Command)}).
		Failure(ctx, s.buildContent(task), cause)
	if err != nil {
		return fmt.Errorf("failed to send the error notice to Slack: %w", err)
	}

	slog.Info("sent the error notice to Slack", "job_id", task.JobID, "error", cause)
	return nil
}

// buildContent assembles the notice body with the command, the job ID, the link to the result and the input source.
// notify.Body drops lines whose value is empty, so no presence check is needed here.
func (s *SlackAdapter) buildContent(task domain.Task) *notify.Body {
	body := notify.NewBody().
		Code("Command", string(task.Command)).
		Code("Job ID", task.JobID)

	if resultURL, label := s.resultPageURL(task); resultURL != "" {
		body.Link(label, resultURL, resultURL)
	}

	return body.Field("Source", task.SourceURL)
}

// resultPageURL returns the URL and link label of the web page where the job's result can be seen.
// Comic generation and regeneration go to the work's detail page (/jobs/{jobID}). A standalone design sheet
// has no state in comics/, so it leads to the character page that shows the result
// (/characters/{characterID}, or the list when several characters were composed together).
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
