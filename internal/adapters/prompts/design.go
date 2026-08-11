package prompts

import (
	"fmt"
	"strings"

	"github.com/shouni/go-comic-kit/ports"
)

const (
	// designPromptBaseTemplate はデザインシートプロンプトの基本形です。
	designPromptBaseTemplate = "Masterpiece character design sheet of %s"

	// designLayoutMultiView は既定のターンアラウンド（前・横・後の3面図）レイアウトです。
	// 「同一スケール・共通の接地線・1体ずつ完結した全身」を明示し、体がパーツ分割されたり
	// ビュー間で分裂・融合したりする生成崩れを抑えます。
	designLayoutMultiView = "a character turnaround with exactly three views of the same character — front view, side view, and back view — each view standing full body in a neutral A-pose with arms held slightly away from the body so the costume stays fully visible, the three views placed side by side at identical scale on a shared ground line, each view drawn as one complete connected figure from head to toe"
	// designLayoutMultiViewMultiChar は複数キャラクター合成シート用の3面図レイアウトです。
	// キャラクターごとにビューを1つのグループにまとめさせ、キャラクター間の特徴の
	// 混同・融合を明示的に禁止します。
	designLayoutMultiViewMultiChar = "for every character exactly three views — front view, side view, and back view — standing full body in a neutral A-pose with arms held slightly away from the body so the costume stays fully visible, each character's three views grouped together in its own horizontal row, all figures at identical scale on a shared ground line, each view drawn as one complete connected figure from head to toe, never blending or mixing features between different characters"
	// designLayoutSingleView は、他の生成物（パネル、カバーアート等）の参照アンカーとして
	// 使うための、単一ポーズ・正面向きのレイアウトです。
	designLayoutSingleView = "single view, front-facing, standing full body in a neutral relaxed pose, centered composition, the entire body from head to toe inside the frame"
	// designLayoutSingleViewMultiChar は複数キャラクター合成シート用の単一ポーズレイアウトです。
	designLayoutSingleViewMultiChar = "single view, front-facing, all characters standing side by side full body in a neutral relaxed pose at identical scale on a shared ground line, centered composition, every body entirely inside the frame from head to toe, never blending or mixing features between different characters"

	designLayoutPromptFormat = "Layout: %s"

	// designSystemPrompt は、ap-story の SFメカアクション風・技術学習漫画に登場する
	// ずんだもん・めたん・つむぎのデザインシート生成でモデルへ与えるシステム指示です。
	// 生成物はパネル・ページ合成など他ワークフローのキャラクター同一性アンカーとして
	// 参照されるため、演出的な絵作りよりも正確さ・一貫性を最優先させます。
	designSystemPrompt = `You are a professional character designer creating official model sheets for a sci-fi mecha-action tech-learning manga starring Zundamon, Metan, and Tsumugi.
This sheet is the canonical identity reference that every downstream panel, page, and cover art will rely on, so accuracy and consistency outweigh artistic flair:
- One or more reference images are attached for each subject. Treat each attached reference image as the ground truth for that character's identity — strictly preserve its hairstyle, hair color, eye color, skin tone, outfit silhouette, colors, and accessories. The text description supplements the reference image; it does not override or replace it.
- Anatomical correctness is critical. Draw every hand with exactly five fingers, correct limb proportions, and clean readable silhouettes.
- Draw each figure as one complete, physically connected body: head, torso, both arms, and both legs attached in a single continuous silhouette. Never split a figure into pieces, and never add close-up detail insets, detached limbs, floating heads, or partial bodies.
- Every view of a character must depict the SAME character with identical hairstyle, hair color, eye color, skin tone, outfit, and accessories — including any signature props or gadgets.
- Use flat, even, neutral studio lighting only. No dramatic shadows, rim light, lens flares, or color grading — lighting baked into this sheet contaminates every downstream generation that references it.
- The full body must be visible from head to toe and must never be cropped by the frame.
- Render absolutely no text, labels, arrows, color swatches, logos, or annotations of any kind.`

	// designNegativePrompt はデザインシートに含めたくない要素の指定です。
	// 注意: gemini-image-kit はこれを負条件付けとしてではなく "[Negative Prompt]" 見出し付きの
	// 平文としてプロンプト末尾に連結するだけなので、"extra limbs" や "fused fingers" のような
	// 欠陥語彙を並べると通常のプロンプトトークンとして作用し、かえってその崩れを誘発します。
	// そのため解剖学的な品質はシステムプロンプト側で肯定形で指示し、ここでは
	// 指示追従モデルが解釈できる「含めてはならない内容物」の列挙のみに留めます。
	designNegativePrompt = "Do not include any of the following in the image: text, letters, labels, annotations, arrows, diagrams, color swatches, watermarks, logos, signatures, speech bubbles, background scenery or background objects, extra duplicate figures, close-up detail insets, dramatic lighting, strong shadows, rim light, lens flare, color grading, blur"
)

// DesignPrompt は、ap-story 独自のデザインシートプロンプト実装です。
// go-comic-kit 内蔵の prompts.DefaultDesignPrompt と同等の技術的厳密さ（キャラクター同一性・
// 指の解剖学的正確さ・フラット照明）を保ちつつ、ap-story の作品世界向けに文言を独自管理します。
type DesignPrompt struct {
	// Styles は画風プリセットです。シートはパネル用の画風ではなく、
	// 演出を落としたシート用の指定（Style.DesignSuffix）を使います。
	Styles *Styles
}

var _ ports.DesignSheetPrompt = DesignPrompt{}

// BuildDesignSheet はキャラクターデザインシート生成用のシステム/ユーザー/ネガティブプロンプトを
// 構築します。data.Layout に ports.DesignLayoutSingleView を渡すと単一ポーズレイアウトになります。
func (p DesignPrompt) BuildDesignSheet(data *ports.DesignSheetPromptData) (systemPrompt, userPrompt, negativePrompt string, err error) {
	if data == nil || len(data.Descriptions) == 0 {
		return "", "", "", fmt.Errorf("description is required to build a design sheet prompt")
	}

	styleSuffix, negative, err := resolveStyle(p.Styles, data.StyleMode, designNegativePrompt, designSuffix)
	if err != nil {
		return "", "", "", err
	}

	numChars := len(data.Descriptions)
	var subjects string
	if numChars > 1 {
		subjectParts := make([]string, numChars)
		for i, d := range data.Descriptions {
			subjectParts[i] = fmt.Sprintf("[Subject %d: %s]", i+1, d)
		}
		subjects = fmt.Sprintf("%d DIFFERENT characters: %s", numChars, strings.Join(subjectParts, " "))
	} else {
		subjects = data.Descriptions[0]
	}

	base := fmt.Sprintf(designPromptBaseTemplate, subjects)
	layoutPrompt := fmt.Sprintf(designLayoutPromptFormat, designLayout(numChars, data.Layout))

	promptParts := []string{base, layoutPrompt}
	if styleSuffix != "" {
		promptParts = append(promptParts, styleSuffix)
	}
	// 画風指定に演出用の語が紛れ込んでも、参照アンカーとしての制約
	// （フラットな照明・白背景・手の正確さ）を後置して優先させる。
	promptParts = append(promptParts,
		"plain uniform white studio background",
		"flat even neutral lighting",
		"sharp focus",
		"perfectly drawn hands with five fingers per hand",
	)

	userPrompt = strings.Join(promptParts, ", ")
	return designSystemPrompt, userPrompt, negative, nil
}

// designLayout は被写体数とレイアウト指定に応じたレイアウト文を返します。
// 複数キャラクターの合成シートでは「same character」の3面図文がそのまま使われると
// 被写体指定（N DIFFERENT characters）と矛盾して融合・分裂を誘発するため、
// キャラクターごとのグループ化を指示する専用の文言に切り替えます。
func designLayout(numChars int, layout string) string {
	if layout == ports.DesignLayoutSingleView {
		if numChars > 1 {
			return designLayoutSingleViewMultiChar
		}
		return designLayoutSingleView
	}
	if numChars > 1 {
		return designLayoutMultiViewMultiChar
	}
	return designLayoutMultiView
}
