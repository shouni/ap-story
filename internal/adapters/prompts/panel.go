package prompts

import (
	"fmt"
	"strings"

	"github.com/shouni/go-comic-kit/comic"

	"github.com/shouni/go-comic-kit/ports"
)

const (
	// panelSystemPrompt はパネル画像生成時にモデルへ与えるシステム指示です。
	// 添付される参照画像とプロンプト内の [Subject N] は同じ順序で対応します。
	panelSystemPrompt = `You are a professional manga panel illustrator.
Draw a single manga panel following the scene direction, with these rules:
- Attached reference images correspond to [Subject N] in the prompt, in the same order. Strictly preserve each subject's identity from its reference image: hairstyle, hair color, eye color, outfit, and accessories. Never mix identities between subjects.
- Translate each subject's stated emotion and action into expression, gaze, and pose. Place subjects according to their stated positions.
- Anatomical correctness is critical: draw every hand with exactly five fingers and correct limb proportions.
- Render absolutely no text, speech bubbles, sound effects lettering, or logos — dialogue is composited separately.`

	// panelNegativePrompt はパネル画像に含めたくない要素を指定する負のプロンプトです。
	// フキダシ・文字の排除に加え、手・指の崩れ対策を含みます。
	//
	// 画風に依る語（monochrome 等）はここに書きません。スタイルプリセット側が
	// negative として持ち、選ばれた画風のものだけが足されます。ここに書くと、
	// モノクロを選べるスタイルと真っ向から衝突します。
	panelNegativePrompt = "speech bubble, dialogue balloon, text, alphabet, letters, words, signatures, watermark, username, malformed hands, fused fingers, extra fingers, missing fingers, extra limbs, deformed anatomy, low quality, distorted, bad anatomy"

	// panelEditInstruction は編集モードで構図の維持を指示する共通プレフィックスです。
	panelEditInstruction = "Edit the attached manga panel image. Keep the composition, character poses, background, and art style unchanged. Apply ONLY this change: "
)

// PanelPrompt は ap-story のパネル画像生成プロンプトです。
//
// go-comic-kit の既定は形式を守らせる最小限の指示しか持たないため、演出語彙や
// 画風の作り込みはこちら側で持ちます。
type PanelPrompt struct {
	// Styles は画風プリセットです。キットが運ぶのはモード名だけなので、
	// 画風指定とネガティブプロンプトの解決はここで行います。
	Styles *Styles
}

var _ ports.PanelPrompt = PanelPrompt{}

// BuildPanel はパネル画像生成のプロンプトを構築します。
func (p PanelPrompt) BuildPanel(data *ports.PanelPromptData) (string, string, string, error) {
	if data == nil {
		return "", "", "", fmt.Errorf("panel prompt data is required")
	}
	styleSuffix, negative, err := resolveStyle(p.Styles, data.StyleMode, panelNegativePrompt, imageSuffix)
	if err != nil {
		return "", "", "", err
	}

	var sb strings.Builder
	sb.WriteString("Manga panel illustration.")
	if data.Panel.Shot != "" {
		fmt.Fprintf(&sb, " Shot: %s.", data.Panel.Shot)
	}
	if data.Panel.Setting != "" {
		fmt.Fprintf(&sb, " Setting: %s.", data.Panel.Setting)
	}
	if anchor := strings.TrimSpace(data.Panel.VisualAnchor); anchor != "" {
		sb.WriteString("\nScene direction: ")
		sb.WriteString(anchor)
	}

	// [Subject N] の N は参照画像の添付順（SubjectIDs の並び）と一致させる。
	// ずれるとモデルは別人の参照画像を見ながら描くことになる。
	for i, id := range data.SubjectIDs {
		char := data.Characters.GetCharacter(id)
		if char == nil {
			continue
		}
		sb.WriteString("\n")
		sb.WriteString(subjectLine(i+1, char, findPanelCharacter(&data.Panel, id)))
	}

	if extras := backgroundExtras(&data.Panel); extras != "" {
		sb.WriteString("\nBackground extras (no reference, generic): ")
		sb.WriteString(extras)
	}
	if styleSuffix != "" {
		sb.WriteString("\nStyle: ")
		sb.WriteString(styleSuffix)
	}
	sb.WriteString("\nNo speech bubbles, no text.")

	return panelSystemPrompt, sb.String(), negative, nil
}

// BuildPanelEdit は既存パネル画像への編集指示を構築します。
// 構図・ポーズ・背景を保ったまま、指示された箇所だけを変更させます。
//
// 編集は既存画像の画風を引き継ぐため、画風プリセットのネガティブは足しません。
func (PanelPrompt) BuildPanelEdit(editPrompt string) (string, string, string, error) {
	return panelSystemPrompt, panelEditInstruction + editPrompt, panelNegativePrompt, nil
}

// subjectLine は1キャラクター分の [Subject N] 記述を構築します。
func subjectLine(index int, char *comic.Character, pc *comic.PanelCharacter) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[Subject %d: %s", index, char.Name)
	if len(char.VisualCues) > 0 {
		fmt.Fprintf(&sb, " (%s)", strings.Join(char.VisualCues, ", "))
	}
	sb.WriteString("]")
	if pc == nil {
		return sb.String()
	}
	if pc.Emotion != "" {
		fmt.Fprintf(&sb, " emotion: %s.", pc.Emotion)
	}
	if pc.Action != "" {
		fmt.Fprintf(&sb, " action: %s.", pc.Action)
	}
	if pc.Position != "" {
		fmt.Fprintf(&sb, " position: %s.", pc.Position)
	}
	return sb.String()
}

// findPanelCharacter はパネル内のキャラクター指定を ID で引きます。
func findPanelCharacter(panel *comic.Panel, charID string) *comic.PanelCharacter {
	for i := range panel.Characters {
		if panel.Characters[i].CharacterID == charID {
			return &panel.Characters[i]
		}
	}
	return nil
}

// backgroundExtras は background（モブ）キャラクターの一覧を返します。
func backgroundExtras(panel *comic.Panel) string {
	var extras []string
	for i := range panel.Characters {
		pc := &panel.Characters[i]
		if pc.Prominence != comic.ProminenceBackground {
			continue
		}
		extras = append(extras, backgroundExtraDesc(pc))
	}
	return strings.Join(extras, ", ")
}

// backgroundExtraDesc は background キャラクター1人分の記述を構築します。
func backgroundExtraDesc(pc *comic.PanelCharacter) string {
	desc := pc.CharacterID
	if pc.Action != "" {
		desc += " (" + pc.Action + ")"
	}
	return desc
}
