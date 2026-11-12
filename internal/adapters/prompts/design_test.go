package prompts

import (
	"strings"
	"testing"

	"github.com/shouni/go-comic-kit/ports"

	"github.com/shouni/ap-story/internal/domain"
)

func TestDesignPromptBuildsSingleSubject(t *testing.T) {
	t.Parallel()

	system, user, negative, err := testDesignPrompt(t).BuildDesignSheet(&ports.DesignSheetPromptData{
		Descriptions: []string{"Zundamon (green hair)"},
	})
	if err != nil {
		t.Fatalf("BuildDesignSheet failed: %v", err)
	}
	if !strings.Contains(system, "canonical identity reference") {
		t.Errorf("system prompt = %q, want identity-consistency instructions", system)
	}
	if !strings.Contains(system, "one complete, physically connected body") {
		t.Errorf("system prompt = %q, want connected-body instructions", system)
	}
	if !strings.Contains(negative, "Do not include") {
		t.Errorf("negative prompt = %q, want instruction-style exclusion list", negative)
	}
	// imagegen concatenates the negative prompt into the prompt as plain text, so
	// defect vocabulary (extra fingers and so on) actually invites the breakage. This guarantees it is absent.
	for _, defect := range []string{"extra fingers", "fused fingers", "extra limbs", "malformed"} {
		if strings.Contains(negative, defect) {
			t.Errorf("negative prompt contains defect vocabulary %q, which can induce the artifact", defect)
		}
	}
	if !strings.Contains(user, "Zundamon (green hair)") || !strings.Contains(user, "anime style") {
		t.Errorf("user prompt = %q, want description and style suffix", user)
	}
	if !strings.Contains(user, "front view, side view, and back view") {
		t.Errorf("user prompt = %q, want default turnaround layout", user)
	}
	if !strings.Contains(user, "complete connected figure") {
		t.Errorf("user prompt = %q, want connected-figure layout constraint", user)
	}
}

func TestDesignPromptMultiSubject(t *testing.T) {
	t.Parallel()

	_, user, _, err := testDesignPrompt(t).BuildDesignSheet(&ports.DesignSheetPromptData{
		Descriptions: []string{"Zundamon", "Metan"},
	})
	if err != nil {
		t.Fatalf("BuildDesignSheet failed: %v", err)
	}
	if !strings.Contains(user, "2 DIFFERENT characters") {
		t.Errorf("user prompt = %q, want multi-subject framing", user)
	}
	if !strings.Contains(user, "each character's three views grouped together") {
		t.Errorf("user prompt = %q, want per-character view grouping", user)
	}
	if strings.Contains(user, "of the same character") {
		t.Errorf("user prompt = %q, must not contain single-character layout phrasing", user)
	}
}

func TestDesignPromptSingleViewLayout(t *testing.T) {
	t.Parallel()

	_, user, _, err := testDesignPrompt(t).BuildDesignSheet(&ports.DesignSheetPromptData{
		Descriptions: []string{"Zundamon"},
		Layout:       ports.DesignLayoutSingleView,
	})
	if err != nil {
		t.Fatalf("BuildDesignSheet failed: %v", err)
	}
	if !strings.Contains(user, "single view, front-facing") {
		t.Errorf("user prompt = %q, want single-view layout", user)
	}
	if strings.Contains(user, "three views") {
		t.Errorf("user prompt = %q, must not contain turnaround layout", user)
	}
}

func TestDesignPromptSingleViewMultiSubject(t *testing.T) {
	t.Parallel()

	_, user, _, err := testDesignPrompt(t).BuildDesignSheet(&ports.DesignSheetPromptData{
		Descriptions: []string{"Zundamon", "Metan"},
		Layout:       ports.DesignLayoutSingleView,
	})
	if err != nil {
		t.Fatalf("BuildDesignSheet failed: %v", err)
	}
	if !strings.Contains(user, "single view, front-facing") {
		t.Errorf("user prompt = %q, want single-view layout", user)
	}
	if !strings.Contains(user, "side by side") {
		t.Errorf("user prompt = %q, want side-by-side multi-character layout", user)
	}
	if strings.Contains(user, "three views") {
		t.Errorf("user prompt = %q, must not contain turnaround layout", user)
	}
}

func TestDesignPromptRejectsEmptyDescriptions(t *testing.T) {
	t.Parallel()

	if _, _, _, err := testDesignPrompt(t).BuildDesignSheet(&ports.DesignSheetPromptData{}); err == nil {
		t.Error("BuildDesignSheet with no descriptions succeeded, want error")
	}
	if _, _, _, err := testDesignPrompt(t).BuildDesignSheet(nil); err == nil {
		t.Error("BuildDesignSheet(nil) succeeded, want error")
	}
}

// A subject without a reference image (a new character's first sheet) must not be told to
// follow one: with no image the model invents a look, with another subject's it borrows theirs.
func TestDesignPromptDescribesWhichSubjectsHaveAReference(t *testing.T) {
	t.Parallel()

	build := func(descriptions []string, referenced []bool) string {
		t.Helper()
		system, _, _, err := testDesignPrompt(t).BuildDesignSheet(&ports.DesignSheetPromptData{
			Descriptions: descriptions, Referenced: referenced,
		})
		if err != nil {
			t.Fatalf("BuildDesignSheet failed: %v", err)
		}
		return system
	}

	all := build([]string{"Zundamon"}, []bool{true})
	if !strings.Contains(all, "Treat each attached reference image as the ground truth") {
		t.Errorf("all referenced: system = %q, want the ground-truth rule", all)
	}
	// an older caller passes no flags: every subject had a reference then
	if legacy := build([]string{"Zundamon"}, nil); legacy != all {
		t.Error("Referenced unset must read as every subject having a reference")
	}

	none := build([]string{"Akari (silver hair)"}, []bool{false})
	if strings.Contains(none, "ground truth") || !strings.Contains(none, "No reference image is attached") {
		t.Errorf("none referenced: system = %q, want the text-only rule and no ground-truth rule", none)
	}

	some := build([]string{"Metan", "Akari"}, []bool{true, false})
	if !strings.Contains(some, "for these subjects only, in this order: Subject 1.") ||
		!strings.Contains(some, "design that character from the text description alone") {
		t.Errorf("some referenced: system = %q, want Subject 1 named and the others drawn from text", some)
	}
}

// The same sheets serve the comics and the music series, so the system prompt names no work.
func TestDesignSystemPromptNamesNoWork(t *testing.T) {
	t.Parallel()

	system, _, _, err := testDesignPrompt(t).BuildDesignSheet(&ports.DesignSheetPromptData{Descriptions: []string{"Akari"}})
	if err != nil {
		t.Fatalf("BuildDesignSheet failed: %v", err)
	}
	for _, word := range []string{"Zundamon", "Metan", "Tsumugi", "manga", "mecha"} {
		if strings.Contains(system, word) {
			t.Errorf("system prompt names %q; it serves every work", word)
		}
	}
}

// The supplementary sheets replace the full-body rules, which would forbid what they are made of
// ("floating heads, partial bodies" for portraits, a body for a props sheet).
func TestDesignPromptSupplementaryLayouts(t *testing.T) {
	t.Parallel()

	build := func(layout string) (string, string, string) {
		t.Helper()
		system, user, negative, err := testDesignPrompt(t).BuildDesignSheet(&ports.DesignSheetPromptData{
			Descriptions: []string{"Akari (red armband on the right arm)"}, Referenced: []bool{true}, Layout: layout,
		})
		if err != nil {
			t.Fatalf("BuildDesignSheet(%q) failed: %v", layout, err)
		}
		return system, user, negative
	}

	system, user, negative := build(domain.DesignLayoutExpressions)
	if !strings.Contains(user, "six head-and-shoulders portraits") || strings.Contains(system, "floating heads") ||
		strings.Contains(negative, "extra duplicate figures") {
		t.Errorf("expressions: user = %q / system = %q / negative = %q", user, system, negative)
	}

	system, user, negative = build(domain.DesignLayoutParts)
	if !strings.Contains(user, "with no person in the image") || !strings.Contains(system, "never a person") ||
		strings.Contains(user, "five fingers") || strings.Contains(system, "head to toe") {
		t.Errorf("parts: user = %q / system = %q", user, system)
	}
	if strings.Contains(negative, "close-up detail insets") {
		t.Errorf("parts negative = %q, should not forbid what the sheet is made of", negative)
	}

	system, user, _ = build(domain.DesignLayoutOutfit)
	if !strings.Contains(user, "front view and back view") || !strings.Contains(system, "one complete, physically connected body") {
		t.Errorf("outfit: user = %q / system = %q, want full-body rules", user, system)
	}
}
