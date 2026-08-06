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
}

// BuildHandlers は各ハンドラーの依存関係をすべて組み立て、AppHandlers 構造体を返します。
func BuildHandlers(appCtx *app.Container) (*AppHandlers, error) {
	if appCtx.Config.Server.ServiceURL == "" {
		return nil, fmt.Errorf("認証リダイレクトのために ServiceURL の設定が必要です")
	}

	// 1. 認証Handlerの初期化（Google OAuth + Cloud Tasks OIDC 検証）
	authHandler, err := createAuthHandler(appCtx.Config)
	if err != nil {
		return nil, fmt.Errorf("認証Handlerの初期化に失敗しました: %w", err)
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
		return nil, fmt.Errorf("WebHandlerの初期化に失敗しました: %w", err)
	}

	// 3. 非同期ワーカー用Handlerの初期化
	workerHandler := worker.NewHandler[domain.Task](appCtx.Pipeline)

	// 4. M2M(サーバー間通信)用OIDC検証器の初期化
	m2mVerifier := auth.NewM2MVerifier(appCtx.Config.Server.ServiceURL, appCtx.Config.Auth.AllowedM2MServiceAccounts)

	return &AppHandlers{
		Auth:   authHandler,
		Web:    webHandler,
		Worker: workerHandler,
		M2M:    m2mVerifier,
	}, nil
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
		TaskAudienceURL:   cfg.GCP.TaskAudienceURL,
		// Cloud Tasks の OIDC トークンに署名するサービスアカウント。audience は
		// 誰でも指定できる文字列に過ぎず、それだけでは呼び出し元を認証できないため、
		// 発行元サービスアカウントの照合まで行わせる（未設定だと起動時に失敗する）。
		AllowedTaskServiceAccounts: []string{cfg.GCP.ServiceAccountEmail},
	})
}
