package builder

import (
	"fmt"

	"github.com/shouni/go-http-kit/httpkit"

	"github.com/shouni/ap-story/internal/adapters"
	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
)

// buildNotifier は Slack 通知アダプターを構築します。SLACK_WEBHOOK_URL が未設定でも
// 動作し、その場合は通知を送らないアダプターを返します（運用の任意設定）。
func buildNotifier(httpClient httpkit.HTTPClient, cfg *config.Config) (domain.Notifier, error) {
	notifier, err := adapters.NewSlackAdapter(httpClient, cfg.Notification.SlackWebhookURL, cfg.Server.ServiceURL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Slack adapter: %w", err)
	}
	return notifier, nil
}
