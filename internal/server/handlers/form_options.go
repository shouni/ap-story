package handlers

import (
	"fmt"
	"slices"
	"strings"

	"github.com/shouni/ap-story/internal/adapters/prompts"
)

// selectOption はフォームの <select> 1項目です。
// モデル選択（GEMINI_MODELS / IMAGE_MODELS）とプロンプトモード選択
// （台本モード・スタイルモード）で共有します。
type selectOption struct {
	Value string
	Label string
	// Hint は選択肢の補足説明（title 属性）です。空なら属性ごと出しません。
	Hint     string
	Selected bool
}

// modelOptions はモデル名の一覧を選択肢に変換します。先頭が既定モデルです。
// 用途ごとの使い分けは持たないので、一覧をそのまま選択肢にします。
//
// 空文字（既定モデルを使う）の選択肢はテンプレート側が持つため、ここには含めません。
func modelOptions(models []string, selected string) []selectOption {
	options := make([]selectOption, 0, len(models))
	for i, model := range models {
		label := model
		if i == 0 {
			label = "既定: " + model
		}
		options = append(options, selectOption{
			Value:    model,
			Label:    label,
			Selected: model == selected,
		})
	}
	return options
}

// modeOptions はプロンプトモードの一覧を選択肢に変換します。ラベルと補足説明は
// テンプレート先頭の front matter（prompts.ModeInfo）から取ります。
//
// 未選択（空文字）は既定モードを選んだものとして扱います。ワーカー側も空文字を
// 既定モードへ寄せるため、画面と実際の生成で選択が食い違わないようにするためです。
func modeOptions(modes []prompts.ModeInfo, selected string) []selectOption {
	if selected == "" {
		selected = prompts.ModeDefault
	}
	options := make([]selectOption, 0, len(modes))
	for _, mode := range modes {
		label := mode.Label()
		if mode.Name == prompts.ModeDefault {
			label += "（既定）"
		}
		options = append(options, selectOption{
			Value:    mode.Name,
			Label:    label,
			Hint:     mode.Hint(),
			Selected: mode.Name == selected,
		})
	}
	return options
}

// modeNames はモード一覧から名前だけを取り出します。選択値の検証に使います。
func modeNames(modes []prompts.ModeInfo) []string {
	names := make([]string, 0, len(modes))
	for _, mode := range modes {
		names = append(names, mode.Name)
	}
	return names
}

// validateAllowed は、指定された値が許可リストに含まれるかを確かめます。
// 空文字は「既定を使う」意味なので常に有効です。
//
// domain.Task の検証ではなくここに置くのは、許可リストが env とテンプレート由来で
// ドメイン層が知らないためです。ブラウザは <select> の選択肢に縛られますが、
// JSON API は任意の文字列を送れます。
func validateAllowed(kind, value string, allowed []string) error {
	value = strings.TrimSpace(value)
	if value == "" || slices.Contains(allowed, value) {
		return nil
	}
	return fmt.Errorf("不正な%sです: %s", kind, value)
}
