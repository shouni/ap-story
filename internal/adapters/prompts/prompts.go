// Package prompts は、ap-story 独自の Outline/ChapterScript/DesignSheet プロンプトを
// go-comic-kit の ports.OutlinePrompt / ports.ChapterScriptPrompt / ports.DesignSheetPrompt
// として実装します。ずんだもん・めたん・つむぎの配役・世界観（SFメカアクション風・技術学習漫画）は
// ap-comp の assets/prompts/prompt_dialogue.md を踏襲しています。
package prompts

import (
	"fmt"

	"github.com/shouni/go-comic-kit/ports"
	promptkit "github.com/shouni/go-prompt-kit/prompts"

	"github.com/shouni/ap-story/assets"
)

// ModeDefault は既定のプロンプトモード名です。
const ModeDefault = "default"

// ScriptPrompts は、ap-story 独自の Outline/ChapterScript プロンプトです。
type ScriptPrompts struct {
	outline *promptkit.Builder
	chapter *promptkit.Builder
}

var (
	_ ports.OutlinePrompt       = (*ScriptPrompts)(nil)
	_ ports.ChapterScriptPrompt = (*ScriptPrompts)(nil)
)

// NewScriptPrompts は埋め込みテンプレートを読み込んで ScriptPrompts を構築します。
func NewScriptPrompts() (*ScriptPrompts, error) {
	outline, err := loadPrompts(assets.OutlinePromptDir)
	if err != nil {
		return nil, fmt.Errorf("章立てテンプレートの読み込みに失敗しました: %w", err)
	}
	chapter, err := loadPrompts(assets.ChapterPromptDir)
	if err != nil {
		return nil, fmt.Errorf("章台本テンプレートの読み込みに失敗しました: %w", err)
	}
	return &ScriptPrompts{outline: outline, chapter: chapter}, nil
}

// BuildOutline は章立て生成プロンプトを構築します。
func (p *ScriptPrompts) BuildOutline(mode string, data *ports.OutlinePromptData) (string, error) {
	return execute(p.outline, mode, data)
}

// BuildChapterScript は章単位の台本生成プロンプトを構築します。
func (p *ScriptPrompts) BuildChapterScript(mode string, data *ports.ChapterPromptData) (string, error) {
	return execute(p.chapter, mode, data)
}

// loadPrompts は、埋め込みディレクトリ配下の .md をモード名（拡張子を除いたファイル名）で読み込みます。
// 該当ファイルが無い場合は go-prompt-kit 側がエラーを返します。
func loadPrompts(dir string) (*promptkit.Builder, error) {
	return promptkit.LoadFS(assets.Prompts, dir, "", promptkit.WithExtensions(".md"))
}

// execute はモード未指定を既定モードへ寄せたうえでプロンプトを構築します。
// 未知のモードは既定へフォールバックさせずエラーにします。モード名の指定ミスを
// 既定プロンプトとして黙って通さないためで、go-prompt-kit の WithDefaultMode は
// 意図的に使っていません。
func execute(builder *promptkit.Builder, mode string, data any) (string, error) {
	if mode == "" {
		mode = ModeDefault
	}
	out, err := builder.Build(mode, data)
	if err != nil {
		return "", fmt.Errorf("プロンプトの構築に失敗しました (mode: %s): %w", mode, err)
	}
	return out, nil
}
