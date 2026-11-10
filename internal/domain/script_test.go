package domain

import (
	"strings"
	"testing"

	kitcomic "github.com/shouni/go-comic-kit/comic"
)

func testState() *kitcomic.MangaState {
	return &kitcomic.MangaState{
		ID:    "job-1",
		Title: "作品",
		Chapters: []kitcomic.Chapter{
			{ID: "ch01", Title: "第1話", Summary: "あらすじ"},
		},
		Panels: []kitcomic.Panel{
			{
				ID: "ch01-p01", ChapterID: "ch01", Page: 1, Shot: "medium",
				Dialogues: []kitcomic.DialogueLine{
					{SpeakerID: "zundamon", Text: "元のセリフなのだ", Kind: kitcomic.DialogueKindSpeech},
				},
				Generation: &kitcomic.GenerationRecord{ImageURL: "gs://b/p01.png", UsedSeed: 42},
			},
			{
				ID: "ch01-p02", ChapterID: "ch01", Page: 1, Shot: "close-up",
				Dialogues: []kitcomic.DialogueLine{
					{SpeakerID: "metan", Text: "そうよ", Kind: kitcomic.DialogueKindSpeech},
				},
			},
		},
	}
}

func knownSpeakers(ids ...string) func(string) bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return func(id string) bool { return set[id] }
}

func TestNewScriptDraftCarriesTheContextNeededToReadTheScript(t *testing.T) {
	t.Parallel()

	draft := NewScriptDraft(testState())

	if draft.JobID != "job-1" || draft.Title != "作品" {
		t.Errorf("the work's identifying fields were dropped: %+v", draft)
	}
	if len(draft.Chapters) != 1 || draft.Chapters[0].Title != "第1話" {
		t.Errorf("the chapter headings were dropped: %+v", draft.Chapters)
	}
	if len(draft.Panels) != 2 {
		t.Fatalf("panel count = %d, want 2", len(draft.Panels))
	}
	// The page number always goes in, because it tells the reader what has to be recomposed after a fix
	if draft.Panels[0].Page != 1 || draft.Panels[0].ChapterID != "ch01" {
		t.Errorf("the chapter and page context were dropped: %+v", draft.Panels[0])
	}
	if draft.Panels[0].Dialogues[0].Text != "元のセリフなのだ" {
		t.Errorf("the dialogue was not extracted: %+v", draft.Panels[0].Dialogues)
	}
}

func TestApplyToReplacesOnlyTheDialogues(t *testing.T) {
	t.Parallel()

	state := testState()
	draft := NewScriptDraft(state)
	draft.Panels[0].Dialogues[0].Text = "直したセリフなのだ"

	if err := draft.ApplyTo(state, knownSpeakers("zundamon", "metan")); err != nil {
		t.Fatalf("ApplyTo() = %v, want nil", err)
	}

	if got := state.Panels[0].Dialogues[0].Text; got != "直したセリフなのだ" {
		t.Errorf("the dialogue was not applied: %q", got)
	}
	// Generation records are the very mapping to the panel images, so editing the script must not touch them
	if state.Panels[0].Generation == nil || state.Panels[0].Generation.UsedSeed != 42 {
		t.Errorf("the generation record was lost: %+v", state.Panels[0].Generation)
	}
	if state.Panels[0].Shot != "medium" || state.Panels[0].Page != 1 {
		t.Errorf("the panel structure was rewritten: %+v", state.Panels[0])
	}
}

func TestApplyToRejectsChangesToThePanelLineup(t *testing.T) {
	t.Parallel()

	speakers := knownSpeakers("zundamon", "metan")
	cases := []struct {
		name   string
		mutate func(*ScriptDraft)
	}{
		{"removing a panel", func(d *ScriptDraft) { d.Panels = d.Panels[:1] }},
		{"adding a panel", func(d *ScriptDraft) { d.Panels = append(d.Panels, ScriptPanel{PanelID: "ch01-p03"}) }},
		{"reordering panels", func(d *ScriptDraft) { d.Panels[0], d.Panels[1] = d.Panels[1], d.Panels[0] }},
		{"renaming a panel ID", func(d *ScriptDraft) { d.Panels[0].PanelID = "ch09-p09" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			state := testState()
			draft := NewScriptDraft(state)
			tt.mutate(&draft)

			if err := draft.ApplyTo(state, speakers); err == nil {
				t.Fatal("a change to the panel structure was accepted")
			}
		})
	}
}

func TestApplyToLeavesTheStateUntouchedWhenAnyPanelIsInvalid(t *testing.T) {
	t.Parallel()

	state := testState()
	draft := NewScriptDraft(state)
	draft.Panels[0].Dialogues[0].Text = "1コマ目は正しい"
	draft.Panels[1].Dialogues[0].Kind = "whisper" // unknown kind

	if err := draft.ApplyTo(state, knownSpeakers("zundamon", "metan")); err == nil {
		t.Fatal("an invalid kind was accepted")
	}
	// A partial apply would leave it "changed even though it failed"
	if got := state.Panels[0].Dialogues[0].Text; got != "元のセリフなのだ" {
		t.Errorf("the first panel was rewritten even though it failed: %q", got)
	}
}

func TestApplyToRejectsAnUnknownSpeaker(t *testing.T) {
	t.Parallel()

	// go-comic-kit turns an unknown speaker into narration when generating a script, but silently
	// turning a hand-written line into something else garbles the line someone meant to fix. The edit path sends it back.
	state := testState()
	draft := NewScriptDraft(state)
	draft.Panels[0].Dialogues[0].SpeakerID = "dareka"

	err := draft.ApplyTo(state, knownSpeakers("zundamon", "metan"))
	if err == nil {
		t.Fatal("an unknown speaker was accepted")
	}
	if !strings.Contains(err.Error(), "dareka") {
		t.Errorf("the error does not say which speaker is the problem: %v", err)
	}
}

func TestApplyToRequiresASpeakerExceptForNarrationAndSFX(t *testing.T) {
	t.Parallel()

	speakers := knownSpeakers("zundamon", "metan")
	cases := []struct {
		kind    string
		wantErr bool
	}{
		{kitcomic.DialogueKindSpeech, true},
		{kitcomic.DialogueKindShout, true},
		{kitcomic.DialogueKindThought, true},
		{"", true},
		{kitcomic.DialogueKindNarration, false},
		{kitcomic.DialogueKindSFX, false},
	}
	for _, tt := range cases {
		t.Run("kind="+tt.kind, func(t *testing.T) {
			state := testState()
			draft := NewScriptDraft(state)
			draft.Panels[0].Dialogues[0].SpeakerID = ""
			draft.Panels[0].Dialogues[0].Kind = tt.kind

			err := draft.ApplyTo(state, speakers)
			if tt.wantErr && err == nil {
				t.Errorf("%q without a speaker was accepted", tt.kind)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("%q without a speaker was rejected: %v", tt.kind, err)
			}
		})
	}
}

func TestApplyToDropsEmptyLinesSoABubbleCanBeRemoved(t *testing.T) {
	t.Parallel()

	state := testState()
	draft := NewScriptDraft(state)
	draft.Panels[0].Dialogues = append(draft.Panels[0].Dialogues, ScriptDialogue{
		SpeakerID: "metan", Text: "   ", Kind: kitcomic.DialogueKindSpeech,
	})

	if err := draft.ApplyTo(state, knownSpeakers("zundamon", "metan")); err != nil {
		t.Fatalf("ApplyTo() = %v, want nil", err)
	}
	if len(state.Panels[0].Dialogues) != 1 {
		t.Errorf("empty lines remain: %+v", state.Panels[0].Dialogues)
	}
}

func TestApplyToRejectsATextLongEnoughToBreakThePage(t *testing.T) {
	t.Parallel()

	state := testState()
	draft := NewScriptDraft(state)
	draft.Panels[0].Dialogues[0].Text = strings.Repeat("あ", MaxDialogueRunes+1)

	if err := draft.ApplyTo(state, knownSpeakers("zundamon", "metan")); err == nil {
		t.Fatal("dialogue over the limit was accepted")
	}
}

func TestApplyToAcceptsTheOverBudgetLinesTheGeneratorAlreadyProduces(t *testing.T) {
	t.Parallel()

	// The script prompt asks for 25 characters or fewer, but generated scripts routinely exceed that.
	// Rejecting on that basis would make merely reading and re-saving an existing script fail.
	state := testState()
	draft := NewScriptDraft(state)
	draft.Panels[0].Dialogues[0].Text = strings.Repeat("あ", 30)

	if err := draft.ApplyTo(state, knownSpeakers("zundamon", "metan")); err != nil {
		t.Fatalf("30-character dialogue was rejected: %v", err)
	}
}

func TestApplyToRejectsMoreBubblesThanAPanelCanHold(t *testing.T) {
	t.Parallel()

	state := testState()
	draft := NewScriptDraft(state)
	for range MaxDialoguesPerPanel {
		draft.Panels[0].Dialogues = append(draft.Panels[0].Dialogues, ScriptDialogue{
			SpeakerID: "metan", Text: "増やす", Kind: kitcomic.DialogueKindSpeech,
		})
	}

	if err := draft.ApplyTo(state, knownSpeakers("zundamon", "metan")); err == nil {
		t.Fatal("a request over the speech-bubble limit was accepted")
	}
}
