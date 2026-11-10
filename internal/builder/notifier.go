package builder

import (
	"fmt"

	"github.com/shouni/go-http-kit/httpkit"

	"github.com/shouni/ap-story/internal/adapters"
	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
)

// buildNotifier builds the Slack notification adapter. It works without SLACK_WEBHOOK_URL,
// in which case it returns an adapter that sends nothing (an optional operational setting).
func buildNotifier(httpClient httpkit.HTTPClient, cfg *config.Config) (domain.Notifier, error) {
	notifier, err := adapters.NewSlackAdapter(httpClient, cfg.Notification.SlackWebhookURL, cfg.Server.ServiceURL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Slack adapter: %w", err)
	}
	return notifier, nil
}
