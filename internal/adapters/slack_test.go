package adapters

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shouni/go-notify/notify"

	"github.com/shouni/ap-story/internal/domain"
)

// recordingNotifier は送信された notify.Message を記録するフェイクです。
// 実際の Slack 記法への変換は go-notify 側の責務なので、ここでは
// ap-story が組み立てた見出しと本文だけを検証します。
type recordingNotifier struct {
	got []notify.Message
}

// Notify は notify.Notifier を実装し、受け取った Message を記録します。
func (r *recordingNotifier) Notify(_ context.Context, msg notify.Message) error {
	r.got = append(r.got, msg)
	return nil
}

// newTestAdapter は Notifier を差し替えたアダプターを組み立てます。
// 見出しは送信時に WithTitles で決まるため、ここでは空の Titles で足ります。
func newTestAdapter(n notify.Notifier, serviceURL string) *SlackAdapter {
	return &SlackAdapter{
		pipeline:   notify.NewPipeline(n, notify.Titles{}),
		serviceURL: serviceURL,
	}
}

// last は最後に送信された Message を返します。
func (r *recordingNotifier) last(t *testing.T) notify.Message {
	t.Helper()
	if len(r.got) == 0 {
		t.Fatal("通知が送信されていません")
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

// TestNotifyCompleteSendsTitleAndBody は、完了通知がコマンドに応じた見出しと
// 組み立てた本文で送信されることを確認します。
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

// TestNotifyErrorAppendsCause は、失敗通知が本文の末尾にエラー内容を
// 追記して送信することを確認します。
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

// TestNotifyErrorWithNilCause は、原因が nil でも通知が壊れないことを確認します。
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

// TestNotifySetsLevel は、完了・失敗が結果の種別を伴って送信されることを確認します。
// Slack 側はこれを attachment の色に落とすため、見出しの絵文字とは別に必要です。
func TestNotifySetsLevel(t *testing.T) {
	t.Parallel()

	task := domain.Task{Command: domain.TaskCommandComposeComic, JobID: "job-1"}

	t.Run("完了は成功", func(t *testing.T) {
		t.Parallel()

		rec := &recordingNotifier{}
		if err := newTestAdapter(rec, "").NotifyComplete(context.Background(), task); err != nil {
			t.Fatalf("NotifyComplete failed: %v", err)
		}
		if got := rec.last(t).Level; got != notify.LevelSuccess {
			t.Errorf("Level = %v, want %v", got, notify.LevelSuccess)
		}
	})

	t.Run("失敗は失敗", func(t *testing.T) {
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
