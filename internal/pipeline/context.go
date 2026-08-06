// Package pipeline は、domain.Task のコマンドに応じて go-comic-kit の操作を
// 順に実行する Worker パイプラインを提供します。
package pipeline

import (
	"github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/domain"
)

// State は、パイプライン実行中に変化していく値です。
type State struct {
	Task *domain.Task
	// Manga は実行対象の MangaState です。ステップが読み書きします。
	// compose_comic の最初のステップ（Outline）でのみ nil から新規作成されます。
	Manga *ports.MangaState
	// OutputDir はこのジョブの成果物を格納する GCS URI です。作品ジョブでは
	// comics/{jobID} を指します。デザインシート単体生成で既存の作品 state が
	// 見つからない場合、LoadStateStepOptional が DesignJobOutputDir へ切り替えます。
	OutputDir string
	// DesignJobOutputDir は、デザインシート単体生成ジョブの state 保存先
	// （design-jobs/{jobID}）です。
	DesignJobOutputDir string
	// CharacterOutputDir は、ジョブに依存しない共有キャラクター資産
	// （デザインシート等）の保存先バケットルート URI です。
	CharacterOutputDir string
}

// Services は、パイプライン実行中は固定の外部依存です。ステップは参照するだけで
// 書き換えません。
type Services struct {
	Ops    *ports.Operations
	Reader ports.ContentReader
	Writer remoteio.Writer
}

// Context は、パイプライン各ステップ間で引き継がれる実行コンテキストです。
// 埋め込みによるフィールド昇格で pc.Task / pc.Manga のようにフラットに参照できます。
type Context struct {
	State
	Services
}
