// Package app は、アプリケーションの依存関係を組み立てて保持する DI コンテナを提供します。
package app

import (
	"io"
	"log/slog"

	characterkit "github.com/shouni/go-character-kit/character"
	"github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-http-kit/httpkit"
	"github.com/shouni/go-job-kit/cache"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/config"
	"github.com/shouni/ap-story/internal/domain"
	"github.com/shouni/ap-story/internal/pipeline"
)

// Container はアプリケーションの依存関係（DIコンテナ）を保持します。
type Container struct {
	Config *config.Config
	// I/O and Storage
	RemoteIO *RemoteIO
	// go-comic-kit の全操作（章立て・章台本・デザインシート・パネル・ページ）
	Ops *ports.Operations
	// Pipeline は Task のコマンドに応じて Ops の操作を実行する Worker パイプラインです。
	Pipeline *pipeline.Runner
	// TaskQueue は非同期ジョブ（Task）を Cloud Tasks に投入します。
	TaskQueue domain.TaskQueue
	// Repository は作品履歴の一覧・詳細・削除を提供します。
	Repository domain.ComicRepository
	// JobStatus はジョブ進行状況の記録・参照を提供します。
	JobStatus domain.JobStatusStore
	// HistoryCache は Repository が使う履歴サマリの TTL キャッシュです。
	// Close で停止するために Container が保持します。
	HistoryCache *cache.TTL[domain.ComicHistory]
	// Characters は go-character-kit のキャラクター定義です。
	Characters *characterkit.Characters
	// HTTPClient は外部 HTTP 通信（go-web-reader 等）に使う汎用クライアントです。
	HTTPClient httpkit.HTTPClient
	Closers    []io.Closer
}

// RemoteIO は外部ストレージ操作に関するコンポーネントをまとめます。
//
// 実体は go-remote-io が持つ remoteio.Bundle です。同じ構造体と組み立て関数を
// 各アプリが個別に持っていたものをライブラリへ引き取ったため、ここはアプリ内での
// 呼び名を保つための別名だけになっています（rio.Reader などの参照はそのまま使えます）。
type RemoteIO = remoteio.Bundle

// Close は、Container が保持するすべての外部接続リソースを安全に解放します。
func (c *Container) Close() {
	for _, closer := range c.Closers {
		if closer == nil {
			continue
		}
		if err := closer.Close(); err != nil {
			slog.Error("failed to close resource", "error", err)
		}
	}
	if c.Ops != nil {
		if err := c.Ops.Close(); err != nil {
			slog.Error("failed to close comic operations", "error", err)
		}
	}
	if c.HistoryCache != nil {
		c.HistoryCache.Close()
	}
}
