package prompts

import (
	"fmt"
	"strings"

	"github.com/shouni/go-comic-kit/comic"

	"github.com/shouni/go-comic-kit/ports"
)

const (
	// panelSystemPrompt is the system instruction given to the model when generating panel images.
	// The attached reference images and the [Subject N] entries in the prompt correspond in the same order.
	panelSystemPrompt = `You are a professional manga panel illustrator.
Draw a single manga panel following the scene direction, with these rules:
- Attached reference images correspond to [Subject N] in the prompt, in the same order. Strictly preserve each subject's identity from its reference image: hairstyle, hair color, eye color, outfit, and accessories. Never mix identities between subjects.
- Translate each subject's stated emotion and action into expression, gaze, and pose. Place subjects according to their stated positions.
- Anatomical correctness is critical: draw every hand with exactly five fingers and correct limb proportions.
- Render absolutely no text, speech bubbles, sound effects lettering, or logos — dialogue is composited separately.`

	// panelNegativePrompt is the negative prompt listing what panel images should not contain.
	// Besides excluding speech bubbles and text, it covers broken hands and fingers.
	//
	// Words that depend on art style (monochrome and so on) are not written here. The style preset holds them
	// as its negative, and only those of the chosen style are added. Written here, they would clash
	// head-on with a style that allows monochrome.
	panelNegativePrompt = "speech bubble, dialogue balloon, text, alphabet, letters, words, signatures, watermark, username, malformed hands, fused fingers, extra fingers, missing fingers, extra limbs, deformed anatomy, low quality, distorted, bad anatomy"

	// panelEditInstruction is the shared prefix that instructs keeping the composition in edit mode.
	panelEditInstruction = "Edit the attached manga panel image. Keep the composition, character poses, background, and art style unchanged. Apply ONLY this change: "
)

// PanelPrompt is ap-story's panel-image prompt.
//
// go-comic-kit's default carries only the minimal instructions that enforce the format, so the
// staging vocabulary and art-style refinements live on this side.
type PanelPrompt struct {
	// Styles are the art-style presets. The kit carries only the mode name, so resolving it into the
	// art-style instruction and the negative prompt happens here.
	Styles *Styles
}

var _ ports.PanelPrompt = PanelPrompt{}

// BuildPanel builds the prompt for panel-image generation.
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

	// The N in [Subject N] matches the attachment order of the reference images (the order of SubjectIDs).
	// If they drift, the model draws while looking at someone else's reference image.
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

// BuildPanelEdit builds the edit instruction for an existing panel image.
// It has only the specified parts changed, keeping the composition, pose and background.
//
// An edit inherits the existing image's art style, so the preset's negative is not added.
func (PanelPrompt) BuildPanelEdit(editPrompt string) (string, string, string, error) {
	return panelSystemPrompt, panelEditInstruction + editPrompt, panelNegativePrompt, nil
}

// subjectLine builds the [Subject N] description for one character.
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

// findPanelCharacter looks up a character in the panel by ID.
func findPanelCharacter(panel *comic.Panel, charID string) *comic.PanelCharacter {
	for i := range panel.Characters {
		if panel.Characters[i].CharacterID == charID {
			return &panel.Characters[i]
		}
	}
	return nil
}

// backgroundExtras returns the list of background (crowd) characters.
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

// backgroundExtraDesc builds the description of one background character.
func backgroundExtraDesc(pc *comic.PanelCharacter) string {
	desc := pc.CharacterID
	if pc.Action != "" {
		desc += " (" + pc.Action + ")"
	}
	return desc
}
