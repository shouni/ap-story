package prompts

import (
	"fmt"
	"strings"

	"github.com/shouni/go-prompt-kit/frontmatter"
	"go.yaml.in/yaml/v3"
)

// ModeInfo は、プロンプトテンプレート先頭の front matter に書かれたモードの説明です。
//
// モード名（ファイル名）だけでは「そのモードが何をするのか」が分からず、選ぶ側が
// テンプレート本文を開くはめになります。フォームの選択肢とエージェント向けの説明を
// テンプレート自身に持たせるための欄で、キー名は姉妹プロジェクト（ap-comp の
// lyrics/compose モード）に合わせています。
//
// front matter が無いテンプレートも許容します（Name だけの ModeInfo になります）。
// 説明はテンプレート作者への強制ではなく、書けば画面に出るという位置づけです。
type ModeInfo struct {
	// Name はモード名（拡張子を除いたファイル名）です。front matter には書きません。
	Name string `yaml:"-"`
	// Direction はそのモードの方向性を一言で表します（選択肢のラベルに使います）。
	Direction string `yaml:"direction"`
	// UseWhen はどんなときに選ぶかです。
	UseWhen string `yaml:"use_when"`
	// Avoids はそのモードが避けること・向かない用途です。
	Avoids string `yaml:"avoids"`
	// Category は選択肢を束ねる表示上のグループ名です（任意）。
	Category string `yaml:"category"`
}

// Label は選択肢に表示する文字列を返します。
func (m ModeInfo) Label() string {
	if m.Direction == "" {
		return m.Name
	}
	return m.Name + " — " + m.Direction
}

// Hint は選択肢の補足説明（title 属性）に使う文字列を返します。
func (m ModeInfo) Hint() string {
	var parts []string
	if m.UseWhen != "" {
		parts = append(parts, "向いている用途: "+m.UseWhen)
	}
	if m.Avoids != "" {
		parts = append(parts, "避けること: "+m.Avoids)
	}
	return strings.Join(parts, " / ")
}

// parseModeInfo は front matter を ModeInfo へ読み取ります。
// front matter が空なら名前だけの ModeInfo を返します。
func parseModeInfo(name, front string) (ModeInfo, error) {
	info := ModeInfo{Name: name}
	if err := frontmatter.Decode(front, &info, yaml.Unmarshal); err != nil {
		return ModeInfo{}, fmt.Errorf("front matter の解析に失敗しました (mode: %s): %w", name, err)
	}
	return info, nil
}
