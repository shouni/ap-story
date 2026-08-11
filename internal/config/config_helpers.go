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
		if len(c.Tasks.AllowedServiceAccounts) == 0 {
			return fmt.Errorf("許可する caller SA が 1 件も指定されていません。ALLOWED_TASK_SERVICE_ACCOUNTS を設定してください")
		}
	}

	return nil
}

// TaskCallerServiceAccount は、投入するタスクに指定する caller SA を返します。
// 値は env から読んだままなので、前後の空白だけ落とします。
func (c *Config) TaskCallerServiceAccount() string {
	return strings.TrimSpace(c.Tasks.CallerServiceAccountEmail)
}

// validateWebConfig は Web 面（OAuth ログインとセッション、タスク投入）に必要な設定を検証します。
// Worker 面だけを提供するプロセスに OAuth 関連の設定を要求すると、
// 使わない認証情報へのアクセス権を配ることになるため役割で分けています。
func (c *Config) validateWebConfig() error {
	// モデル一覧は Web 面だけの要件です。フォームの選択肢と、投入時の許可リストに使います。
	// worker は読みません——ジョブが必ず自分のモデル名を運ぶようになったためです
	// （go-comic-kit も設定としてのモデル既定を持ちません）。
	//
	// 空のまま起動すると選択欄が消え、JSON API のモデル指定が全部 400 になります。
	// 起動自体は成功してしまうので、「選べないこと」に気付くのが使ってみた後になります。
	for _, models := range []struct {
		name string
		list []string
	}{
		{"IMAGE_MODELS", c.AI.ImageModels},
		{"GEMINI_MODELS", c.AI.GeminiModels},
	} {
		if len(models.list) == 0 {
			return fmt.Errorf("%s が設定されていません（カンマ区切りで複数指定すると、先頭が既定でフォームの選択肢になります）", models.name)
		}
	}

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

	return nil
}
