package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/shouni/netarmor/securenet"
)

const taskGeneratePath = "/tasks/generate"

func normalizeWorkerURL(workerURL string, serviceURL string) (string, error) {
	workerURL = strings.TrimSpace(workerURL)
	if workerURL != "" {
		return workerURL, nil
	}
	return joinWorkerPath(serviceURL)
}

func joinWorkerPath(serviceURL string) (string, error) {
	serviceURL = strings.TrimSpace(serviceURL)
	if serviceURL == "" {
		return taskGeneratePath, nil
	}

	workerURL, err := url.JoinPath(serviceURL, taskGeneratePath)
	if err != nil {
		return "", fmt.Errorf("invalid service URL %q: %w", serviceURL, err)
	}
	return workerURL, nil
}

// IsSecureServiceURL は、設定された ServiceURL が安全なスキーム（HTTPS など）を使用しているかどうかを確認します。
func (c *Config) IsSecureServiceURL() bool {
	return securenet.IsSecureServiceURL(c.Server.ServiceURL)
}

// WarnOnContradictoryGenerationSettings は、生成制御の設定同士が噛み合っていない場合に
// 警告を出します。起動を止めるほどではないが、黙って期待外れの挙動になる組み合わせを
// 運用者に気づかせるためのものです。
func (c *Config) WarnOnContradictoryGenerationSettings() {
	// 一括生成のスループット上限は 1/RATE_INTERVAL で決まるため、間隔を空けたまま
	// 並列数だけ上げても速くならない。設定した本人が一番気づきにくい組み合わせ。
	if c.AI.MaxConcurrency > 1 && c.AI.RateInterval > 0 {
		slog.Warn("MAX_CONCURRENCY を上げても RATE_INTERVAL が発射間隔を律速するため並列化の効果が出ません",
			"max_concurrency", c.AI.MaxConcurrency,
			"rate_interval", c.AI.RateInterval,
			"effective_calls_per_minute", time.Minute/c.AI.RateInterval)
	}
	// 画像生成1枚に数十秒かかるため、極端に短い上限は生成そのものを打ち切る。
	if c.AI.RequestTimeout > 0 && c.AI.RequestTimeout < minSafeRequestTimeout {
		slog.Warn("REQUEST_TIMEOUT が短く、画像生成が完了前に打ち切られる可能性があります",
			"request_timeout", c.AI.RequestTimeout,
			"recommended_minimum", minSafeRequestTimeout)
	}
}

// minSafeRequestTimeout は、画像生成1回が収まる目安の下限です。
const minSafeRequestTimeout = 2 * time.Minute

// ValidateEssentialConfig はアプリケーション実行に不可欠な設定を検証します。
func (c *Config) ValidateEssentialConfig() error {
	if !c.IsSecureServiceURL() {
		return fmt.Errorf("本番環境では SERVICE_URL ('%s') は HTTPS である必要があります", c.Server.ServiceURL)
	}

	if c.GCP.ProjectID == "" {
		return fmt.Errorf("GCP_PROJECT_ID が設定されていません (Vertex AI 運用に必須)")
	}
	if c.GCP.LocationID == "" {
		return fmt.Errorf("GCP_LOCATION_ID が設定されていません (デフォルト: asia-northeast1)")
	}
	if c.Storage.GCSBucket == "" {
		return fmt.Errorf("STORY_BUCKET が設定されていません")
	}
	// CHARACTERS_JSON_PATH は任意。未設定時は go-character-kit の埋め込みデフォルト
	// キャラクター定義にフォールバックする（internal/adapters.LoadCharacters）。

	if c.Server.Role.ServesWeb() {
		if err := c.validateWebConfig(); err != nil {
			return err
		}
	}

	if c.Server.Role.ServesWorker() {
		if c.Tasks.TaskAudienceURL == "" {
			return fmt.Errorf("TASK_AUDIENCE_URL が設定されていません。Cloud Tasks の OIDC 検証に必須です")
		}
		// 空だと検証器が fail-closed になり、全タスクが 500 で失敗し続けます。
		if len(c.TaskIssuers()) == 0 {
			return fmt.Errorf("タスクの発行元が 1 件も指定されていません。ALLOWED_TASK_SERVICE_ACCOUNTS または SERVICE_ACCOUNT_EMAIL を設定してください")
		}
	}

	return nil
}

// TaskCallerServiceAccount は、投入するタスクに指定する caller SA を返します。
//
// TASK_CALLER_SERVICE_ACCOUNT_EMAIL があればそれを使い、無ければ旧 SERVICE_ACCOUNT_EMAIL に
// フォールバックします。後者は Terraform を新変数へ切り替えるまでの移行用であり、
// 適用後に削除します（残すと「この変数は誰のこと？」という曖昧さが戻るため）。
//
// TaskIssuers と同じく、フォールバックを normalize ではなくここに置くのは
// 呼び出し順への依存を作らないためです。
func (c *Config) TaskCallerServiceAccount() string {
	if email := strings.TrimSpace(c.Tasks.CallerServiceAccountEmail); email != "" {
		return email
	}
	return strings.TrimSpace(c.GCP.ServiceAccountEmail)
}

// TaskIssuers は、受信側が受け付ける Cloud Tasks トークンの発行元を返します。
//
// ALLOWED_TASK_SERVICE_ACCOUNTS があればそれを使い、無ければ SERVICE_ACCOUNT_EMAIL の
// 1 件にフォールバックします。web と worker で実行サービスアカウントを分けている構成では、
// worker が受け付けるべき発行元は自分自身ではなく web 側の SA です。単一値の
// SERVICE_ACCOUNT_EMAIL しか無いと、その値が役割ごとに別の意味（web では署名者、
// worker では許可する発行元）になってしまうため、明示できる口を用意しています。
//
// フォールバックを normalize ではなくここに置くのは、呼び出し順への依存を作らないためです。
func (c *Config) TaskIssuers() []string {
	if len(c.Tasks.AllowedServiceAccounts) > 0 {
		return c.Tasks.AllowedServiceAccounts
	}
	if email := strings.TrimSpace(c.GCP.ServiceAccountEmail); email != "" {
		return []string{email}
	}
	return nil
}

// validateWebConfig は Web 面（OAuth ログインとセッション、タスク投入）に必要な設定を検証します。
// Worker 面だけを提供するプロセスに OAuth 関連の設定を要求すると、
// 使わない認証情報へのアクセス権を配ることになるため役割で分けています。
func (c *Config) validateWebConfig() error {
	// タスクを投入するのは Web 面だけなので、キュー名も Web 面の要件です。
	if c.Tasks.QueueID == "" {
		return fmt.Errorf("CLOUD_TASKS_QUEUE_ID が設定されていません")
	}

	// caller SA はタスクを投入する側＝ web 面の要件です。worker が受け付ける許可リストは
	// ALLOWED_TASK_SERVICE_ACCOUNTS で別に指定するため、worker 専用プロセスは持たずに済みます。
	if c.TaskCallerServiceAccount() == "" {
		return fmt.Errorf("TASK_CALLER_SERVICE_ACCOUNT_EMAIL が設定されていません")
	}

	if c.Auth.GoogleClientID == "" || c.Auth.GoogleClientSecret == "" || c.Auth.SessionSecret == "" {
		return fmt.Errorf("google OAuth 関連の設定（ClientID, ClientSecret, SessionSecret）が不足しています")
	}

	if len(c.Auth.AllowedEmails) == 0 && len(c.Auth.AllowedDomains) == 0 {
		return fmt.Errorf("許可されたメールアドレスまたはドメインが一つも設定されていません（認可リストが空です）")
	}

	if c.Auth.SessionEncryptKey == "" {
		return fmt.Errorf("SESSION_ENCRYPT_KEY が設定されていません。セキュアな運用のために必須です")
	}

	// SessionEncryptKey の長さチェック (AES要件: 16, 24, 32 bytes)
	if keyLen := len(c.Auth.SessionEncryptKey); keyLen != 16 && keyLen != 24 && keyLen != 32 {
		return fmt.Errorf("SESSION_ENCRYPT_KEY の長さが不正です (%d バイト)。16, 24, 32 バイトのいずれかにしてください", keyLen)
	}

	// M2M（ap-mcp 等からの呼び出し）も継続して受け付けるため、SA 許可リストも必須とする。
	if len(c.Auth.AllowedM2MServiceAccounts) == 0 {
		return fmt.Errorf("ALLOWED_M2M_SERVICE_ACCOUNTS が設定されていません（M2M 呼び出しを許可する SA が1つも登録されていません）")
	}

	return nil
}
