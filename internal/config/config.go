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
)

// 画風指定の既定値。
//
// go-comic-kit はモデル名と画風指定のどちらも既定値を持たず、未設定のまま渡すと
// ports.ErrConfigInvalid で構築に失敗します。画風指定は作品ごとに調整する文言なので
// このアプリが既定値を持ちますが、モデル ID は Google 側の都合で世代交代するため
// 既定値を持たず、GEMINI_MODELS / IMAGE_MODELS で必ず指定させます。
const (
	// DefaultStyleSuffix はパネル・ページ画像に付与する既定の画風指定です。
	// 演出（cinematic lighting 等）を含むため、デザインシートには使いません。
	DefaultStyleSuffix = "Japanese anime style, official art, cel-shaded, clean line art, high-quality manga coloring, expressive eyes, vibrant colors, cinematic lighting, masterpiece, ultra-detailed, flat shading, clear character features, no 3D effect, high resolution"

	// DefaultDesignStyleSuffix はデザインシートに付与する既定の画風指定です。
	// シートは他生成物の同一性アンカーなので、照明・演出系の指定を含めません。
	DefaultDesignStyleSuffix = "Japanese anime style, official character reference art, cel-shaded, clean line art, vibrant colors, clear character features, no 3D effect, high resolution"
)

// ServerConfig は HTTP サーバーの設定です。
type ServerConfig struct {
	ServiceURL string `env:"SERVICE_URL" envDefault:"http://localhost:8080"`
	Port       string `env:"PORT" envDefault:"8080"`
	// Role はこのプロセスが担う役割です。明示が必須で、未設定は起動時エラーになります。
	Role            ServerRole `env:"SERVER_ROLE"`
	ShutdownTimeout time.Duration
}

// ServerRole はプロセスが担う役割です。Cloud Run のサービスを web と worker に
// 分けたときに、各プロセスが必要とする依存だけを構築するために使います。
type ServerRole string

const (
	// ServerRoleBoth は Web と Worker の両方を提供します（ローカル開発用）。
	ServerRoleBoth ServerRole = "both"
	// ServerRoleWeb は Web UI と M2M API だけを提供し、/tasks/generate を公開しません。
	ServerRoleWeb ServerRole = "web"
	// ServerRoleWorker は /tasks/generate だけを提供し、Web UI と OAuth を持ちません。
	ServerRoleWorker ServerRole = "worker"
)

// ParseServerRole は SERVER_ROLE の値を役割に変換します。空文字も未知の値もエラーです。
//
// 未設定を both とみなすと、本番の環境変数が 1 つ欠けただけで公開 web に
// ワーカールートが復活します。未知の値を黙って受け入れると、今度は何のルートも
// 提供しないサービスがデプロイされます。どちらも起動時に落とすほうが安全です。
func ParseServerRole(raw string) (ServerRole, error) {
	role := ServerRole(strings.ToLower(strings.TrimSpace(raw)))
	switch role {
	case ServerRoleBoth, ServerRoleWeb, ServerRoleWorker:
		return role, nil
	default:
		return "", fmt.Errorf("SERVER_ROLE (%q) は %q, %q, %q のいずれかである必要があります",
			raw, ServerRoleWeb, ServerRoleWorker, ServerRoleBoth)
	}
}

// ServesWeb は、この役割が Web 面（/api/* と OAuth）を提供するかを返します。
func (r ServerRole) ServesWeb() bool { return r == ServerRoleBoth || r == ServerRoleWeb }

// ServesWorker は、この役割が Worker 面（/tasks/generate）を提供するかを返します。
func (r ServerRole) ServesWorker() bool { return r == ServerRoleBoth || r == ServerRoleWorker }

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
	// モデル一覧はカンマ区切りで、先頭が既定モデルです。単数形は LoadConfig が
	// 一覧の先頭から埋めるので、環境変数からは読みません。既定値は持たず、
	// 空なら ValidateEssentialConfig が起動時に落とします。
	//
	// ImageModels はデザインシート・パネル・ページのすべてに使います（先頭が既定で、
	// 残りはデザインシート生成フォームの選択肢になります）。用途ごとにモデルを
	// 分ける仕組みは持ちません。
	GeminiModels []string `env:"GEMINI_MODELS"`
	ImageModels  []string `env:"IMAGE_MODELS"`
	GeminiModel  string   `env:"-"`
	ImageModel   string   `env:"-"`

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

// applyDefaults は画風指定の未設定を既定値で補完し、モデル一覧を正規化します。
// 並列数・タイムアウト・各種上限はキットの ApplyDefaults に任せます
// （キットを壊さず動かすための値なので、既定値の持ち主はキット側です）。
func (a *AIConfig) applyDefaults() {
	a.GeminiModels = normalizeList(a.GeminiModels)
	a.ImageModels = normalizeList(a.ImageModels)
	a.GeminiModel = firstModel(a.GeminiModels)
	a.ImageModel = firstModel(a.ImageModels)
	a.StyleSuffix = defaultIfEmpty(a.StyleSuffix, DefaultStyleSuffix)
	a.DesignStyleSuffix = defaultIfEmpty(a.DesignStyleSuffix, DefaultDesignStyleSuffix)
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

// firstModel は一覧の先頭（＝既定として使うモデル）を返します。
// 一覧が空になるのは設定漏れのときだけで、その値は使われる前に起動時検証で弾かれます。
func firstModel(models []string) string {
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

// defaultIfEmpty は、値が空白のみなら fallback を返します。
func defaultIfEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

// KitConfig は go-comic-kit の ports.Config に変換します。
func (a AIConfig) KitConfig() ports.Config {
	return ports.Config{
		GeminiModel:         a.GeminiModel,
		ImageModel:          a.ImageModel,
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
	role, err := ParseServerRole(string(c.Server.Role))
	if err != nil {
		return err
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
