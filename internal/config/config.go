// Package config は、環境変数からアプリケーション設定を読み込み・検証します。
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-utils/text"
)

const (
	// DefaultShutdownGrace はサーバー停止時の猶予時間のデフォルト値です。
	DefaultShutdownGrace = 15 * time.Second
	// DefaultHTTPTimeout は外部 HTTP 通信のタイムアウトのデフォルト値です。
	DefaultHTTPTimeout = 60 * time.Second
	// SignedURLExpiration は署名付き URL の有効期限です。
	SignedURLExpiration = 30 * time.Minute
	// DefaultHistoryPageSize は履歴一覧のページサイズのデフォルト値です。
	DefaultHistoryPageSize = 20
)

// ServerConfig は HTTP サーバーの設定です。
type ServerConfig struct {
	ServiceURL      string `env:"SERVICE_URL" envDefault:"http://localhost:8080"`
	WorkerURL       string `env:"WORKER_URL"`
	Port            string `env:"PORT" envDefault:"8080"`
	ShutdownTimeout time.Duration
}

// GCPConfig は Google Cloud Platform の設定です。
type GCPConfig struct {
	ProjectID           string `env:"GCP_PROJECT_ID"`
	LocationID          string `env:"GCP_LOCATION_ID"`
	QueueID             string `env:"CLOUD_TASKS_QUEUE_ID"`
	TaskAudienceURL     string `env:"TASK_AUDIENCE_URL"`
	ServiceAccountEmail string `env:"SERVICE_ACCOUNT_EMAIL"`
}

// StorageConfig はストレージの設定です。
type StorageConfig struct {
	GCSBucket          string `env:"STORY_BUCKET"`
	CharactersJSONPath string `env:"CHARACTERS_JSON_PATH"`
}

// NotificationConfig は通知の設定です。
type NotificationConfig struct {
	SlackWebhookURL string `env:"SLACK_WEBHOOK_URL"`
}

// AIConfig は AI モデルと実行制御の設定です。go-comic-kit の ports.Config にマップされます。
type AIConfig struct {
	GeminiModel         string `env:"GEMINI_MODEL"`
	ImageStandardModel  string `env:"IMAGE_STANDARD_MODEL"`
	ImageQualityModel   string `env:"IMAGE_QUALITY_MODEL"`
	StyleSuffix         string `env:"STYLE_SUFFIX"`
	DesignStyleSuffix   string `env:"DESIGN_STYLE_SUFFIX"`
	MaxConcurrency      int    `env:"MAX_CONCURRENCY"`
	MaxChapters         int    `env:"MAX_CHAPTERS"`
	MaxPanelsPerChapter int    `env:"MAX_PANELS_PER_CHAPTER"`
	MaxPanelsPerPage    int    `env:"MAX_PANELS_PER_PAGE"`

	// RateInterval は AI 呼び出しの発射間隔の下限です（0 で無制限）。
	// スループットの上限は MAX_CONCURRENCY ではなく 1/RATE_INTERVAL で決まるため、
	// 並列生成を効かせたい場合はここを 0 か十分小さい値にしてください。
	RateInterval time.Duration `env:"RATE_INTERVAL" envDefault:"10s"`

	// RequestTimeout は外部 AI 呼び出し1回あたりの上限です。
	// 画像生成1枚に数十秒かかるため、短すぎると生成そのものが打ち切られます
	// （go-comic-kit の既定と揃えて 5 分）。
	RequestTimeout time.Duration `env:"REQUEST_TIMEOUT" envDefault:"5m"`

	// PipelineTimeout はワーカータスク1件の実行時間の上限です。0 以下は無制限を意味します。
	// REQUEST_TIMEOUT が外部 API 呼び出し1回あたりの上限であるのに対し、こちらは
	// ステップ列全体（台本→パネル→ページ）を包む上限です。
	PipelineTimeout time.Duration `env:"PIPELINE_TIMEOUT" envDefault:"45m"`
}

// KitConfig は go-comic-kit の ports.Config に変換します。
// ゼロ値のフィールドは go-comic-kit 側の ApplyDefaults が既定値で補完します。
func (a AIConfig) KitConfig() ports.Config {
	return ports.Config{
		GeminiModel:         a.GeminiModel,
		ImageStandardModel:  a.ImageStandardModel,
		ImageQualityModel:   a.ImageQualityModel,
		MaxConcurrency:      a.MaxConcurrency,
		RateInterval:        a.RateInterval,
		StyleSuffix:         a.StyleSuffix,
		DesignStyleSuffix:   a.DesignStyleSuffix,
		MaxChapters:         a.MaxChapters,
		MaxPanelsPerChapter: a.MaxPanelsPerChapter,
		MaxPanelsPerPage:    a.MaxPanelsPerPage,
		RequestTimeout:      a.RequestTimeout,
	}
}

// AuthConfig は認証と認可の設定です。
// ブラウザ向け Web UI 用の Google OAuth と、サーバー間通信（M2M）向けの
// サービスアカウント OIDC 検証の両方を扱います（ap-comp と同様の二本立て）。
type AuthConfig struct {
	GoogleClientID     string `env:"GOOGLE_CLIENT_ID"`
	GoogleClientSecret string `env:"GOOGLE_CLIENT_SECRET"`
	// SessionSecret はセッションクッキーの署名鍵（HMAC、16バイト以上）です。
	SessionSecret string `env:"SESSION_SECRET"`
	// SessionEncryptKey はセッションクッキーの暗号化鍵（AES、16/24/32バイト）です。
	SessionEncryptKey string `env:"SESSION_ENCRYPT_KEY"`
	AllowedEmailsRaw  string `env:"ALLOWED_EMAILS"`
	AllowedDomainsRaw string `env:"ALLOWED_DOMAINS"`
	AllowedEmails     []string
	AllowedDomains    []string
	// AllowedM2MServiceAccountsRaw は、API をサーバー間通信（OIDC Bearer トークン）で
	// 呼び出せるサービスアカウントのメールアドレス（カンマ区切り）です。
	AllowedM2MServiceAccountsRaw string `env:"ALLOWED_M2M_SERVICE_ACCOUNTS"`
	AllowedM2MServiceAccounts    []string
}

// Config はアプリ設定です。
type Config struct {
	Server       ServerConfig
	GCP          GCPConfig
	Storage      StorageConfig
	Notification NotificationConfig
	AI           AIConfig
	Auth         AuthConfig
}

// LoadConfig は環境変数から設定を読み込みます。
func LoadConfig() (*Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, err
	}

	if err := cfg.normalize(); err != nil {
		return nil, fmt.Errorf("failed to normalize config: %w", err)
	}
	if cfg.GCP.TaskAudienceURL == "" {
		cfg.GCP.TaskAudienceURL = cfg.Server.ServiceURL
	}

	cfg.Auth.AllowedEmails = text.ParseCommaSeparatedList(cfg.Auth.AllowedEmailsRaw)
	cfg.Auth.AllowedDomains = text.ParseCommaSeparatedList(cfg.Auth.AllowedDomainsRaw)
	cfg.Auth.AllowedM2MServiceAccounts = text.ParseCommaSeparatedList(cfg.Auth.AllowedM2MServiceAccountsRaw)
	cfg.Server.ShutdownTimeout = DefaultShutdownGrace

	return &cfg, nil
}

func (c *Config) normalize() error {
	c.Server.ServiceURL = strings.TrimSpace(c.Server.ServiceURL)
	workerURL, err := normalizeWorkerURL(c.Server.WorkerURL, c.Server.ServiceURL)
	if err != nil {
		return err
	}
	c.Server.WorkerURL = workerURL
	c.GCP.TaskAudienceURL = strings.TrimSpace(c.GCP.TaskAudienceURL)
	return nil
}
