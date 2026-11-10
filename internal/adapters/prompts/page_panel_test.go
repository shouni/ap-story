package prompts

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/shouni/go-comic-kit/comic"

	characterkit "github.com/shouni/go-character-kit/character"
	"github.com/shouni/go-comic-kit/ports"
)

func testCharacters(t *testing.T) *comic.Characters {
	t.Helper()
	cm, err := characterkit.NewCharacters([]comic.Character{
		{ID: "zundamon", Name: "ずんだもん", ReferenceURL: "gs://b/zunda.png", VisualCues: []string{"green hair"}, IsDefault: true},
		{ID: "metan", Name: "めたん", ReferenceURL: "gs://b/metan.png", VisualCues: []string{"purple hair"}},
	})
	if err != nil {
		t.Fatalf("NewCharacters failed: %v", err)
	}
	return cm
}

// TestPanelPromptNumbersSubjectsInAttachmentOrder checks that the [Subject N] numbers match the
// attachment order of the reference images (SubjectIDs). If they drift, the model draws while looking at
// someone else's reference image.
func TestPanelPromptNumbersSubjectsInAttachmentOrder(t *testing.T) {
	t.Parallel()
	cm := testCharacters(t)

	system, user, negative, err := testPanelPrompt(t).BuildPanel(&ports.PanelPromptData{
		Panel: comic.Panel{
			ID:      "ch01-p01",
			Shot:    "wide",
			Setting: "放課後の音楽室",
			Characters: []comic.PanelCharacter{
				{CharacterID: "zundamon", Prominence: comic.ProminencePrimary, Emotion: "驚き"},
				{CharacterID: "metan", Prominence: comic.ProminenceSecondary},
				{CharacterID: "students", Prominence: comic.ProminenceBackground, Action: "ざわめく"},
			},
		},
		Characters: cm,
		SubjectIDs: []string{"zundamon", "metan"},
	})
	if err != nil {
		t.Fatalf("BuildPanel() error = %v", err)
	}

	for _, want := range []string{
		"[Subject 1: ずんだもん (green hair)] emotion: 驚き.",
		"[Subject 2: めたん (purple hair)]",
		"Background extras (no reference, generic): students (ざわめく)",
		"Style: Japanese anime style", // the default preset's art style is appended
		"No speech bubbles, no text.",
	} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt does not contain %q\nprompt: %s", want, user)
		}
	}
	if !strings.Contains(system, "correspond to [Subject N]") {
		t.Error("system prompt does not explain the subject ordering")
	}
	if !strings.Contains(negative, "speech bubble") {
		t.Error("negative prompt lost the lettering exclusions")
	}
}

// TestPagePromptLayoutAndReferences checks the panel placement map, the reference numbers and the
// drawing instructions per dialogue kind (a regression test of the refinements moved over from go-comic-kit).
func TestPagePromptLayoutAndReferences(t *testing.T) {
	t.Parallel()
	cm := testCharacters(t)

	data := &ports.PagePromptData{
		Panels: []comic.Panel{
			{
				ID: "ch01-p01", Shot: "wide",
				Characters: []comic.PanelCharacter{{CharacterID: "zundamon", Prominence: comic.ProminencePrimary}},
				Dialogues: []comic.DialogueLine{
					{SpeakerID: "zundamon", Kind: comic.DialogueKindShout, Text: "なんなのだ！？"},
					{Kind: comic.DialogueKindNarration, Text: "その時、事件は起きた。"},
				},
			},
			{
				ID:         "ch01-p02",
				Characters: []comic.PanelCharacter{{CharacterID: "metan", Prominence: comic.ProminencePrimary}},
				Dialogues:  []comic.DialogueLine{{SpeakerID: "metan", Text: "落ち着きなさい。"}},
			},
		},
		Characters:    cm,
		CharacterFile: map[string]int{"zundamon": 1, "metan": 2},
		PanelFile:     map[string]int{"ch01-p01": 3},
	}

	system, user, _, err := testPagePrompt(t).BuildPage(data)
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}

	for _, want := range []string{
		"PANEL COUNT: [ 2 ]",
		// Feature panels containing a shout, so both panels stack vertically at full width
		"TIER 1 (65% of the page height): PANEL 1 = FULL WIDTH.",
		"TIER 2 (35% of the page height): PANEL 2 = FULL WIDTH.",
		// The side with a panel image is the reference, so no list of character sheets is shown (see the dedicated test below).
		"SUBJECT [めたん]: Match input_file_2",
		"COMPOSITION_GUIDE: Recreate the composition, posing, background, and character appearance from input_file_3",
		// The side with a panel image is the reference. The character sheet steps down to helping with detail.
		"CHARACTER_IDENTITY: [ ずんだもん ] already appears in input_file_3",
		"SHOUT: Jagged, explosive speech bubble for [ずんだもん]",
		"NARRATION: Rectangular caption box",
		`TEXT_TO_RENDER: "なんなのだ！？"`,
		"Horizontal (Yokogaki) or Vertical", // a short shout may be written horizontally
		`TEXT_TO_RENDER: "落ち着きなさい。"`,
	} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt does not contain %q\nprompt: %s", want, user)
		}
	}
	if !strings.Contains(system, "READING FLOW: Right-to-Left") || !strings.Contains(system, "### ARTISTIC STYLE ###") {
		t.Error("system prompt missing the format rules or the style section")
	}
}

// TestPagePromptFullWidthImpactForOddCount checks that even on a page with no staging instructions,
// the last panel becomes a full-width feature panel (the last panel pulls into the next page, so it gets room).
func TestPagePromptFullWidthImpactForOddCount(t *testing.T) {
	t.Parallel()
	cm := testCharacters(t)

	_, user, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{
		Panels:     []comic.Panel{{ID: "p1"}, {ID: "p2"}, {ID: "p3"}},
		Characters: cm,
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	for _, want := range []string{
		"TIER 1 (45% of the page height): PANEL 1 = RIGHT, 50% of the width | PANEL 2 = LEFT, 50% of the width.",
		"TIER 2 (55% of the page height): PANEL 3 = FULL WIDTH.",
		"PANEL 3 [FULL-WIDTH IMPACT]",
	} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt does not contain %q:\n%s", want, user)
		}
	}
}

// TestPageLayoutFollowsPanelContent checks that the layout is decided by the panels' content.
// With a uniform two-column grid, the long shot that always opens a chapter is squashed to half width.
func TestPageLayoutFollowsPanelContent(t *testing.T) {
	t.Parallel()

	say := func(text string) comic.DialogueLine {
		return comic.DialogueLine{SpeakerID: "zundamon", Kind: comic.DialogueKindSpeech, Text: text}
	}
	panels := []comic.Panel{
		{ID: "p1", Shot: "wide", Setting: "サーバルーム", Dialogues: []comic.DialogueLine{say("ここが心臓部なのだ")}},
		{ID: "p2", Shot: "medium", Setting: "サーバルーム", Dialogues: []comic.DialogueLine{say("説明する"), say("返す")}},
		{ID: "p3", Shot: "close-up", Setting: "サーバルーム"},
		{ID: "p4", Shot: "medium", Setting: "サーバルーム", Dialogues: []comic.DialogueLine{say("なるほど")}},
		{ID: "p5", Shot: "close-up", Setting: "サーバルーム", Dialogues: []comic.DialogueLine{say("つまり")}},
		{ID: "p6", Shot: "wide", Setting: "屋上", Dialogues: []comic.DialogueLine{{SpeakerID: "zundamon", Kind: comic.DialogueKindShout, Text: "行くのだ！"}}},
	}

	layout := planPageLayout(panels)
	for _, tc := range []struct {
		panel  int
		column string
	}{
		{0, columnFull},  // the chapter's opening long shot
		{1, columnRight}, // the exchange; wider than its partner, a silent close-up
		{2, columnLeft},
		{5, columnFull}, // the feature panel closing on a shout
	} {
		if got := layout[tc.panel].Column; got != tc.column {
			t.Errorf("PANEL %d position = %s, want %s", tc.panel+1, got, tc.column)
		}
	}
	if layout[1].Width <= layout[2].Width {
		t.Errorf("the exchange panel is not wider than the silent close-up: %d%% vs %d%%", layout[1].Width, layout[2].Width)
	}
	if layout[5].Height <= layout[2].Height {
		t.Errorf("the closing panel's tier is not taller than the explanation's tier: %d%% vs %d%%", layout[5].Height, layout[2].Height)
	}
}

// The tier heights always total 100%. A remainder would become a blank strip at the bottom of the page.
func TestPageLayoutTierHeightsFillThePage(t *testing.T) {
	t.Parallel()

	for n := 1; n <= 12; n++ {
		panels := make([]comic.Panel, n)
		for i := range panels {
			panels[i] = comic.Panel{ID: fmt.Sprintf("p%d", i+1)}
		}
		layout := planPageLayout(panels)
		if len(layout) != n {
			t.Fatalf("n=%d: %d placements", n, len(layout))
		}

		total := 0
		for _, tier := range groupTiers(layout) {
			if tier.Height <= 0 {
				t.Errorf("n=%d: TIER %d has height %d%%", n, tier.Number, tier.Height)
			}
			total += tier.Height
			width := 0
			for _, i := range tier.Panels {
				width += layout[i].Width
			}
			if width != 100 {
				t.Errorf("n=%d: the widths in TIER %d total %d%%", n, tier.Number, width)
			}
		}
		if total != 100 {
			t.Errorf("n=%d: the tier heights total %d%%", n, total)
		}
	}
}

// The placement map and the per-panel instructions are written from the same layout. Built separately,
// the same page would carry conflicting position instructions.
func TestPagePromptMapAgreesWithPanelBreakdown(t *testing.T) {
	t.Parallel()

	panels := make([]comic.Panel, 5)
	for i := range panels {
		panels[i] = comic.Panel{ID: fmt.Sprintf("p%d", i+1), Shot: "medium"}
	}
	panels[0].Shot = "wide"

	_, user, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{Panels: panels, Characters: testCharacters(t)})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}

	mapTier := map[int]int{}
	for _, m := range regexp.MustCompile(`\* TIER (\d+) \(\d+% of the page height\): (.+)\.`).FindAllStringSubmatch(user, -1) {
		tier, _ := strconv.Atoi(m[1])
		for _, entry := range regexp.MustCompile(`PANEL (\d+) =`).FindAllStringSubmatch(m[2], -1) {
			panel, _ := strconv.Atoi(entry[1])
			mapTier[panel] = tier
		}
	}
	if len(mapTier) != len(panels) {
		t.Fatalf("%d panels in the placement map:\n%s", len(mapTier), user)
	}
	for _, m := range regexp.MustCompile(`### PANEL (\d+) \[[^\]]+\]\n- POSITION: Tier (\d+) of`).FindAllStringSubmatch(user, -1) {
		panel, _ := strconv.Atoi(m[1])
		tier, _ := strconv.Atoi(m[2])
		if mapTier[panel] != tier {
			t.Errorf("PANEL %d: the placement map says TIER %d, the panel instruction says TIER %d", panel, mapTier[panel], tier)
		}
		delete(mapTier, panel)
	}
	if len(mapTier) != 0 {
		t.Errorf("in the placement map but not in the panel instructions: %v", mapTier)
	}
}

// When several speech bubbles go into a narrow panel, say so. They would hide all the art.
func TestPagePromptFlagsBalloonCrowdingInNarrowPanels(t *testing.T) {
	t.Parallel()

	chatter := []comic.DialogueLine{
		{SpeakerID: "zundamon", Kind: comic.DialogueKindSpeech, Text: "どうなってるのだ"},
		{SpeakerID: "metan", Kind: comic.DialogueKindSpeech, Text: "落ち着きなさい"},
	}
	// A page that opens and closes on a long shot. Only the middle tier is half width.
	panels := []comic.Panel{
		{ID: "p1", Shot: "wide"},
		{ID: "p2", Shot: "medium", Dialogues: chatter},
		{ID: "p3", Shot: "medium", Dialogues: chatter},
		{ID: "p4", Shot: "wide"},
	}

	layout := planPageLayout(panels)
	_, user, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{Panels: panels, Characters: testCharacters(t)})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	if layout[1].Column == columnFull {
		t.Fatalf("the premise is broken: no half-width panel (%+v)", layout)
	}
	if !strings.Contains(user, "BALLOON SPACE: 2 balloons must fit inside this narrow panel") {
		t.Errorf("the crowding of speech bubbles in a narrow panel is not mentioned:\n%s", user)
	}

	// Not needed for a full-width panel. It is not narrow, so saying it is only noise.
	_, single, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{
		Panels: panels[1:2], Characters: testCharacters(t),
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	if strings.Contains(single, "BALLOON SPACE") {
		t.Error("a full-width panel got the narrowness warning")
	}
}

// A page with no panels cannot be made. Building it with 0 panels would pass "PANEL COUNT: [ 0 ]".
func TestPagePromptRejectsEmptyPage(t *testing.T) {
	t.Parallel()

	if _, _, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{Characters: testCharacters(t)}); err == nil {
		t.Error("a page with 0 panels did not fail")
	}
}

// TestPromptEditModePreservesComposition checks that edit mode instructs keeping the composition.
func TestPromptEditModePreservesComposition(t *testing.T) {
	t.Parallel()

	_, panelUser, _, err := PanelPrompt{}.BuildPanelEdit("表情を笑顔に変える")
	if err != nil {
		t.Fatalf("BuildPanelEdit() error = %v", err)
	}
	if !strings.Contains(panelUser, "Keep the composition") || !strings.Contains(panelUser, "表情を笑顔に変える") {
		t.Errorf("panel edit prompt = %q", panelUser)
	}

	_, pageUser, _, err := PagePrompt{}.BuildPageEdit("夕焼けにする")
	if err != nil {
		t.Fatalf("BuildPageEdit() error = %v", err)
	}
	if !strings.Contains(pageUser, "Keep the panel layout") || !strings.Contains(pageUser, "夕焼けにする") {
		t.Errorf("page edit prompt = %q", pageUser)
	}
}

// testStyles loads the embedded art-style presets.
// The prompt implementations leave resolving the art style to the presets, so the tests use the real ones too
// (the wording itself is checked in style_test.go).
func testStyles(t *testing.T) *Styles {
	t.Helper()
	styles, err := NewStyles()
	if err != nil {
		t.Fatalf("NewStyles failed: %v", err)
	}
	return styles
}

func testPanelPrompt(t *testing.T) PanelPrompt   { return PanelPrompt{Styles: testStyles(t)} }
func testPagePrompt(t *testing.T) PagePrompt     { return PagePrompt{Styles: testStyles(t)} }
func testDesignPrompt(t *testing.T) DesignPrompt { return DesignPrompt{Styles: testStyles(t)} }

// When one panel has several utterances, the reading order is instructed.
// Without it, bubble placement is decided by the art's convenience and an exchange can read in reverse.
func TestPagePromptOrdersMultipleBalloons(t *testing.T) {
	t.Parallel()

	chars := testCharacters(t)
	multi := comic.Panel{
		ID: "ch01-p01", Page: 1, VisualAnchor: "bridge",
		Characters: []comic.PanelCharacter{
			{CharacterID: "zundamon", Prominence: comic.ProminencePrimary},
			{CharacterID: "metan", Prominence: comic.ProminenceSecondary},
		},
		Dialogues: []comic.DialogueLine{
			{SpeakerID: "zundamon", Text: "全自動なのだ！", Kind: comic.DialogueKindShout},
			{SpeakerID: "metan", Text: "勘違いよ。", Kind: comic.DialogueKindSpeech},
		},
	}

	_, user, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{
		Panels: []comic.Panel{multi}, Characters: chars,
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	if !strings.Contains(user, "BALLOON ORDER") {
		t.Errorf("several speech bubbles but no reading-order instruction:\n%s", user)
	}
	// Each speaker's bubble is identified
	for _, want := range []string{"ずんだもん", "めたん", `"全自動なのだ！"`, `"勘違いよ。"`} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt missing %q", want)
		}
	}

	// With only one, no extra instruction is given
	single := multi
	single.Dialogues = multi.Dialogues[:1]
	_, user, _, err = testPagePrompt(t).BuildPage(&ports.PagePromptData{
		Panels: []comic.Panel{single}, Characters: chars,
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	if strings.Contains(user, "BALLOON ORDER") {
		t.Error("a reading-order instruction appears with only one speech bubble")
	}
}

// A page's job is to draw speech bubbles, so a "do not draw text" that slipped into the composition notes must not be passed on.
// The same prompt would instruct "do not draw" and "draw this text" at once.
func TestPagePromptDropsTextExclusionsFromScene(t *testing.T) {
	t.Parallel()

	panel := comic.Panel{
		ID: "ch01-p01", Page: 1,
		VisualAnchor: "Cinematic dutch angle, dramatic rim lighting, no speech bubbles, no text",
		Characters:   []comic.PanelCharacter{{CharacterID: "zundamon", Prominence: comic.ProminencePrimary}},
		Dialogues:    []comic.DialogueLine{{SpeakerID: "zundamon", Text: "起動なのだ！", Kind: comic.DialogueKindShout}},
	}

	_, user, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{
		Panels: []comic.Panel{panel}, Characters: testCharacters(t),
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	for _, unwanted := range []string{"no speech bubbles", "no text"} {
		if strings.Contains(user, unwanted) {
			t.Errorf("%q remains in the page prompt:\n%s", unwanted, user)
		}
	}
	// The composition itself is kept
	if !strings.Contains(user, "dramatic rim lighting") {
		t.Error("the composition description was dropped too")
	}
	if !strings.Contains(user, "TEXT_TO_RENDER") {
		t.Error("the instruction for the text to draw is gone")
	}
}

// The panel side is the opposite: it needs an instruction not to draw speech bubbles (handled by a dedicated negative prompt).
func TestPanelNegativePromptStillExcludesText(t *testing.T) {
	t.Parallel()

	_, _, negative, err := testPanelPrompt(t).BuildPanel(&ports.PanelPromptData{
		Panel:      comic.Panel{ID: "ch01-p01", VisualAnchor: "a"},
		Characters: testCharacters(t),
	})
	if err != nil {
		t.Fatalf("BuildPanel() error = %v", err)
	}
	for _, want := range []string{"speech bubble", "text"} {
		if !strings.Contains(negative, want) {
			t.Errorf("the panel negative prompt lacks %q", want)
		}
	}
}

// For a page, the reference is the panel image already generated. A character sheet is a different picture with a
// different ratio and style, and prioritising it causes re-fitting. Only when there is no panel image is the sheet the sole clue.
func TestPagePromptPrefersPanelOverCharacterSheet(t *testing.T) {
	t.Parallel()

	chars := testCharacters(t)
	panel := comic.Panel{
		ID: "ch01-p01", Page: 1, VisualAnchor: "bridge",
		Characters: []comic.PanelCharacter{{CharacterID: "zundamon", Prominence: comic.ProminencePrimary}},
		Generation: &comic.GenerationRecord{ImageURL: "gs://b/panel.png"},
	}

	_, withGuide, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{
		Panels: []comic.Panel{panel}, Characters: chars,
		CharacterFile: map[string]int{"zundamon": 1},
		PanelFile:     map[string]int{"ch01-p01": 2},
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	if !strings.Contains(withGuide, "already appears in input_file_2") {
		t.Errorf("the panel image is not instructed as the reference:\n%s", withGuide)
	}
	if strings.Contains(withGuide, "MUST match input_file_1 exactly") {
		t.Error("an instruction prioritising the character sheet remains")
	}

	// On a page with no panel image, the character sheet is the sole clue.
	_, withoutGuide, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{
		Panels: []comic.Panel{panel}, Characters: chars,
		CharacterFile: map[string]int{"zundamon": 1},
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	if !strings.Contains(withoutGuide, "MUST match input_file_1 exactly") {
		t.Errorf("no instruction to use the character sheet although there is no panel image:\n%s", withoutGuide)
	}
}

// For a character shown in a panel image, that panel is the model.
// Adding the list of character sheets on top conflicts with the per-panel instructions (and the descriptions run close to 700 characters).
func TestPagePromptOmitsMasterListWhenPanelsCoverEveryone(t *testing.T) {
	t.Parallel()

	panel := comic.Panel{
		ID: "ch01-p01", Page: 1, VisualAnchor: "bridge",
		Characters: []comic.PanelCharacter{{CharacterID: "zundamon", Prominence: comic.ProminencePrimary}},
	}

	_, covered, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{
		Panels: []comic.Panel{panel}, Characters: testCharacters(t),
		CharacterFile: map[string]int{"zundamon": 1},
		PanelFile:     map[string]int{"ch01-p01": 2},
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	if strings.Contains(covered, "CHARACTER MASTER REFERENCES") {
		t.Errorf("the list of character sheets appears although there is a panel image:\n%s", covered)
	}
	if strings.Contains(covered, "green hair") {
		t.Error("the character sheet descriptions remain in the page prompt")
	}

	// A character with no panel image keeps the sheet, its sole model.
	_, uncovered, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{
		Panels: []comic.Panel{panel}, Characters: testCharacters(t),
		CharacterFile: map[string]int{"zundamon": 1},
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	for _, want := range []string{"CHARACTER MASTER REFERENCES", "no panel guide", "green hair"} {
		if !strings.Contains(uncovered, want) {
			t.Errorf("%q does not appear although there is no panel image", want)
		}
	}
}
