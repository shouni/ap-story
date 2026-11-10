package prompts

import (
	"fmt"
	"strings"

	"github.com/shouni/go-comic-kit/ports"
)

const (
	// designPromptBaseTemplate is the base form of the design-sheet prompt.
	designPromptBaseTemplate = "Masterpiece character design sheet of %s"

	// designLayoutMultiView is the default turnaround layout (front, side and back views).
	// It states "same scale, a common ground line, each figure whole and complete", which curbs
	// generation failures where a body is split into parts or splits/fuses across views.
	designLayoutMultiView = "a character turnaround with exactly three views of the same character — front view, side view, and back view — each view standing full body in a neutral A-pose with arms held slightly away from the body so the costume stays fully visible, the three views placed side by side at identical scale on a shared ground line, each view drawn as one complete connected figure from head to toe"
	// designLayoutMultiViewMultiChar is the three-view layout for sheets composing several characters.
	// It has each character's views grouped together and explicitly forbids mixing up or
	// fusing features between characters.
	designLayoutMultiViewMultiChar = "for every character exactly three views — front view, side view, and back view — standing full body in a neutral A-pose with arms held slightly away from the body so the costume stays fully visible, each character's three views grouped together in its own horizontal row, all figures at identical scale on a shared ground line, each view drawn as one complete connected figure from head to toe, never blending or mixing features between different characters"
	// designLayoutSingleView is a single-pose, front-facing layout for use as a reference anchor
	// for other outputs (panels, cover art and so on).
	designLayoutSingleView = "single view, front-facing, standing full body in a neutral relaxed pose, centered composition, the entire body from head to toe inside the frame"
	// designLayoutSingleViewMultiChar is the single-pose layout for sheets composing several characters.
	designLayoutSingleViewMultiChar = "single view, front-facing, all characters standing side by side full body in a neutral relaxed pose at identical scale on a shared ground line, centered composition, every body entirely inside the frame from head to toe, never blending or mixing features between different characters"

	designLayoutPromptFormat = "Layout: %s"

	// designSystemPrompt is the system instruction given to the model when generating design sheets for
	// Zundamon, Metan and Tsumugi, who appear in ap-story's SF-mecha-action-style tech-learning comics.
	// The output is referenced by other workflows, such as panel and page composition, as the anchor of
	// character identity, so accuracy and consistency take priority over dramatic image-making.
	designSystemPrompt = `You are a professional character designer creating official model sheets for a sci-fi mecha-action tech-learning manga starring Zundamon, Metan, and Tsumugi.
This sheet is the canonical identity reference that every downstream panel, page, and cover art will rely on, so accuracy and consistency outweigh artistic flair:
- One or more reference images are attached for each subject. Treat each attached reference image as the ground truth for that character's identity — strictly preserve its hairstyle, hair color, eye color, skin tone, outfit silhouette, colors, and accessories. The text description supplements the reference image; it does not override or replace it.
- Anatomical correctness is critical. Draw every hand with exactly five fingers, correct limb proportions, and clean readable silhouettes.
- Draw each figure as one complete, physically connected body: head, torso, both arms, and both legs attached in a single continuous silhouette. Never split a figure into pieces, and never add close-up detail insets, detached limbs, floating heads, or partial bodies.
- Every view of a character must depict the SAME character with identical hairstyle, hair color, eye color, skin tone, outfit, and accessories — including any signature props or gadgets.
- Use flat, even, neutral studio lighting only. No dramatic shadows, rim light, lens flares, or color grading — lighting baked into this sheet contaminates every downstream generation that references it.
- The full body must be visible from head to toe and must never be cropped by the frame.
- Render absolutely no text, labels, arrows, color swatches, logos, or annotations of any kind.`

	// designNegativePrompt lists what the design sheet should not contain.
	// Note: genai-kit's imagegen does not use this as negative conditioning; it only appends it to the end of the
	// prompt as plain text under a "[Negative Prompt]" heading, so listing defect vocabulary such as
	// "extra limbs" or "fused fingers" acts as ordinary prompt tokens and actually invites that breakage.
	// Anatomical quality is therefore instructed in positive form in the system prompt, and this
	// is limited to listing "content that must not be included", which an instruction-following model can interpret.
	designNegativePrompt = "Do not include any of the following in the image: text, letters, labels, annotations, arrows, diagrams, color swatches, watermarks, logos, signatures, speech bubbles, background scenery or background objects, extra duplicate figures, close-up detail insets, dramatic lighting, strong shadows, rim light, lens flare, color grading, blur"
)

// DesignPrompt is ap-story's own design-sheet prompt implementation.
// It keeps the technical rigour of go-comic-kit's built-in prompts.DefaultDesignPrompt (character identity,
// anatomically correct fingers, flat lighting) while managing its own wording for ap-story's world.
type DesignPrompt struct {
	// Styles are the art-style presets. Sheets use not the panel art style but the
	// sheet-specific one with the staging removed (Style.DesignSuffix).
	Styles *Styles
}

var _ ports.DesignSheetPrompt = DesignPrompt{}

// BuildDesignSheet builds the system/user/negative prompts for character design-sheet generation.
// Passing ports.DesignLayoutSingleView as data.Layout gives the single-pose layout.
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
	// Even if staging words slip into the art style, the constraints of a reference anchor
	// (flat lighting, white background, accurate hands) come after it and take priority.
	promptParts = append(promptParts,
		"plain uniform white studio background",
		"flat even neutral lighting",
		"sharp focus",
		"perfectly drawn hands with five fingers per hand",
	)

	userPrompt = strings.Join(promptParts, ", ")
	return designSystemPrompt, userPrompt, negative, nil
}

// designLayout returns the layout text for the number of subjects and the requested layout.
// On a sheet composing several characters, using the "same character" three-view text as is
// contradicts the subject count (N DIFFERENT characters) and invites fusing and splitting,
// so it switches to dedicated wording that groups the views per character.
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
