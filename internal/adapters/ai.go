// Package adapters は、Gemini クライアントの初期化とキャラクター定義の読み込みなど、
// go-comic-kit と外部サービスをつなぐアダプターを提供します。
package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/shouni/go-gemini-client/gemini"

	"github.com/shouni/ap-story/internal/config"
)

const (
	// defaultVertexLocationID は Vertex AI のデフォルトロケーションです。
	// Cloud Tasks 等のリージョン（GCP_LOCATION_ID）とは独立して固定します。
	defaultVertexLocationID = "global"
	// defaultVertexInitialDelay はリトライ時の初期待ち時間です。
	defaultVertexInitialDelay = 60 * time.Second
)

// NewVertexAIAdapter は GCP Vertex AI クライアントを初期化します。
// go-comic-kit の workflow.Args.AIClient として渡します。Vertex AI モードでは
// GCS URI を直接参照でき（File API アップロードを省略）、画像生成のコストを抑えられます。
func NewVertexAIAdapter(ctx context.Context, gcp config.GCPConfig) (*gemini.Client, error) {
	if gcp.ProjectID == "" {
		return nil, fmt.Errorf("GCP_PROJECT_ID が設定されていません")
	}

	clientConfig := gemini.Config{
		ProjectID:    gcp.ProjectID,
		LocationID:   defaultVertexLocationID,
		InitialDelay: defaultVertexInitialDelay,
	}

	aiClient, err := gemini.NewClient(ctx, clientConfig)
	if err != nil {
		return nil, fmt.Errorf("vertex AI クライアントの初期化に失敗しました: %w", err)
	}

	return aiClient, nil
}
