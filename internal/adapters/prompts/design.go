package prompts

import (
	"fmt"
	"strings"

	"github.com/shouni/go-comic-kit/ports"

	"github.com/shouni/ap-story/internal/domain"
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

	// The supplementary sheets (one character at a time, domain.validateDesignLayout). Each shows
	// one aspect of the character on its own, beside the full-body anchor rather than on it.
	//
	// designLayoutExpressions: the moods are named so the six portraits differ from each other,
	// and the angle and framing are fixed so only the expression changes.
	designLayoutExpressions = "an expression sheet of the same character: six head-and-shoulders portraits in a 3 by 2 grid, all at identical scale, identical three-quarter angle and identical framing, each with a different expression — calm, gentle smile, bright laugh, determined, singing with the mouth open, quiet sadness — with the hairstyle, hair colour, eye colour and every accessory identical in all six portraits"
	// designLayoutOutfit: front and back, because the back is where layering and fastenings show.
	designLayoutOutfit = "an outfit sheet of the same character: front view and back view side by side, standing full body at identical scale on a shared ground line, arms held slightly away from the body so every layer, seam and fastening of the costume is visible, each view drawn as one complete connected figure from head to toe"
	// designLayoutParts: no person, so the model draws the accessories themselves rather than
	// a body wearing them, where small items get hidden or moved to the wrong side.
	designLayoutParts = "a props and accessories sheet of the same character: each accessory and prop from the description drawn on its own, with no person in the image, separated and evenly spaced on the background at a readable size, each item shown whole with the exact colours, materials and details of the description"

	designLayoutPromptFormat = "Layout: %s"

	// designSystemIntro opens the system instruction. It names no work: the same sheets serve
	// the comics (Zundamon, Metan, Tsumugi) and the music series' characters (Akari, Hikari), so
	// a world written in here would be wrong for one of them.
	designSystemIntro = `You are a professional character designer creating official model sheets.
This sheet is the canonical identity reference that every downstream panel, page, and cover art will rely on, so accuracy and consistency outweigh artistic flair:`

	// designReferenceAll / designReferenceSome / designReferenceNone tell the model how to use the
	// attached images. A subject without a reference image (a new character's first sheet) must
	// not be told to follow one: with no image the model invents a look to match the instruction,
	// and with another subject's image it borrows that subject's.
	designReferenceAll  = "- One or more reference images are attached, one for each subject in order. Treat each attached reference image as the ground truth for that character's identity — strictly preserve its hairstyle, hair color, eye color, skin tone, outfit silhouette, colors, and accessories. The text description supplements the reference image; it does not override or replace it."
	designReferenceSome = "- Reference images are attached for these subjects only, in this order: %s. Treat each of them as the ground truth for that character's identity — strictly preserve its hairstyle, hair color, eye color, skin tone, outfit silhouette, colors, and accessories. Every other subject has no reference image yet: design that character from the text description alone, following every listed trait exactly, and never borrow features from an attached image."
	designReferenceNone = "- No reference image is attached: this is the character's first sheet. Design the character from the text description alone, following every listed trait exactly — hairstyle, hair color, eye color, outfit, colors, and accessories, including which side of the body each accessory is on."

	// designRulesCommon apply to every sheet.
	designRulesCommon = `- Anatomical correctness is critical. Draw every hand with exactly five fingers, correct limb proportions, and clean readable silhouettes.
- Use flat, even, neutral studio lighting only. No dramatic shadows, rim light, lens flares, or color grading — lighting baked into this sheet contaminates every downstream generation that references it.
- Render absolutely no text, labels, arrows, color swatches, logos, or annotations of any kind.`
	// designRulesFullBody apply to the full-body sheets (turnaround, single pose, outfit).
	designRulesFullBody = `- Draw each figure as one complete, physically connected body: head, torso, both arms, and both legs attached in a single continuous silhouette. Never split a figure into pieces, and never add close-up detail insets, detached limbs, floating heads, or partial bodies.
- Every view of a character must depict the SAME character with identical hairstyle, hair color, eye color, skin tone, outfit, and accessories — including any signature props or gadgets.
- The full body must be visible from head to toe and must never be cropped by the frame.`
	// designRulesExpressions apply to the expression sheet: head-and-shoulders portraits replace
	// the full-body rules, which would forbid them as "floating heads, partial bodies".
	designRulesExpressions = `- Every portrait shows the SAME character from the shoulders up, complete within its own cell, with identical hairstyle, hair color, eye color, skin tone and visible accessories. Only the facial expression changes.
- Keep the six cells evenly sized and separated by plain background, with no borders drawn.`
	// designRulesParts apply to the props sheet: no person, so the body rules do not apply.
	designRulesParts = `- Draw only the accessories and props, never a person or a body part wearing them.
- Draw each item once, whole and uncropped, as a clean product-style illustration in the same art style as the character.`

	// designNegativePrompt lists what the full-body sheets should not contain.
	// Note: genai-kit's imagegen does not use this as negative conditioning; it only appends it to the end of the
	// prompt as plain text under a "[Negative Prompt]" heading, so listing defect vocabulary such as
	// "extra limbs" or "fused fingers" acts as ordinary prompt tokens and actually invites that breakage.
	// Anatomical quality is therefore instructed in positive form in the system prompt, and this
	// is limited to listing "content that must not be included", which an instruction-following model can interpret.
	designNegativePrompt = "Do not include any of the following in the image: text, letters, labels, annotations, arrows, diagrams, color swatches, watermarks, logos, signatures, speech bubbles, background scenery or background objects, extra duplicate figures, close-up detail insets, dramatic lighting, strong shadows, rim light, lens flare, color grading, blur"
	// designNegativePromptSupplementary is the same list without "extra duplicate figures" and
	// "close-up detail insets", which are what an expression sheet is made of.
	designNegativePromptSupplementary = "Do not include any of the following in the image: text, letters, labels, annotations, arrows, diagrams, color swatches, watermarks, logos, signatures, speech bubbles, background scenery or background objects, dramatic lighting, strong shadows, rim light, lens flare, color grading, blur"
)

// DesignPrompt is ap-story's own design-sheet prompt implementation.
// It keeps the technical rigour of go-comic-kit's built-in prompts.DefaultDesignPrompt (character identity,
// anatomically correct fingers, flat lighting) while managing its own wording.
type DesignPrompt struct {
	// Styles are the art-style presets. Sheets use not the panel art style but the
	// sheet-specific one with the staging removed (Style.DesignSuffix).
	Styles *Styles
}

var _ ports.DesignSheetPrompt = DesignPrompt{}

// BuildDesignSheet builds the system/user/negative prompts for character design-sheet generation.
// data.Layout chooses the sheet (domain.DesignLayouts); data.Referenced says which subjects have a
// reference image attached.
func (p DesignPrompt) BuildDesignSheet(data *ports.DesignSheetPromptData) (systemPrompt, userPrompt, negativePrompt string, err error) {
	if data == nil || len(data.Descriptions) == 0 {
		return "", "", "", fmt.Errorf("description is required to build a design sheet prompt")
	}

	baseNegative := designNegativePrompt
	if data.Layout == domain.DesignLayoutExpressions || data.Layout == domain.DesignLayoutParts {
		baseNegative = designNegativePromptSupplementary
	}
	styleSuffix, negative, err := resolveStyle(p.Styles, data.StyleMode, baseNegative, designSuffix)
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
	)
	if data.Layout != domain.DesignLayoutParts {
		promptParts = append(promptParts, "perfectly drawn hands with five fingers per hand")
	}

	userPrompt = strings.Join(promptParts, ", ")
	return designSystemPrompt(data), userPrompt, negative, nil
}

// designSystemPrompt assembles the system instruction: the intro, how to use the attached
// images, the rules for every sheet, and the rules of the layout.
func designSystemPrompt(data *ports.DesignSheetPromptData) string {
	parts := []string{designSystemIntro, designReferenceRule(data)}
	parts = append(parts, designRulesCommon)
	switch data.Layout {
	case domain.DesignLayoutExpressions:
		parts = append(parts, designRulesExpressions)
	case domain.DesignLayoutParts:
		parts = append(parts, designRulesParts)
	default:
		parts = append(parts, designRulesFullBody)
	}
	return strings.Join(parts, "\n")
}

// designReferenceRule tells the model how to use the attached images. Referenced missing (an
// older caller) means every subject has one, which was the only case before go-comic-kit v1.11.0.
func designReferenceRule(data *ports.DesignSheetPromptData) string {
	if len(data.Referenced) != len(data.Descriptions) {
		return designReferenceAll
	}
	var withImage []string
	for i, ok := range data.Referenced {
		if ok {
			withImage = append(withImage, fmt.Sprintf("Subject %d", i+1))
		}
	}
	switch {
	case len(withImage) == len(data.Descriptions):
		return designReferenceAll
	case len(withImage) == 0:
		return designReferenceNone
	default:
		return fmt.Sprintf(designReferenceSome, strings.Join(withImage, ", "))
	}
}

// designLayout returns the layout text for the number of subjects and the requested layout.
// On a sheet composing several characters, using the "same character" three-view text as is
// contradicts the subject count (N DIFFERENT characters) and invites fusing and splitting,
// so it switches to dedicated wording that groups the views per character. The supplementary
// sheets are drawn for one character at a time (domain.validateDesignLayout).
func designLayout(numChars int, layout string) string {
	switch layout {
	case domain.DesignLayoutExpressions:
		return designLayoutExpressions
	case domain.DesignLayoutOutfit:
		return designLayoutOutfit
	case domain.DesignLayoutParts:
		return designLayoutParts
	case ports.DesignLayoutSingleView:
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
