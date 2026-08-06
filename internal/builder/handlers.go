package builder

import (
	"fmt"
	"net/url"

	"github.com/shouni/gcp-kit/auth"
	"github.com/shouni/gcp-kit/worker"

	"github.com/shouni/ap-story/internal/app"
	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
	"github.com/shouni/ap-story/internal/server/handlers"
)

const defaultSessionName = "ap-story-session"

// AppHandlers は生成されたすべての HTTP ハンドラーを保持する構造体です。
// server パッケージはこの構造体を受け取ってルーティングを行います。
type AppHandlers struct {
	Auth   *auth.Handler
	Web    *handlers.Handler
	Worker *worker.Handler[domain.Task]
	M2M    *auth.M2MVerifier
	// TaskAuth は Cloud Tasks からの OIDC を検証します。Auth と違い OAuth 設定を
	// 必要としないため、Web 面を持たない Worker プロセスでも構築できます。
	TaskAuth *auth.TaskVerifier
}

// BuildHandlers は各ハンドラーの依存関係を SERVER_ROLE に応じて組み立て、
// AppHandlers 構造体を返します。担当しない面のハンドラーは nil のままにし、
// router 側でルート登録ごと省かれるようにします。
func BuildHandlers(appCtx *app.Container) (*AppHandlers, error) {
	if appCtx.Config.Server.ServiceURL == "" {
		return nil, fmt.Errorf("認証リダイレクトのために ServiceURL の設定が必要です")
	}

	h := &AppHandlers{}
	role := appCtx.Config.Server.Role

	if role.ServesWeb() {
		if err := buildWebHandlers(appCtx, h); err != nil {
			return nil, err
		}
	}

	if role.ServesWorker() {
		// audience と発行元サービスアカウントの両方が揃わないと検証は常に失敗する
		// （fail-closed）ため、起動時に構成を確かめておきます。
		taskAuth := auth.NewTaskVerifier(
			appCtx.Config.Tasks.TaskAudienceURL,
			appCtx.Config.TaskIssuers(),
		)
		if !taskAuth.Configured() {
			return nil, fmt.Errorf("cloud Tasks の OIDC 検証を構成できません: TASK_AUDIENCE_URL と、ALLOWED_TASK_SERVICE_ACCOUNTS または SERVICE_ACCOUNT_EMAIL が必要です")
		}
		h.TaskAuth = taskAuth
		h.Worker = worker.NewHandler[domain.Task](appCtx.Pipeline)
	}

	return h, nil
}

// buildWebHandlers は Web 面（OAuth・Web/API・M2M 検証）のハンドラーを組み立てます。
func buildWebHandlers(appCtx *app.Container, h *AppHandlers) error {
	// 1. 認証Handlerの初期化（Google OAuth + Cloud Tasks OIDC 検証）
	authHandler, err := createAuthHandler(appCtx.Config)
	if err != nil {
		return fmt.Errorf("認証Handlerの初期化に失敗しました: %w", err)
	}

	// 2. Web/API 用Handlerの初期化
	webHandler, err := handlers.NewHandler(handlers.HandlerOptions{
		TaskQueue:          appCtx.TaskQueue,
		Repository:         appCtx.Repository,
		JobStatus:          appCtx.JobStatus,
		Signer:             appCtx.RemoteIO.Signer,
		Bucket:             appCtx.Config.Storage.GCSBucket,
		Characters:         appCtx.Characters,
		ImageStandardModel: appCtx.Config.AI.ImageStandardModel,
		ImageQualityModel:  appCtx.Config.AI.ImageQualityModel,
	})
	if err != nil {
		return fmt.Errorf("WebHandlerの初期化に失敗しました: %w", err)
	}

	h.Auth = authHandler
	h.Web = webHandler
	// 3. M2M(サーバー間通信)用OIDC検証器の初期化
	h.M2M = auth.NewM2MVerifier(appCtx.Config.Server.ServiceURL, appCtx.Config.Auth.AllowedM2MServiceAccounts)

	return nil
}

// createAuthHandler は、認証ハンドラーを初期化して返します。
func createAuthHandler(cfg *config.Config) (*auth.Handler, error) {
	redirectURL, err := url.JoinPath(cfg.Server.ServiceURL, "/auth/callback")
	if err != nil {
		return nil, fmt.Errorf("リダイレクトURLの構築に失敗しました: %w", err)
	}

	return auth.NewHandler(auth.Config{
		ClientID:          cfg.Auth.GoogleClientID,
		ClientSecret:      cfg.Auth.GoogleClientSecret,
		RedirectURL:       redirectURL,
		SessionAuthKey:    cfg.Auth.SessionSecret,
		SessionEncryptKey: cfg.Auth.SessionEncryptKey,
		SessionName:       defaultSessionName,
		IsSecureCookie:    cfg.IsSecureServiceURL(),
		AllowedEmails:     cfg.Auth.AllowedEmails,
		AllowedDomains:    cfg.Auth.AllowedDomains,
		TaskAudienceURL:   cfg.Tasks.TaskAudienceURL,
		// Cloud Tasks の OIDC トークンを発行したサービスアカウントの許可リスト。audience は
		// 誰でも指定できる文字列に過ぎず、それだけでは呼び出し元を認証できないため、
		// 発行元サービスアカウントの照合まで行わせる（未設定だと起動時に失敗する）。
		AllowedTaskServiceAccounts: cfg.TaskIssuers(),
	})
}
