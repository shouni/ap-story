package adapters

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shouni/go-notify/notify"

	"github.com/shouni/ap-story/internal/domain"
)

// recordingNotifier is a fake that records the notify.Message values sent.
// Converting to Slack markup is go-notify's job, so this only checks the heading and
// body that ap-story assembled.
type recordingNotifier struct {
	got []notify.Message
}

// Notify implements notify.Notifier and records the Message it receives.
func (r *recordingNotifier) Notify(_ context.Context, msg notify.Message) error {
	r.got = append(r.got, msg)
	return nil
}

// newTestAdapter assembles an adapter with the Notifier replaced.
// The heading is decided by WithTitles at send time, so empty Titles are enough here.
func newTestAdapter(n notify.Notifier, serviceURL string) *SlackAdapter {
	return &SlackAdapter{
		pipeline:   notify.NewPipeline(n, notify.Titles{}),
		serviceURL: serviceURL,
	}
}

// last returns the last Message sent.
func (r *recordingNotifier) last(t *testing.T) notify.Message {
	t.Helper()
	if len(r.got) == 0 {
		t.Fatal("no notice was sent")
	}
	return r.got[len(r.got)-1]
}

func TestNewSlackAdapterDisabledWhenWebhookURLEmpty(t *testing.T) {
	t.Parallel()

	adapter, err := NewSlackAdapter(nil, "", "https://example.com")
	if err != nil {
		t.Fatalf("NewSlackAdapter failed: %v", err)
	}
	if adapter.pipeline.Enabled() {
		t.Fatal("expected notifier to be disabled when webhook URL is empty")
	}

	if err := adapter.NotifyComplete(context.Background(), domain.Task{JobID: "job-1"}); err != nil {
		t.Errorf("NotifyComplete on disabled adapter returned error: %v", err)
	}
	if err := adapter.NotifyError(context.Background(), domain.Task{JobID: "job-1"}, errors.New("boom")); err != nil {
		t.Errorf("NotifyError on disabled adapter returned error: %v", err)
	}
}

func TestNewSlackAdapterRequiresHTTPClientWhenWebhookSet(t *testing.T) {
	t.Parallel()

	if _, err := NewSlackAdapter(nil, "https://hooks.slack.example/webhook", "https://example.com"); err == nil {
		t.Fatal("expected error when http client is nil but webhook URL is set")
	}
}

func TestSlackContentIncludesCommandAndJobID(t *testing.T) {
	t.Parallel()

	adapter := &SlackAdapter{serviceURL: "https://example.com"}
	content := adapter.buildContent(domain.Task{
		Command: domain.TaskCommandComposeComic,
		JobID:   "job-1",
	}).String()

	if !strings.Contains(content, "**Command:** `compose_comic`") {
		t.Errorf("content = %q, want command", content)
	}
	if !strings.Contains(content, "**Job ID:** `job-1`") {
		t.Errorf("content = %q, want job id", content)
	}
	if !strings.Contains(content, "**History Detail:** [https://example.com/jobs/job-1]") {
		t.Errorf("content = %q, want history detail web link", content)
	}
}

func TestSlackContentLinksToCharacterPageForDesignSheet(t *testing.T) {
	t.Parallel()

	adapter := &SlackAdapter{serviceURL: "https://example.com"}
	content := adapter.buildContent(domain.Task{
		Command:      domain.TaskCommandGenerateDesignSheet,
		JobID:        "job-1",
		CharacterIDs: []string{"zundamon"},
	}).String()

	if !strings.Contains(content, "**Character Page:** [https://example.com/characters/zundamon]") {
		t.Errorf("content = %q, want character page link", content)
	}
	if strings.Contains(content, "History Detail") {
		t.Errorf("content = %q, design sheet job must not link to history detail", content)
	}
}

func TestSlackContentLinksToCharacterListForCompositeDesignSheet(t *testing.T) {
	t.Parallel()

	adapter := &SlackAdapter{serviceURL: "https://example.com"}
	content := adapter.buildContent(domain.Task{
		Command:      domain.TaskCommandGenerateDesignSheet,
		JobID:        "job-1",
		CharacterIDs: []string{"zundamon", "metan"},
	}).String()

	if !strings.Contains(content, "**Character Page:** [https://example.com/characters]") {
		t.Errorf("content = %q, want character list link for composite sheet", content)
	}
}

func TestSlackSuccessTitleVariesByCommand(t *testing.T) {
	t.Parallel()

	if title := slackSuccessTitle(domain.TaskCommandGenerateDesignSheet); !strings.Contains(title, "デザインシート") {
		t.Errorf("title = %q, want design sheet title", title)
	}
	if title := slackSuccessTitle(domain.TaskCommandComposeComic); !strings.Contains(title, "漫画生成") {
		t.Errorf("title = %q, want comic title", title)
	}
	if title := slackSuccessTitle(domain.TaskCommandRegeneratePanel); !strings.Contains(title, "再生成") {
		t.Errorf("title = %q, want regenerate title", title)
	}
}

func TestSlackContentIncludesSourceURL(t *testing.T) {
	t.Parallel()

	adapter := &SlackAdapter{serviceURL: "https://example.com"}
	content := adapter.buildContent(domain.Task{
		Command:   domain.TaskCommandComposeComic,
		JobID:     "job-1",
		SourceURL: "https://source.example/article",
	}).String()

	if !strings.Contains(content, "**Source:** https://source.example/article") {
		t.Errorf("content = %q, want source url", content)
	}
}

func TestSlackContentOmitsHistoryLinkWithoutServiceURL(t *testing.T) {
	t.Parallel()

	adapter := &SlackAdapter{}
	content := adapter.buildContent(domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-1"}).String()

	if strings.Contains(content, "History Detail") {
		t.Errorf("content = %q, did not expect history detail without serviceURL", content)
	}
}

// TestNotifyCompleteSendsTitleAndBody checks that the completion notice is sent with the heading for
// the command and the assembled body.
func TestNotifyCompleteSendsTitleAndBody(t *testing.T) {
	t.Parallel()

	rec := &recordingNotifier{}
	adapter := newTestAdapter(rec, "https://example.com")

	task := domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-1"}
	if err := adapter.NotifyComplete(context.Background(), task); err != nil {
		t.Fatalf("NotifyComplete failed: %v", err)
	}

	msg := rec.last(t)
	if want := slackSuccessTitle(domain.TaskCommandComposeComic); msg.Title != want {
		t.Errorf("Title = %q, want %q", msg.Title, want)
	}

	want := "**Command:** `compose_comic`\n" +
		"**Job ID:** `job-1`\n" +
		"**History Detail:** [https://example.com/jobs/job-1](https://example.com/jobs/job-1)"
	if msg.Body != want {
		t.Errorf("Body =\n%q\nwant\n%q", msg.Body, want)
	}
}

// TestNotifyErrorAppendsCause checks that the failure notice is sent with the error appended to the
// end of the body.
func TestNotifyErrorAppendsCause(t *testing.T) {
	t.Parallel()

	rec := &recordingNotifier{}
	adapter := newTestAdapter(rec, "")

	task := domain.Task{Command: domain.TaskCommandGenerateDesignSheet, JobID: "job-1"}
	if err := adapter.NotifyError(context.Background(), task, errors.New("パネル生成に失敗")); err != nil {
		t.Fatalf("NotifyError failed: %v", err)
	}

	msg := rec.last(t)
	if want := slackErrorTitle(domain.TaskCommandGenerateDesignSheet); msg.Title != want {
		t.Errorf("Title = %q, want %q", msg.Title, want)
	}
	if !strings.Contains(msg.Body, "**エラー内容:**\nパネル生成に失敗") {
		t.Errorf("Body = %q, want error detail", msg.Body)
	}
}

// TestNotifyErrorWithNilCause checks that the notice does not break when the cause is nil.
func TestNotifyErrorWithNilCause(t *testing.T) {
	t.Parallel()

	rec := &recordingNotifier{}
	adapter := newTestAdapter(rec, "")

	if err := adapter.NotifyError(context.Background(), domain.Task{JobID: "job-1"}, nil); err != nil {
		t.Fatalf("NotifyError failed: %v", err)
	}

	if body := rec.last(t).Body; !strings.Contains(body, "**エラー内容:**\n"+notify.NotAvailable) {
		t.Errorf("Body = %q, want N/A fallback", body)
	}
}

// TestNotifySetsLevel checks that completion and failure are sent with the kind of result.
// Slack turns it into the attachment colour, so it is needed separately from the heading's emoji.
func TestNotifySetsLevel(t *testing.T) {
	t.Parallel()

	task := domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-1"}

	t.Run("completion is success", func(t *testing.T) {
		t.Parallel()

		rec := &recordingNotifier{}
		if err := newTestAdapter(rec, "").NotifyComplete(context.Background(), task); err != nil {
			t.Fatalf("NotifyComplete failed: %v", err)
		}
		if got := rec.last(t).Level; got != notify.LevelSuccess {
			t.Errorf("Level = %v, want %v", got, notify.LevelSuccess)
		}
	})

	t.Run("failure is failure", func(t *testing.T) {
		t.Parallel()

		rec := &recordingNotifier{}
		if err := newTestAdapter(rec, "").NotifyError(context.Background(), task, errors.New("boom")); err != nil {
			t.Fatalf("NotifyError failed: %v", err)
		}
		if got := rec.last(t).Level; got != notify.LevelFailure {
			t.Errorf("Level = %v, want %v", got, notify.LevelFailure)
		}
	})
}
