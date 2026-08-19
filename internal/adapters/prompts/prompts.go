// Package prompts は、ap-story 独自の Outline/ChapterScript/DesignSheet プロンプトを
// go-comic-kit の ports.OutlinePrompt / ports.ChapterScriptPrompt / ports.DesignSheetPrompt
// として実装します。ずんだもん・めたん・つむぎの配役・世界観（SFメカアクション風・技術学習漫画）は
// 姉妹プロジェクトの対話プロンプトの構成を踏襲しています。
package prompts

import (
	"fmt"
	"slices"

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
	// infos は章立てテンプレートの front matter です。台本モードの説明は
	// 章立て側に書きます（1つのモードの説明が2ファイルに割れないようにするため）。
	infos map[string]ModeInfo
}

var (
	_ ports.OutlinePrompt       = (*ScriptPrompts)(nil)
	_ ports.ChapterScriptPrompt = (*ScriptPrompts)(nil)
)

// NewScriptPrompts は埋め込みテンプレートを読み込んで ScriptPrompts を構築します。
//
// 台本モードは章立てと章台本の両方でそのモード名のテンプレートを引くため、
// 片方にしか無いモードは構築時に弾きます。選択肢に出したモードが章の生成に入って
// から「テンプレートが無い」と落ちるのを、起動時に前倒しするためです。
func NewScriptPrompts() (*ScriptPrompts, error) {
	outline, infos, err := loadPrompts(assets.OutlinePromptDir)
	if err != nil {
		return nil, fmt.Errorf("章立てテンプレートの読み込みに失敗しました: %w", err)
	}
	chapter, _, err := loadPrompts(assets.ChapterPromptDir)
	if err != nil {
		return nil, fmt.Errorf("章台本テンプレートの読み込みに失敗しました: %w", err)
	}
	outlineModes, chapterModes := sortedModes(outline), sortedModes(chapter)
	if !slices.Equal(outlineModes, chapterModes) {
		return nil, fmt.Errorf("台本モードが章立てと章台本で一致しません: outline=%v chapter=%v", outlineModes, chapterModes)
	}
	return &ScriptPrompts{outline: outline, chapter: chapter, infos: infos}, nil
}

// Modes は選択できる台本モード名を名前順で返します。フォームの検証に使います。
func (p *ScriptPrompts) Modes() []string {
	return sortedModes(p.outline)
}

// ModeInfos は選択できる台本モードの説明を名前順で返します。フォームの選択肢に使います。
func (p *ScriptPrompts) ModeInfos() []ModeInfo {
	return sortedInfos(p.outline, p.infos)
}

// BuildOutline は章立て生成プロンプトを構築します。
func (p *ScriptPrompts) BuildOutline(mode string, data *ports.OutlinePromptData) (string, error) {
	return execute(p.outline, mode, data)
}

// BuildChapterScript は章単位の台本生成プロンプトを構築します。
func (p *ScriptPrompts) BuildChapterScript(mode string, data *ports.ChapterPromptData) (string, error) {
	return execute(p.chapter, mode, data)
}

// loadPrompts は、埋め込みディレクトリ配下の .md をモード名（拡張子を除いたファイル名）で
// 読み込み、front matter を本文から切り離してテンプレートとモード説明を返します。
//
// WithFrontMatter を付けるのは、front matter がプロンプト本文へ紛れ込まないよう
// テンプレート化の前に切り落とす必要があるためです。切り離した内容は
// Builder.FrontMatter から取り出せるので、読み込みは 1 回で済みます。
func loadPrompts(dir string) (*promptkit.Builder, map[string]ModeInfo, error) {
	builder, err := promptkit.LoadFS(assets.Prompts, dir,
		promptkit.WithExtensions(".md"),
		promptkit.WithFrontMatter(),
	)
	if err != nil {
		return nil, nil, err
	}

	modes := builder.Modes()
	infos := make(map[string]ModeInfo, len(modes))
	for _, mode := range modes {
		info, err := parseModeInfo(mode, builder.FrontMatter(mode))
		if err != nil {
			return nil, nil, err
		}
		infos[mode] = info
	}

	return builder, infos, nil
}

// sortedModes は、読み込み済みテンプレートのモード名を名前順で返します。
// go-prompt-kit の Modes() は登録順なので、画面の並びが埋め込みの走査順に
// 左右されないようここで固定します。
func sortedModes(builder *promptkit.Builder) []string {
	modes := builder.Modes()
	slices.Sort(modes)
	return modes
}

// sortedInfos は、読み込み済みテンプレートのモード説明を名前順で返します。
func sortedInfos(builder *promptkit.Builder, infos map[string]ModeInfo) []ModeInfo {
	modes := sortedModes(builder)
	out := make([]ModeInfo, 0, len(modes))
	for _, mode := range modes {
		info, ok := infos[mode]
		if !ok {
			info = ModeInfo{Name: mode}
		}
		out = append(out, info)
	}
	return out
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
