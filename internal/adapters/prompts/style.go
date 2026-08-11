package prompts

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shouni/ap-story/assets"
)

// Style は画風プリセット1件です。assets/prompts/styles.json が持ちます。
//
// 台本プロンプトのようなテンプレートではなく JSON なのは、1件が
// 「画風指定（Suffix）」と「その画風で避けたいもの（Negative）」の対で決まるためです。
// 対等な2つの本文をテンプレートの本文とヘッダーに分けて置くと、片方だけ直す事故が起きます。
type Style struct {
	// Name はスタイルモード名（フォームの選択値、state に記録される値）です。
	Name string `json:"name"`
	// Direction / UseWhen / Avoids / Category は選ぶ側への説明です。
	// キー名は姉妹プロジェクト（ap-comp の lyrics/compose モード）に合わせています。
	Direction string `json:"direction"`
	UseWhen   string `json:"use_when"`
	Avoids    string `json:"avoids"`
	Category  string `json:"category"`
	// Suffix は画像生成プロンプトへ足す画風指定です。
	Suffix string `json:"style"`
	// DesignSuffix はデザインシート用の画風指定です。Suffix とは別に持ちます。
	//
	// シートは他の生成物の同一性アンカーなので、演出（cinematic lighting、rim light、
	// フィルム粒状感など）を含む Suffix をそのまま焼くと、その照明が参照経由で
	// 下流の全生成に伝染します。同じ絵柄で、演出だけを落とした文言を書いてください。
	DesignSuffix string `json:"design_style"`
	// Negative はその画風で避けたいものです。共通のネガティブプロンプト
	// （プロンプト実装が持つ構図・文字・破綻の抑制）に足して使います。
	//
	// 画風ごとに持つのは、共通側に "monochrome" のような画風の語を書くと、
	// モノクロを選べるスタイルを足した瞬間に指定同士が真っ向から衝突するためです。
	Negative string `json:"negative"`
}

// Info は選択 UI 向けの説明（台本モードと共通の形）へ変換します。
func (s Style) Info() ModeInfo {
	return ModeInfo{
		Name:      s.Name,
		Direction: s.Direction,
		UseWhen:   s.UseWhen,
		Avoids:    s.Avoids,
		Category:  s.Category,
	}
}

// Styles は画風プリセットの一覧です。順序は JSON の記載順を保ちます
// （選択肢の並びを作者が決められるようにするため）。
type Styles struct {
	list  []Style
	byKey map[string]Style
}

// NewStyles は埋め込みの styles.json を読み込んで Styles を構築します。
// 既定モードの欠落・名前の重複・必須項目の空はここで弾きます（起動時に落とすため）。
func NewStyles() (*Styles, error) {
	data, err := assets.Prompts.ReadFile(assets.StylesJSONPath)
	if err != nil {
		return nil, fmt.Errorf("画風プリセットの読み込みに失敗しました: %w", err)
	}

	var list []Style
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("画風プリセットの解析に失敗しました: %w", err)
	}

	byKey := make(map[string]Style, len(list))
	for _, style := range list {
		switch {
		case strings.TrimSpace(style.Name) == "":
			return nil, fmt.Errorf("画風プリセットに name のない項目があります")
		case strings.TrimSpace(style.Suffix) == "":
			return nil, fmt.Errorf("画風プリセット %q に style がありません", style.Name)
		case strings.TrimSpace(style.DesignSuffix) == "":
			return nil, fmt.Errorf("画風プリセット %q に design_style がありません", style.Name)
		}
		if _, dup := byKey[style.Name]; dup {
			return nil, fmt.Errorf("画風プリセット %q が重複しています", style.Name)
		}
		byKey[style.Name] = style
	}
	if _, ok := byKey[ModeDefault]; !ok {
		return nil, fmt.Errorf("画風プリセットに既定モード %q がありません", ModeDefault)
	}

	return &Styles{list: list, byKey: byKey}, nil
}

// Modes は選択できるスタイルモード名を記載順で返します。フォームの検証に使います。
func (s *Styles) Modes() []string {
	names := make([]string, 0, len(s.list))
	for _, style := range s.list {
		names = append(names, style.Name)
	}
	return names
}

// ModeInfos は選択できるスタイルモードの説明を記載順で返します。フォームの選択肢に使います。
func (s *Styles) ModeInfos() []ModeInfo {
	infos := make([]ModeInfo, 0, len(s.list))
	for _, style := range s.list {
		infos = append(infos, style.Info())
	}
	return infos
}

// resolveStyle は、キットから渡された画風モードを画風指定とネガティブプロンプトへ解決します。
// パネル・ページ・デザインシートで同じ判断をするため、1か所に置いています。
// suffixOf でプリセットのどちらの画風指定を使うかを切り替えます。
//
// styles は必須です。画風の文言はプリセットにしか無いので、未設定なら画風指定なしで
// 生成してしまいます。黙って画風を落とすより、プロンプト構築を失敗させます。
func resolveStyle(styles *Styles, mode, baseNegative string, suffixOf func(Style) string) (suffix, negative string, err error) {
	if styles == nil {
		return "", "", fmt.Errorf("画風プリセットが設定されていません")
	}
	style, err := styles.Get(mode)
	if err != nil {
		return "", "", err
	}
	if style.Negative == "" {
		return suffixOf(style), baseNegative, nil
	}
	return suffixOf(style), baseNegative + ", " + style.Negative, nil
}

// imageSuffix / designSuffix は resolveStyle に渡す画風指定の選び方です。
func imageSuffix(s Style) string  { return s.Suffix }
func designSuffix(s Style) string { return s.DesignSuffix }

// Get はスタイルモード名に対応するプリセットを返します。
// 空文字は既定モードを指します。未知のモードは台本モードと同じくエラーです
// （黙って既定へ落とすと、指定したつもりの画風で生成されていないことに気付けません）。
func (s *Styles) Get(mode string) (Style, error) {
	if mode == "" {
		mode = ModeDefault
	}
	style, ok := s.byKey[mode]
	if !ok {
		return Style{}, fmt.Errorf("未知のスタイルモードです: %s", mode)
	}
	return style, nil
}
