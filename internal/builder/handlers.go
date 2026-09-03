package builder

import (
	"errors"
	"fmt"

	"github.com/shouni/gcp-kit/auth/oidc"
	"github.com/shouni/gcp-kit/auth/session"
	"github.com/shouni/gcp-kit/worker"

	"github.com/shouni/ap-story/internal/adapters/prompts"
	"github.com/shouni/ap-story/internal/app"
	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
	"github.com/shouni/ap-story/internal/server/handlers"
)

const defaultSessionName = "ap-story-session"

// AppHandlers は生成されたすべての HTTP ハンドラーを保持する構造体です。
// server パッケージはこの構造体を受け取ってルーティングを行います。
type AppHandlers struct {
	Auth   *session.Handler
	Web    *handlers.Handler
	Worker *worker.Handler[domain.Task]
	M2M    *oidc.Verifier
	// TaskAuth は Cloud Tasks からの OIDC を検証します。Auth と違い OAuth 設定を
	// 必要としないため、Web 面を持たない Worker プロセスでも構築できます。
	TaskAuth *oidc.Verifier
}

// Validate は、組み立て結果が役割として筋の通った形になっていることを確かめます。
//
// TaskAuth と Worker は「Cloud Tasks の検証」と「その先の処理」で対になっており、
// 片方だけが nil なのは DI の不整合です。router.go は nil を見てルート登録を省くため、
// 放置すると /tasks/generate が黙って 404 になるだけで、原因が設定なのか実装なのか
// リクエストからは区別できません。ルーターが 404 を返す前に起動を失敗させます。
func (h *AppHandlers) Validate() error {
	if (h.TaskAuth == nil) != (h.Worker == nil) {
		return errors.New("TaskAuth と Worker は同時に構成する必要があります")
	}
	return nil
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
		// audience と許可する caller SA の両方が揃わないと検証は常に失敗する
		// （fail-closed）ため、起動時に構成を確かめておきます。
		taskAuth, err := oidc.New(appCtx.Config.Tasks.TaskAudienceURL, appCtx.Config.Tasks.AllowedServiceAccounts)
		if err != nil {
			return nil, fmt.Errorf("cloud Tasks の OIDC 検証を構成できません: TASK_AUDIENCE_URL と ALLOWED_TASK_SERVICE_ACCOUNTS が必要です: %w", err)
		}
		h.TaskAuth = taskAuth
		h.Worker = worker.NewHandler[domain.Task](appCtx.Pipeline)
	}

	if err := h.Validate(); err != nil {
		return nil, err
	}

	return h, nil
}

// buildWebHandlers は Web 面（OAuth・Web/API・M2M 検証）のハンドラーを組み立てます。
func buildWebHandlers(appCtx *app.Container, h *AppHandlers) error {
	// 1. 認証Handlerの初期化（Google OAuth + Cloud Tasks OIDC 検証）
	authHandler, err := createAuthHandler(appCtx.Config, appCtx.SessionStore)
	if err != nil {
		return fmt.Errorf("認証Handlerの初期化に失敗しました: %w", err)
	}

	// 2. Web/API 用Handlerの初期化
	// 台本モード・スタイルモードの選択肢は、生成時と同じテンプレート（assets/prompts）
	// から取ります。Web 面が別の一覧を持つと、画面に出したモードが worker に無い、
	// という食い違いが起こり得ます。
	scriptPrompts, err := prompts.NewScriptPrompts()
	if err != nil {
		return fmt.Errorf("台本プロンプトの読み込みに失敗しました: %w", err)
	}
	styles, err := prompts.NewStyles()
	if err != nil {
		return fmt.Errorf("画風プリセットの読み込みに失敗しました: %w", err)
	}

	webHandler, err := handlers.NewHandler(handlers.HandlerOptions{
		TaskQueue:    appCtx.TaskQueue,
		Repository:   appCtx.Repository,
		JobStatus:    appCtx.JobStatus,
		Signer:       appCtx.Store,
		Bucket:       appCtx.Config.Storage.GCSBucket,
		Characters:   appCtx.Characters,
		ImageModels:  appCtx.Config.AI.ImageModels,
		GeminiModels: appCtx.Config.AI.GeminiModels,
		ScriptModes:  scriptPrompts.ModeInfos(),
		StyleModes:   styles.ModeInfos(),
	})
	if err != nil {
		return fmt.Errorf("WebHandlerの初期化に失敗しました: %w", err)
	}

	h.Auth = authHandler
	h.Web = webHandler
	m2m, err := newM2MVerifier(appCtx.Config.Server.ServiceURL, appCtx.Config.Auth.AllowedM2MServiceAccounts)
	if err != nil {
		return err
	}
	h.M2M = m2m

	return nil
}

// createAuthHandler は、認証ハンドラーを初期化して返します。
func createAuthHandler(cfg *config.Config, store session.Store) (*session.Handler, error) {
	return session.New(session.Config{
		ClientID:       cfg.Auth.GoogleClientID,
		ClientSecret:   cfg.Auth.GoogleClientSecret,
		ServiceURL:     cfg.Server.ServiceURL,
		SessionName:    defaultSessionName,
		Store:          store,
		AllowedEmails:  cfg.Auth.AllowedEmails,
		AllowedDomains: cfg.Auth.AllowedDomains,
	})
}

// newM2MVerifier は M2M(サーバー間通信)用の OIDC 検証器を構成します。
//
// ProtectedMiddleware は M2M を無効化できません。許可リストか audience が欠けていても
// 経路は生き続け、検証が必ず失敗してセッション認証へフォールバックします。つまり設定漏れは
// 「ブラウザは正常に動くが M2M クライアントだけログイン画面の HTML を受け取る」という形でしか
// 現れません。意図的な無効化と設定漏れを区別する手段が無い以上、空は後者としか解釈できない
// ので、TaskVerifier と同じく起動時に弾きます。
//
// 構成の可否を config ではなく検証器自身に尋ねるのは、必要な設定が何かを知っているのが
// gcp-kit 側だからです。許可リストの空だけを config で見ると audience（SERVICE_URL）の
// 欠落を拾えず、kit が要件を増やしても追随しません。
func newM2MVerifier(serviceURL string, allowedServiceAccounts []string) (*oidc.Verifier, error) {
	m2m, err := oidc.New(serviceURL, allowedServiceAccounts)
	if err != nil {
		return nil, fmt.Errorf("m2m の OIDC 検証を構成できません（SERVICE_URL と ALLOWED_M2M_SERVICE_ACCOUNTS）: %w", err)
	}
	return m2m, nil
}
