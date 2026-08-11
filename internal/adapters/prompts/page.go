package prompts

import (
	"fmt"
	"strings"

	"github.com/shouni/go-comic-kit/comic"

	"github.com/shouni/go-comic-kit/ports"
)

const (
	// pageSystemPrompt はページ合成時にモデルへ与えるシステム指示です。
	// パネル数・レイアウトの厳守と、参照画像とのキャラクター同一性を最優先させます。
	pageSystemPrompt = `You are a master digital manga artist. You MUST follow the exact panel count and layout rules. Character identity MUST match the character master reference files.

### FORMAT RULES ###
- STYLE: Follow the ARTISTIC STYLE section below exactly. It decides colour and rendering.
- RENDERING: Sharp clean lineart with a professional finish.
- LAYOUT: Strict multi-panel composition. Use ONLY the specified number of panels.
- NO FILLER: Do not add extra panels or decorative small frames. Fill the page with the given count.
- BORDERS: Deep black, crisp frame borders for EVERY panel.
- GUTTERS: Pure white space between panels.
- READING FLOW: Right-to-Left, Top-to-Bottom.`

	// pageNegativePrompt はページ画像に含めたくない要素を指定する負のプロンプトです。
	// パネル数の暴走・手の崩れ・透かしなど、画風によらないものだけを並べます。
	//
	// 彩色の指定（monochrome / screentone 等の排除）はスタイルプリセットの negative が
	// 持ちます。ここでフルカラーを強制すると、モノクロのスタイルを選べなくなります。
	pageNegativePrompt = "watermark, signature, deformed faces, bad anatomy, disfigured, poorly drawn hands, extra fingers, missing fingers, extra panels, unexpected panels, more than specified panels, split panels"

	// pageEditInstruction は編集モードでレイアウトの維持を指示する共通プレフィックスです。
	pageEditInstruction = "Edit the attached manga page image. Keep the panel layout, compositions, dialogue balloons, and art style unchanged. Apply ONLY this change: "
)

// PagePrompt は ap-story のページ合成プロンプトです。
// コマ割り・フキダシ・写植の指示を含む、作品固有の作り込みを持ちます。
type PagePrompt struct {
	// Styles は画風プリセットです（PanelPrompt.Styles と同じ役割）。
	Styles *Styles
}

var _ ports.PagePrompt = PagePrompt{}

// BuildPage はページ合成のプロンプトを構築します。
func (p PagePrompt) BuildPage(data *ports.PagePromptData) (string, string, string, error) {
	if data == nil {
		return "", "", "", fmt.Errorf("page prompt data is required")
	}

	styleSuffix, negative, err := resolveStyle(p.Styles, data.StyleMode, pageNegativePrompt, imageSuffix)
	if err != nil {
		return "", "", "", err
	}

	var sb strings.Builder
	numPanels := len(data.Panels)

	sb.WriteString("# PAGE PRODUCTION REQUEST\n")
	sb.WriteString("- OUTPUT: ONE single portrait manga page image.\n")
	// 彩色はスタイルプリセットの指定（ART STYLE として後置される）に従わせます。
	// ここでフルカラーを言い切ると、モノクロのスタイルと真逆の指示になります。
	sb.WriteString("- COLOR: Follow the ARTISTIC STYLE instruction exactly.\n")
	fmt.Fprintf(&sb, "- PANEL COUNT: [ %d ] (STRICTLY ONLY %d PANELS. DO NOT ADD ANY MORE).\n\n", numPanels, numPanels)

	writeLayoutStructure(&sb, numPanels)
	writeCharacterReferences(&sb, data)
	writePanelBreakdown(&sb, data)

	return p.systemPrompt(styleSuffix), strings.TrimRight(sb.String(), "\n"), negative, nil
}

// BuildPageEdit は既存ページ画像への編集指示を構築します。
func (p PagePrompt) BuildPageEdit(editPrompt string) (string, string, string, error) {
	return pageSystemPrompt, pageEditInstruction + editPrompt, pageNegativePrompt, nil
}

// systemPrompt はスタイル指定を後置したシステムプロンプトを返します。
func (PagePrompt) systemPrompt(styleSuffix string) string {
	if styleSuffix == "" {
		return pageSystemPrompt
	}
	return pageSystemPrompt + "\n\n### ARTISTIC STYLE ###\n" + styleSuffix
}

// writeLayoutStructure は右開き・2列グリッドのパネル配置マップを出力します。
// パネル数が奇数の場合、最後のパネルは下段の全幅（見せゴマ）にします。
func writeLayoutStructure(sb *strings.Builder, numPanels int) {
	sb.WriteString("## MANDATORY PAGE STRUCTURE\n")
	sb.WriteString("- READING ORDER: Japanese Style (Right-to-Left, then Top-to-Bottom).\n")
	sb.WriteString("- PANEL PLACEMENT MAP:\n")

	if numPanels == 1 {
		sb.WriteString("  * PANEL 1: SINGLE FULL-PAGE PANEL (covers entire image area).\n")
	} else {
		for i := range numPanels {
			if numPanels%2 == 1 && i == numPanels-1 {
				fmt.Fprintf(sb, "  * PANEL %d: BOTTOM ROW, FULL-WIDTH.\n", i+1)
				continue
			}
			side := "RIGHT"
			if i%2 == 1 {
				side = "LEFT"
			}
			fmt.Fprintf(sb, "  * PANEL %d: ROW %d, %s column.\n", i+1, i/2+1, side)
		}
	}
	sb.WriteString("- FRAME STYLE: Deep black borders. GUTTERS: Pure white.\n\n")
}

// writeCharacterReferences はキャラクターマスター参照の一覧を出力します。
// input_file の番号は CharacterFile（実際の添付順）に従います。
func writeCharacterReferences(sb *strings.Builder, data *ports.PagePromptData) {
	if len(data.CharacterFile) == 0 {
		return
	}
	sb.WriteString("## CHARACTER MASTER REFERENCES\n")
	seen := make(map[string]struct{})
	for _, panel := range data.Panels {
		for _, id := range panel.ReferencedCharacterIDs() {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			idx, ok := data.CharacterFile[id]
			if !ok {
				continue
			}
			char := data.Characters.GetCharacter(id)
			if char == nil {
				continue
			}
			cues := "vivid anime color palette"
			if len(char.VisualCues) > 0 {
				cues = strings.Join(char.VisualCues, ", ")
			}
			fmt.Fprintf(sb, "- SUBJECT [%s]: Match input_file_%d. Traits: {%s}.\n", char.Name, idx, cues)
		}
	}
	sb.WriteString("\n")
}

// writePanelBreakdown はパネルごとの内訳（配置・演出・登場キャラ・セリフ）を出力します。
func writePanelBreakdown(sb *strings.Builder, data *ports.PagePromptData) {
	numPanels := len(data.Panels)
	sb.WriteString("## PANEL BREAKDOWN\n")
	for i := range data.Panels {
		panel := &data.Panels[i]
		writePanelHeader(sb, i, numPanels)
		writePanelScene(sb, panel, data)
		writePanelCharacters(sb, panel, data)
		writePanelDialogues(sb, panel, data.Characters)
		sb.WriteString("\n")
	}
}

// writePanelHeader はパネルの見出しと配置指示を出力します。
func writePanelHeader(sb *strings.Builder, index, numPanels int) {
	switch {
	case numPanels == 1:
		fmt.Fprintf(sb, "### PANEL 1 [FULL-PAGE]\n- POSITION: Entire page area\n")
	case numPanels%2 == 1 && index == numPanels-1:
		fmt.Fprintf(sb, "### PANEL %d [FULL-WIDTH IMPACT]\n", index+1)
		sb.WriteString("- POSITION: Bottom row, covering the entire width of the page\n")
		sb.WriteString("- COMPOSITION: Cinematic wide shot, high impact focus.\n")
	default:
		side := "RIGHT"
		if index%2 == 1 {
			side = "LEFT"
		}
		fmt.Fprintf(sb, "### PANEL %d [Standard]\n- POSITION: Row %d, %s column\n", index+1, index/2+1, side)
	}
}

// writePanelScene はシーン演出と構図ガイド（生成済みパネル画像）を出力します。
func writePanelScene(sb *strings.Builder, panel *comic.Panel, data *ports.PagePromptData) {
	if panel.Shot != "" {
		fmt.Fprintf(sb, "- SHOT: %s\n", panel.Shot)
	}
	if panel.Setting != "" {
		fmt.Fprintf(sb, "- SETTING: %s\n", panel.Setting)
	}
	if anchor := strings.TrimSpace(panel.VisualAnchor); anchor != "" {
		fmt.Fprintf(sb, "- SCENE: %s\n", anchor)
	}
	if idx, ok := data.PanelFile[panel.ID]; ok {
		fmt.Fprintf(sb, "- COMPOSITION_GUIDE: Recreate the composition, posing, and background from input_file_%d inside this panel.\n", idx)
	}
}

// writePanelCharacters は登場キャラクターの同一性・演出指示を出力します。
func writePanelCharacters(sb *strings.Builder, panel *comic.Panel, data *ports.PagePromptData) {
	for i := range panel.Characters {
		pc := &panel.Characters[i]
		if pc.Prominence == comic.ProminenceBackground {
			fmt.Fprintf(sb, "- BACKGROUND_EXTRA: %s (generic, no reference)\n", backgroundExtraDesc(pc))
			continue
		}
		char := data.Characters.GetCharacter(pc.CharacterID)
		if char == nil {
			continue
		}
		if idx, ok := data.CharacterFile[pc.CharacterID]; ok {
			fmt.Fprintf(sb, "- CHARACTER_IDENTITY: [ %s ] from input_file_%d. (Face, hair, and outfit MUST match input_file_%d exactly).\n", char.Name, idx, idx)
		} else {
			fmt.Fprintf(sb, "- SUBJECT: %s\n", char.Name)
		}
		var traits []string
		if pc.Emotion != "" {
			traits = append(traits, "emotion: "+pc.Emotion)
		}
		if pc.Action != "" {
			traits = append(traits, "action: "+pc.Action)
		}
		if pc.Position != "" {
			traits = append(traits, "position: "+pc.Position)
		}
		if len(traits) > 0 {
			fmt.Fprintf(sb, "  - DIRECTION: %s\n", strings.Join(traits, " / "))
		}
	}
}

// writePanelDialogues はセリフ・ナレーション・SFX の描画指示を kind 別に出力します。
//
// 同じコマに複数の発話があるときは読む順序を明示します。順序を言わないと吹き出しの
// 配置が絵の都合で決まり、掛け合いが逆順に読めてしまいます。
func writePanelDialogues(sb *strings.Builder, panel *comic.Panel, characters *comic.Characters) {
	if countSpokenLines(panel) > 1 {
		sb.WriteString("- BALLOON ORDER: Place the balloons below in the listed order, read right-to-left then top-to-bottom within this panel.\n")
	}
	for _, line := range panel.Dialogues {
		text := strings.TrimSpace(line.Text)
		if text == "" {
			continue
		}
		switch line.Kind {
		case comic.DialogueKindNarration:
			sb.WriteString("- NARRATION: Rectangular caption box.\n")
		case comic.DialogueKindThought:
			fmt.Fprintf(sb, "- THOUGHT: Cloud-shaped thought bubble for [%s].\n", speakerName(characters, line.SpeakerID))
		case comic.DialogueKindShout:
			fmt.Fprintf(sb, "- SHOUT: Jagged, explosive speech bubble for [%s].\n", speakerName(characters, line.SpeakerID))
		case comic.DialogueKindSFX:
			sb.WriteString("- SFX: Stylized sound-effect lettering integrated into the artwork.\n")
		default:
			// SpeakerID が空のセリフはナレーション/キャプション扱い（comic.DialogueLine 参照）
			if strings.TrimSpace(line.SpeakerID) == "" {
				sb.WriteString("- NARRATION: Rectangular caption box.\n")
			} else {
				fmt.Fprintf(sb, "- SPEECH: Speech bubble for [%s].\n", speakerName(characters, line.SpeakerID))
			}
		}
		fmt.Fprintf(sb, "  - TEXT_TO_RENDER: %q\n", text)

		direction := "Vertical (Tategaki)"
		layoutDesc := "traditional Japanese manga style layout"
		// 短い叫びなどはインパクト重視で「横書き」も許可する
		if len([]rune(text)) <= 10 && strings.ContainsAny(text, "!?！？") {
			direction = "Horizontal (Yokogaki) or Vertical"
			layoutDesc = "bold and high impact placement"
		}
		fmt.Fprintf(sb, "  - TEXT_DIRECTION: %s\n", direction)
		fmt.Fprintf(sb, "  - TYPOGRAPHY: Use professional Japanese manga font (Gothic/Mincho). %s.\n", layoutDesc)
		sb.WriteString("  - LANGUAGE: Japanese characters. Ensure accurate rendering of Kanji/Kana.\n")
	}
}

// countSpokenLines は、そのコマの空でない発話の数を返します。
func countSpokenLines(panel *comic.Panel) int {
	n := 0
	for _, line := range panel.Dialogues {
		if strings.TrimSpace(line.Text) != "" {
			n++
		}
	}
	return n
}

// speakerName は話者IDから表示名を引きます。未知IDは空文字列です。
func speakerName(characters *comic.Characters, speakerID string) string {
	if speakerID == "" || characters == nil {
		return ""
	}
	if char := characters.GetCharacter(speakerID); char != nil {
		return char.Name
	}
	return ""
}
