// Package config は、環境変数からアプリケーション設定を読み込み・検証します。
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/shouni/gcp-kit/serverrole"

	"github.com/caarlos0/env/v11"
	"github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-utils/text"
)

const (
	// DefaultShutdownGrace はサーバー停止時の猶予時間のデフォルト値です。
	DefaultShutdownGrace = 15 * time.Second
	// DefaultHTTPTimeout は外部 HTTP 通信のタイムアウトのデフォルト値です。
	DefaultHTTPTimeout = 60 * time.Second
)

// 画風の文言はこのパッケージが持ちません。コマ・ページもデザインシートも
// assets/prompts/styles.json のプリセットをスタイルモードで選ぶ形になったためです
// （シート用は演出を落とした design_style を別に持ちます）。
//
// モデル ID も既定値を持ちません。Google 側の都合で世代交代するため、
// GEMINI_MODELS / IMAGE_MODELS で必ず指定させ、未設定は起動時に落とします。

// ServerConfig は HTTP サーバーの設定です。
type ServerConfig struct {
	ServiceURL string `env:"SERVICE_URL" envDefault:"http://localhost:8080"`
	Port       string `env:"PORT" envDefault:"8080"`
	// Role はこのプロセスが担う役割です。明示が必須で、未設定は起動時エラーになります。
	Role            serverrole.Role `env:"SERVER_ROLE"`
	ShutdownTimeout time.Duration
}

// GCPConfig は Google Cloud Platform の設定です。
type GCPConfig struct {
	ProjectID  string `env:"GCP_PROJECT_ID"`
	LocationID string `env:"GCP_LOCATION_ID"`
}

// TasksConfig は Cloud Tasks キューへのエンキューと、受信時の OIDC 検証の設定です。
// Cloud Tasks に閉じた設定であり、GCP 一般の設定でも HTTP サーバーの設定でもないため、
// ap-mv・ap-comp と同じくここに集約します。
type TasksConfig struct {
	QueueID         string `env:"CLOUD_TASKS_QUEUE_ID"`
	WorkerURL       string `env:"WORKER_URL"`
	TaskAudienceURL string `env:"TASK_AUDIENCE_URL"`
	// CallerServiceAccountEmail は、投入するタスクの oidcToken.serviceAccountEmail に
	// 指定する caller SA です。トークンを生成して付与するのは Cloud Tasks であり、
	// このプロセスが署名するわけではありません。投入側＝ web 面だけの設定です。
	CallerServiceAccountEmail string `env:"TASK_CALLER_SERVICE_ACCOUNT_EMAIL"`
	// AllowedServiceAccountsRaw は、worker が受け付ける caller SA の許可リスト（カンマ区切り）です。
	// 空だと検証器が fail-closed になるため、worker では必須です。
	AllowedServiceAccountsRaw string `env:"ALLOWED_TASK_SERVICE_ACCOUNTS"`
	AllowedServiceAccounts    []string
	// DispatchDeadline は、投入するタスクに載せる応答待ちの上限です。
	//
	// 「待つ時間」ではなく **ワーカーの実行時間の実効上限** です。これを超えると
	// ワーカーがまだ処理中でも Cloud Tasks が待受を打ち切り、キューは max_attempts = 1 なので
	// 再試行も来ません。Cloud Run の timeout をいくら伸ばしてもこの上限は動きません。
	// 定数ではなく env なのは、この値をインフラ側（Terraform）が唯一の出どころとして
	// 持てるようにするためです。定数だとインフラが写しを抱え、ズレても誰も気付きません。
	//
	// **既定値は持ちません。** 三段のタイムアウトはデプロイ先の事情で決まる値なので、
	// 出どころは Terraform 1 箇所に閉じます。アプリが既定を持つと同じ数字が 2 箇所に
	// 現れ、設定漏れが「誰も選んでいない値」で動いてしまいます。
	DispatchDeadline time.Duration `env:"TASK_DISPATCH_DEADLINE"`
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
	// モデル一覧はカンマ区切りで、先頭が既定モデルです。既定値は持たず、
	// 空なら ValidateEssentialConfig が起動時に落とします（Web 面のみ）。
	//
	// ImageModels はデザインシート・パネル・ページのすべてに使い、GeminiModels は
	// 台本（章立て・章台本）に使います。どちらも先頭が既定で、一覧そのものが
	// フォームの選択肢と、投入時の許可リストになります。用途ごとにモデルを
	// 分ける仕組みは持ちません。
	GeminiModels []string `env:"GEMINI_MODELS"`
	ImageModels  []string `env:"IMAGE_MODELS"`

	// 比率と解像度は未設定ならキットの既定（3:4 / パネル 1K / ページ・シート 2K）に
	// 落ちます。従来の固定値と同じなので、設定しなければ挙動は変わりません。
	// 解像度は1コマごとに費用が効くため、デプロイ側で選べるようにしてあります。
	AspectRatio    string `env:"IMAGE_ASPECT_RATIO"`
	PanelImageSize string `env:"PANEL_IMAGE_SIZE"`
	PageImageSize  string `env:"PAGE_IMAGE_SIZE"`

	MaxConcurrency      int `env:"MAX_CONCURRENCY"`
	MaxChapters         int `env:"MAX_CHAPTERS"`
	MaxPanelsPerChapter int `env:"MAX_PANELS_PER_CHAPTER"`
	MaxPanelsPerPage    int `env:"MAX_PANELS_PER_PAGE"`

	// RateInterval は AI 呼び出しの発射間隔の下限です（0 で無制限）。
	// スループットの上限は MAX_CONCURRENCY ではなく 1/RATE_INTERVAL で決まるため、
	// 並列生成を効かせたい場合はここを 0 か十分小さい値にしてください。
	RateInterval time.Duration `env:"RATE_INTERVAL" envDefault:"10s"`

	// RequestTimeout は外部 AI 呼び出し1回あたりの上限です。
	// 画像生成1枚に数十秒かかるため、短すぎると生成そのものが打ち切られます
	// （go-comic-kit の既定と揃えて 5 分）。
	RequestTimeout time.Duration `env:"REQUEST_TIMEOUT" envDefault:"5m"`

	// PipelineTimeout はワーカータスク1件の実行時間の上限です。
	// REQUEST_TIMEOUT が外部 API 呼び出し1回あたりの上限であるのに対し、こちらは
	// ステップ列全体（台本→パネル→ページ）を包む上限です。
	//
	// TaskDispatchDeadline より短く取ります。等号でも駄目で、アプリが先に諦められないと
	// 失敗の記録も Slack 通知も出ないまま Cloud Tasks に打ち切られます
	// （worker では validatePipelineTimeout が起動時に拒否します）。
	PipelineTimeout time.Duration `env:"PIPELINE_TIMEOUT" envDefault:"25m"`
}

// applyDefaults はモデル一覧を正規化します。画風はプリセット（assets/prompts/styles.json）が
// 持つため、ここで補完するものはありません。並列数・タイムアウト・各種上限はキットの
// ApplyDefaults に任せます（キットを壊さず動かすための値なので、既定値の持ち主はキット側です）。
func (a *AIConfig) applyDefaults() {
	a.GeminiModels = normalizeList(a.GeminiModels)
	a.ImageModels = normalizeList(a.ImageModels)
}

// normalizeList は env が分割しただけのカンマ区切り値を整えます。
// 前後の空白を落とし、空要素と重複を捨て、順序は保ちます。
func normalizeList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		normalized = append(normalized, v)
	}
	return normalized
}

// KitConfig は go-comic-kit の ports.Config に変換します。
func (a AIConfig) KitConfig() ports.Config {
	return ports.Config{
		MaxConcurrency:      a.MaxConcurrency,
		RateInterval:        a.RateInterval,
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
	Tasks        TasksConfig
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
	if cfg.Tasks.TaskAudienceURL == "" {
		cfg.Tasks.TaskAudienceURL = cfg.Server.ServiceURL
	}

	cfg.Auth.AllowedEmails = text.ParseCommaSeparatedList(cfg.Auth.AllowedEmailsRaw)
	cfg.Auth.AllowedDomains = text.ParseCommaSeparatedList(cfg.Auth.AllowedDomainsRaw)
	cfg.Auth.AllowedM2MServiceAccounts = text.ParseCommaSeparatedList(cfg.Auth.AllowedM2MServiceAccountsRaw)
	cfg.Tasks.AllowedServiceAccounts = text.ParseCommaSeparatedList(cfg.Tasks.AllowedServiceAccountsRaw)
	cfg.Server.ShutdownTimeout = DefaultShutdownGrace

	return &cfg, nil
}

func (c *Config) normalize() error {
	// 環境変数名はアプリ側の関心事なので、キットのエラーへここで文脈を足します。
	role, err := serverrole.Parse(string(c.Server.Role))
	if err != nil {
		return fmt.Errorf("SERVER_ROLE: %w", err)
	}
	c.Server.Role = role

	c.Server.ServiceURL = strings.TrimSpace(c.Server.ServiceURL)
	workerURL, err := normalizeWorkerURL(c.Tasks.WorkerURL, c.Server.ServiceURL)
	if err != nil {
		return err
	}
	c.Tasks.WorkerURL = workerURL
	c.Tasks.TaskAudienceURL = strings.TrimSpace(c.Tasks.TaskAudienceURL)
	c.AI.applyDefaults()
	return nil
}
