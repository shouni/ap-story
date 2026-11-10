package prompts

import (
	"fmt"
	"strings"

	"github.com/shouni/go-comic-kit/comic"

	"github.com/shouni/go-comic-kit/ports"
)

const (
	// pageSystemPrompt is the system instruction given to the model at page composition.
	// It puts strict adherence to the panel count and layout, and character identity with the reference images, first.
	pageSystemPrompt = `You are a master digital manga artist. You MUST follow the exact panel count and layout rules. Character identity MUST match the character master reference files.

### FORMAT RULES ###
- STYLE: Follow the ARTISTIC STYLE section below exactly. It decides colour and rendering.
- RENDERING: Sharp clean lineart with a professional finish.
- LAYOUT: The page is split into horizontal tiers. Panels never straddle a tier boundary.
- PANEL SIZE: Tier heights and panel widths are given as percentages of the page. Honour them exactly — never fall back to an even grid.
- NO FILLER: Do not add extra panels or decorative small frames. Fill the page with the given count.
- BORDERS: Deep black, crisp frame borders for EVERY panel.
- GUTTERS: Pure white space between panels.
- READING FLOW: Right-to-Left, Top-to-Bottom.`

	// pageNegativePrompt is the negative prompt listing what the page image should not contain.
	// It lists only things independent of art style, such as runaway panel counts, broken hands and watermarks.
	//
	// Colouring instructions (excluding monochrome / screentone and so on) belong to the style preset's
	// negative. Forcing full colour here would make monochrome styles impossible to choose.
	pageNegativePrompt = "watermark, signature, deformed faces, bad anatomy, disfigured, poorly drawn hands, extra fingers, missing fingers, extra panels, unexpected panels, more than specified panels, split panels"

	// pageEditInstruction is the shared prefix that instructs keeping the layout in edit mode.
	pageEditInstruction = "Edit the attached manga page image. Keep the panel layout, compositions, dialogue balloons, and art style unchanged. Apply ONLY this change: "
)

// PagePrompt is ap-story's page-composition prompt.
// It carries the work-specific refinements, including instructions for panel layout, speech bubbles and lettering.
type PagePrompt struct {
	// Styles are the art-style presets (the same role as PanelPrompt.Styles).
	Styles *Styles
}

var _ ports.PagePrompt = PagePrompt{}

// BuildPage builds the page-composition prompt.
func (p PagePrompt) BuildPage(data *ports.PagePromptData) (string, string, string, error) {
	if data == nil {
		return "", "", "", fmt.Errorf("page prompt data is required")
	}

	styleSuffix, negative, err := resolveStyle(p.Styles, data.StyleMode, pageNegativePrompt, imageSuffix)
	if err != nil {
		return "", "", "", err
	}

	numPanels := len(data.Panels)
	if numPanels == 0 {
		return "", "", "", fmt.Errorf("page prompt requires at least one panel")
	}
	// Decide the layout once and write both the placement map and the per-panel instructions from it.
	// Built separately, the same page would carry conflicting position instructions.
	layout := planPageLayout(data.Panels)

	var sb strings.Builder
	sb.WriteString("# PAGE PRODUCTION REQUEST\n")
	sb.WriteString("- OUTPUT: ONE single portrait manga page image.\n")
	// Colouring follows the style preset's instruction (appended as ART STYLE).
	// Insisting on full colour here would directly contradict a monochrome style.
	sb.WriteString("- COLOR: Follow the ARTISTIC STYLE instruction exactly.\n")
	fmt.Fprintf(&sb, "- PANEL COUNT: [ %d ] (STRICTLY ONLY %d PANELS. DO NOT ADD ANY MORE).\n\n", numPanels, numPanels)

	writeLayoutStructure(&sb, layout)
	writeCharacterReferences(&sb, data)
	writePanelBreakdown(&sb, data, layout)

	return p.systemPrompt(styleSuffix), strings.TrimRight(sb.String(), "\n"), negative, nil
}

// BuildPageEdit builds the edit instruction for an existing page image.
func (p PagePrompt) BuildPageEdit(editPrompt string) (string, string, string, error) {
	return pageSystemPrompt, pageEditInstruction + editPrompt, pageNegativePrompt, nil
}

// systemPrompt returns the system prompt with the style instruction appended.
func (PagePrompt) systemPrompt(styleSuffix string) string {
	if styleSuffix == "" {
		return pageSystemPrompt
	}
	return pageSystemPrompt + "\n\n### ARTISTIC STYLE ###\n" + styleSuffix
}

// writeLayoutStructure writes the tier layout (each tier's height, and each panel's side and width).
// Pages read right to left, so within a tier the lower-numbered panel is on the right.
func writeLayoutStructure(sb *strings.Builder, layout []panelPlacement) {
	sb.WriteString("## MANDATORY PAGE STRUCTURE\n")
	sb.WriteString("- READING ORDER: Japanese Style (Right-to-Left, then Top-to-Bottom).\n")

	tiers := groupTiers(layout)
	if len(layout) == 1 {
		sb.WriteString("- PANEL PLACEMENT MAP:\n")
		sb.WriteString("  * PANEL 1: SINGLE FULL-PAGE PANEL (covers entire image area).\n")
		sb.WriteString("- FRAME STYLE: Deep black borders. GUTTERS: Pure white.\n\n")
		return
	}

	fmt.Fprintf(sb, "- TIERS: %d horizontal tiers stacked top to bottom. The heights below are deliberate — do NOT even them out.\n", len(tiers))
	sb.WriteString("- PANEL PLACEMENT MAP:\n")
	for _, tier := range tiers {
		parts := make([]string, 0, len(tier.Panels))
		for _, i := range tier.Panels {
			if layout[i].Column == columnFull {
				parts = append(parts, fmt.Sprintf("PANEL %d = FULL WIDTH", i+1))
				continue
			}
			parts = append(parts, fmt.Sprintf("PANEL %d = %s, %d%% of the width", i+1, layout[i].Column, layout[i].Width))
		}
		fmt.Fprintf(sb, "  * TIER %d (%d%% of the page height): %s.\n", tier.Number, tier.Height, strings.Join(parts, " | "))
	}
	sb.WriteString("- FRAME STYLE: Deep black borders. GUTTERS: Pure white.\n\n")
}

// writeCharacterReferences writes the list of character master references.
// The input_file numbers follow CharacterFile (the actual attachment order).
// writeCharacterReferences lists master references only for characters not shown in a panel image.
//
// For a character shown in a panel image, that panel is the reference (each panel's CHARACTER_IDENTITY reference).
// Saying "match the character sheet" on top of that here would compete with the per-panel instructions.
// The descriptions (visual_cues) are written for design sheets and run long enough to list pin placements.
// That granularity does not suit a character shrunk to a few hundred pixels on a page; including it is only noise.
func writeCharacterReferences(sb *strings.Builder, data *ports.PagePromptData) {
	if len(data.CharacterFile) == 0 {
		return
	}

	var lines []string
	seen := make(map[string]struct{})
	for _, panel := range data.Panels {
		if _, hasGuide := data.PanelFile[panel.ID]; hasGuide {
			// This panel's art exists. For a character shown here, that panel is the model.
			continue
		}
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
			lines = append(lines, fmt.Sprintf("- SUBJECT [%s]: Match input_file_%d. Traits: {%s}.\n", char.Name, idx, cues))
		}
	}
	if len(lines) == 0 {
		return
	}

	sb.WriteString("## CHARACTER MASTER REFERENCES\n")
	sb.WriteString("These characters have no panel guide on this page; draw them from the sheet.\n")
	for _, line := range lines {
		sb.WriteString(line)
	}
	sb.WriteString("\n")
}

// writePanelBreakdown writes each panel's breakdown (placement, staging, characters, dialogue).
func writePanelBreakdown(sb *strings.Builder, data *ports.PagePromptData, layout []panelPlacement) {
	sb.WriteString("## PANEL BREAKDOWN\n")
	for i := range data.Panels {
		panel := &data.Panels[i]
		writePanelHeader(sb, i, panel, layout[i])
		writePanelScene(sb, panel, data)
		writePanelCharacters(sb, panel, data)
		writePanelDialogues(sb, panel, data.Characters, layout[i].Width)
		sb.WriteString("\n")
	}
}

// writePanelHeader writes the panel heading and placement instruction.
func writePanelHeader(sb *strings.Builder, index int, panel *comic.Panel, place panelPlacement) {
	switch {
	case place.Tiers == 1 && place.Column == columnFull:
		sb.WriteString("### PANEL 1 [FULL-PAGE]\n- POSITION: Entire page area\n")
	case place.Column == columnFull:
		fmt.Fprintf(sb, "### PANEL %d [FULL-WIDTH IMPACT]\n", index+1)
		fmt.Fprintf(sb, "- POSITION: Tier %d of %d, full page width, %d%% of the page height\n", place.Tier, place.Tiers, place.Height)
		fmt.Fprintf(sb, "- COMPOSITION: %s\n", fullWidthComposition(panel))
	default:
		fmt.Fprintf(sb, "### PANEL %d [Standard]\n", index+1)
		fmt.Fprintf(sb, "- POSITION: Tier %d of %d, %s side, %d%% of the width, %d%% of the page height\n",
			place.Tier, place.Tiers, place.Column, place.Width, place.Height)
	}
}

// fullWidthComposition writes the intent of the composition from the reason the panel went full width.
// A long shot, a key panel on a shout and a mere pause each want a different picture, though all are full width.
func fullWidthComposition(panel *comic.Panel) string {
	var hints []string
	if isWideShot(panel.Shot) {
		hints = append(hints, "Wide establishing composition; show the location across the full width")
	}
	if hasImpactLine(panel) {
		hints = append(hints, "High impact; keep the subject large and dynamic")
	}
	if len(hints) == 0 {
		hints = append(hints, "Let the moment breathe; a single clear focal point")
	}
	return strings.Join(hints, ". ") + "."
}

// writePanelScene writes the scene staging and the composition guide (the generated panel image).
func writePanelScene(sb *strings.Builder, panel *comic.Panel, data *ports.PagePromptData) {
	if panel.Shot != "" {
		fmt.Fprintf(sb, "- SHOT: %s\n", panel.Shot)
	}
	if panel.Setting != "" {
		fmt.Fprintf(sb, "- SETTING: %s\n", panel.Setting)
	}
	if anchor := stripTextExclusions(panel.VisualAnchor); anchor != "" {
		fmt.Fprintf(sb, "- SCENE: %s\n", anchor)
	}
	if idx, ok := data.PanelFile[panel.ID]; ok {
		fmt.Fprintf(sb, "- COMPOSITION_GUIDE: Recreate the composition, posing, background, and character appearance from input_file_%d inside this panel.\n", idx)
	}
}

// stripTextExclusions drops "do not draw text or speech bubbles" instructions that slipped into the composition notes.
//
// That is right for panel generation, but page composition's job is to draw speech bubbles. Passed on as is,
// the same prompt would instruct "do not draw" and "draw this text" at once.
// The script side no longer writes them, but they are dropped here too for states made before that.
func stripTextExclusions(anchor string) string {
	parts := strings.Split(anchor, ",")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "no ") &&
			(strings.Contains(lower, "text") || strings.Contains(lower, "speech") ||
				strings.Contains(lower, "bubble") || strings.Contains(lower, "balloon") ||
				strings.Contains(lower, "letter") || strings.Contains(lower, "caption")) {
			continue
		}
		kept = append(kept, trimmed)
	}
	return strings.Join(kept, ", ")
}

// writePanelCharacters writes the identity and staging instructions for the characters who appear.
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
		sheet, hasSheet := data.CharacterFile[pc.CharacterID]
		switch guide, hasGuide := data.PanelFile[panel.ID]; {
		case hasGuide && hasSheet:
			// This panel's art already exists, and the figure drawn there is the reference for this work.
			// A character sheet is a different picture with a different ratio and style, and prioritising it causes re-fitting.
			fmt.Fprintf(sb, "- CHARACTER_IDENTITY: [ %s ] already appears in input_file_%d — keep exactly that face, hair, and outfit. Consult input_file_%d (character sheet) only for details input_file_%d does not show clearly.\n",
				char.Name, guide, sheet, guide)
		case hasSheet:
			// A page with no panel image (an individual recomposition and the like). Only then is the character sheet the sole clue.
			fmt.Fprintf(sb, "- CHARACTER_IDENTITY: [ %s ] from input_file_%d. (Face, hair, and outfit MUST match input_file_%d exactly).\n", char.Name, sheet, sheet)
		default:
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

// writePanelDialogues writes the drawing instructions for dialogue, narration and SFX per kind.
//
// When the same panel has several utterances, the reading order is stated. Without it, bubble
// placement is decided by the art's convenience and an exchange can read in reverse.
func writePanelDialogues(sb *strings.Builder, panel *comic.Panel, characters *comic.Characters, width int) {
	if lines := countSpokenLines(panel); lines > 1 {
		sb.WriteString("- BALLOON ORDER: Place the balloons below in the listed order, read right-to-left then top-to-bottom within this panel.\n")
		if width < 100 {
			fmt.Fprintf(sb, "- BALLOON SPACE: %d balloons must fit inside this narrow panel. Keep the composition simple and leave the upper area clear.\n", lines)
		}
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
			// Dialogue with an empty SpeakerID is treated as narration/caption (see comic.DialogueLine)
			if strings.TrimSpace(line.SpeakerID) == "" {
				sb.WriteString("- NARRATION: Rectangular caption box.\n")
			} else {
				fmt.Fprintf(sb, "- SPEECH: Speech bubble for [%s].\n", speakerName(characters, line.SpeakerID))
			}
		}
		fmt.Fprintf(sb, "  - TEXT_TO_RENDER: %q\n", text)

		direction := "Vertical (Tategaki)"
		layoutDesc := "traditional Japanese manga style layout"
		// Short shouts and the like may also be written horizontally, for impact
		if len([]rune(text)) <= 10 && strings.ContainsAny(text, "!?！？") {
			direction = "Horizontal (Yokogaki) or Vertical"
			layoutDesc = "bold and high impact placement"
		}
		fmt.Fprintf(sb, "  - TEXT_DIRECTION: %s\n", direction)
		fmt.Fprintf(sb, "  - TYPOGRAPHY: Use professional Japanese manga font (Gothic/Mincho). %s.\n", layoutDesc)
		sb.WriteString("  - LANGUAGE: Japanese characters. Ensure accurate rendering of Kanji/Kana.\n")
	}
}

// countSpokenLines returns the number of non-empty utterances in the panel.
func countSpokenLines(panel *comic.Panel) int {
	n := 0
	for _, line := range panel.Dialogues {
		if strings.TrimSpace(line.Text) != "" {
			n++
		}
	}
	return n
}

// speakerName looks up the display name from a speaker ID. An unknown ID gives the empty string.
func speakerName(characters *comic.Characters, speakerID string) string {
	if speakerID == "" || characters == nil {
		return ""
	}
	if char := characters.GetCharacter(speakerID); char != nil {
		return char.Name
	}
	return ""
}
