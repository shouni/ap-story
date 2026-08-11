// Package pipeline は、domain.Task のコマンドに応じて go-comic-kit の操作を
// 順に実行する Worker パイプラインを提供します。
package pipeline

import (
	"github.com/shouni/go-comic-kit/comic"
	"github.com/shouni/go-comic-kit/ports"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/domain"
)

// State は、パイプライン実行中に変化していく値です。
type State struct {
	Task *domain.Task
	// Manga は実行対象の MangaState です。ステップが読み書きします。
	// compose_comic の最初のステップ（Outline）でのみ nil から新規作成されます。
	Manga *comic.MangaState
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

// textModel は、この実行で台本生成に使うテキストモデルを返します。
// Task の指定を優先し、無ければ state の記録（この作品を書いたモデル）を引き継ぎます。
// どちらも空なら空文字を返し、worker の設定（GEMINI_MODELS 先頭）が使われます。
//
// 引き継ぐのは、章の台本を後から作り直したときに他の章と違うモデルが書くのを防ぐためです。
func (pc *Context) textModel() string {
	if pc.Task != nil && pc.Task.TextModel != "" {
		return pc.Task.TextModel
	}
	if pc.Manga != nil {
		return pc.Manga.TextModel
	}
	return ""
}

// imageModel は、この実行で画像生成に使うモデルを返します（解決規則は textModel と同じ）。
func (pc *Context) imageModel() string {
	if pc.Task != nil && pc.Task.ModelOverride != "" {
		return pc.Task.ModelOverride
	}
	if pc.Manga != nil {
		return pc.Manga.ImageModel
	}
	return ""
}

// styleMode は、この実行で使う画風モードを返します。
// 画風は作品単位の絵作りなので、Task ではなく state（章立て時に決めた値）を見ます。
// これで「台本まで生成 → 後から画像生成」でも同じ画風が使われます。
//
// 画風指定とネガティブプロンプトへの解決は、プロンプト実装
// （internal/adapters/prompts の Styles）が行います。パイプラインは名前を運ぶだけです。
func (pc *Context) styleMode() string {
	if pc.Manga == nil {
		return ""
	}
	return pc.Manga.StyleMode
}

// Context は、パイプライン各ステップ間で引き継がれる実行コンテキストです。
// 埋め込みによるフィールド昇格で pc.Task / pc.Manga のようにフラットに参照できます。
type Context struct {
	State
	Services
}
