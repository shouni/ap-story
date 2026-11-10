package domain

import (
	"fmt"
	"slices"
	"strings"

	kitcomic "github.com/shouni/go-comic-kit/comic"
)

// MaxDialogueRunes is the limit (in characters) on the dialogue in one speech bubble.
//
// It is far looser than what the script prompt (assets/prompts/chapter/default.md) asks of the writing,
// "25 characters or fewer, up to 35 only for explaining technical terms". This is not about good
// writing; it is a guard that stops input that would break page composition. Dialogue over 25
// characters is common in generated scripts too, so rejecting on that basis would make reading and
// re-saving an existing script fail. Warning about overly long dialogue is the screen's job.
const MaxDialogueRunes = 120

// MaxDialoguesPerPanel is the number of speech bubbles one panel can hold.
// It matches the script prompt's "at most 3 speech bubbles per panel".
const MaxDialoguesPerPanel = 3

// ScriptDialogue is one utterance in the script. It has the same shape as kitcomic.DialogueLine but
// is kept as a separate type for editing input and output (so that a change in the state's
// internal representation does not drag the API shape along).
type ScriptDialogue struct {
	SpeakerID string `json:"speaker_id"`
	Text      string `json:"text"`
	Kind      string `json:"kind,omitempty"`
}

// ScriptPanel is the script for one panel.
//
// ChapterID / Page / Shot cannot be edited. They are included so the reader can tell "which page
// of which chapter is being fixed". The page number is needed because the page is the unit
// that gets recomposed when dialogue is fixed.
type ScriptPanel struct {
	PanelID   string           `json:"panel_id"`
	ChapterID string           `json:"chapter_id,omitempty"`
	Page      int              `json:"page,omitempty"`
	Shot      string           `json:"shot,omitempty"`
	Dialogues []ScriptDialogue `json:"dialogues"`
}

// ScriptChapter is a chapter heading: context for reading the script through, not something to edit.
type ScriptChapter struct {
	ChapterID string `json:"chapter_id"`
	Title     string `json:"title,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

// ScriptDraft is the editable part of a work's script.
//
// This travels instead of the whole state so that panel-image generation records (GenerationRecord)
// and page artifacts never ride the editing path. They are results of generation, not inputs,
// and there is no reason to let them be rewritten by hand.
type ScriptDraft struct {
	JobID    string          `json:"job_id"`
	Title    string          `json:"title,omitempty"`
	Chapters []ScriptChapter `json:"chapters,omitempty"`
	Panels   []ScriptPanel   `json:"panels"`
}

// NewScriptDraft extracts an editable script from a state.
func NewScriptDraft(state *kitcomic.MangaState) ScriptDraft {
	if state == nil {
		return ScriptDraft{}
	}

	draft := ScriptDraft{
		JobID:    state.ID,
		Title:    state.Title,
		Chapters: make([]ScriptChapter, 0, len(state.Chapters)),
		Panels:   make([]ScriptPanel, 0, len(state.Panels)),
	}
	for _, chapter := range state.Chapters {
		draft.Chapters = append(draft.Chapters, ScriptChapter{
			ChapterID: chapter.ID,
			Title:     chapter.Title,
			Summary:   chapter.Summary,
		})
	}
	for _, panel := range state.Panels {
		lines := make([]ScriptDialogue, 0, len(panel.Dialogues))
		for _, line := range panel.Dialogues {
			lines = append(lines, ScriptDialogue{
				SpeakerID: line.SpeakerID,
				Text:      line.Text,
				Kind:      line.Kind,
			})
		}
		draft.Panels = append(draft.Panels, ScriptPanel{
			PanelID:   panel.ID,
			ChapterID: panel.ChapterID,
			Page:      panel.Page,
			Shot:      panel.Shot,
			Dialogues: lines,
		})
	}
	return draft
}

// ApplyTo applies the script edits to the state's dialogue.
//
// Only each panel's Dialogues is replaced. The panel structure (ID, order, chapter, page)
// corresponds one-to-one with the generated panel images, so letting it move here would break
// the mapping between the state and the images. Therefore "the submitted panel sequence matches
// the existing one exactly" is checked first, and a mismatching request is rejected whole, never applied in part.
func (d ScriptDraft) ApplyTo(state *kitcomic.MangaState, knownSpeaker func(string) bool) error {
	if state == nil {
		return fmt.Errorf("state is required")
	}
	if len(d.Panels) != len(state.Panels) {
		return fmt.Errorf("パネル数が一致しません（要求 %d、実際 %d）。コマの追加・削除はできません", len(d.Panels), len(state.Panels))
	}

	// Writes happen only after every check has passed. Failing midway with only part applied would
	// leave the caller looking at a state that "changed even though it failed".
	applied := make([][]kitcomic.DialogueLine, len(state.Panels))
	for i, panel := range d.Panels {
		if panel.PanelID != state.Panels[i].ID {
			return fmt.Errorf("%d 番目のパネルIDが一致しません（要求 %q、実際 %q）。コマの並べ替えはできません", i+1, panel.PanelID, state.Panels[i].ID)
		}
		lines, err := panel.normalizedDialogues(knownSpeaker)
		if err != nil {
			return err
		}
		applied[i] = lines
	}

	for i := range state.Panels {
		state.Panels[i].Dialogues = applied[i]
	}
	return nil
}

// normalizedDialogues validates one panel's dialogue and shapes it for writing to the state.
func (p ScriptPanel) normalizedDialogues(knownSpeaker func(string) bool) ([]kitcomic.DialogueLine, error) {
	if len(p.Dialogues) > MaxDialoguesPerPanel {
		return nil, fmt.Errorf("%s: 吹き出しが %d 個あります（1コマ最大 %d 個）", p.PanelID, len(p.Dialogues), MaxDialoguesPerPanel)
	}

	lines := make([]kitcomic.DialogueLine, 0, len(p.Dialogues))
	for i, line := range p.Dialogues {
		text := strings.TrimSpace(line.Text)
		// An empty line is taken as the intent to "delete that speech bubble" and dropped silently,
		// so that removing one from the screen does not require rearranging the array.
		if text == "" {
			continue
		}
		if count := len([]rune(text)); count > MaxDialogueRunes {
			return nil, fmt.Errorf("%s の %d 番目のセリフが %d 文字あります（上限 %d 文字）", p.PanelID, i+1, count, MaxDialogueRunes)
		}

		kind := strings.TrimSpace(line.Kind)
		if !isKnownDialogueKind(kind) {
			return nil, fmt.Errorf("%s の %d 番目のセリフの kind が不正です: %q", p.PanelID, i+1, line.Kind)
		}

		speaker := strings.TrimSpace(line.SpeakerID)
		// Unknown speaker IDs are rejected. go-comic-kit turns them into narration when generating
		// a script, but silently turning hand-edited content into something else makes the line
		// someone meant to fix become narration without a word. Sending it back to its author is kinder.
		if speaker != "" && knownSpeaker != nil && !knownSpeaker(speaker) {
			return nil, fmt.Errorf("%s の %d 番目のセリフの話者 %q は登録されていません", p.PanelID, i+1, speaker)
		}
		if speaker == "" && kind != kitcomic.DialogueKindNarration && kind != kitcomic.DialogueKindSFX {
			return nil, fmt.Errorf("%s の %d 番目のセリフに話者がありません（話者なしで書けるのは %s と %s だけです）",
				p.PanelID, i+1, kitcomic.DialogueKindNarration, kitcomic.DialogueKindSFX)
		}

		lines = append(lines, kitcomic.DialogueLine{
			SpeakerID: speaker,
			Text:      text,
			Kind:      kind,
		})
	}
	return lines, nil
}

// ScriptChange summarises what changed as a result of rewriting the script.
type ScriptChange struct {
	// ChangedLines is the number of dialogue lines whose text, speaker or kind changed.
	ChangedLines int `json:"changed_lines"`
	// AffectedPages are the page numbers (ascending, no duplicates) holding the changed panels.
	//
	// The image model draws the dialogue in at page composition, so saving alone leaves the art as it was.
	// These are what must be recomposed to put the fixed text into the art, handed on to the caller.
	AffectedPages []int `json:"affected_pages"`
}

// DiffScripts compares the script before and after an edit and returns a summary of the changes.
// When the panel sequences disagree (the shape ApplyTo rejects), only the panels that correspond are compared.
func DiffScripts(before, after ScriptDraft) ScriptChange {
	previous := make(map[string][]ScriptDialogue, len(before.Panels))
	for _, panel := range before.Panels {
		previous[panel.PanelID] = panel.Dialogues
	}

	change := ScriptChange{}
	pages := map[int]bool{}
	for _, panel := range after.Panels {
		count := countChangedLines(previous[panel.PanelID], panel.Dialogues)
		if count == 0 {
			continue
		}
		change.ChangedLines += count
		pages[panel.Page] = true
	}

	change.AffectedPages = make([]int, 0, len(pages))
	for page := range pages {
		change.AffectedPages = append(change.AffectedPages, page)
	}
	slices.Sort(change.AffectedPages)
	return change
}

// countChangedLines compares one panel's dialogue and counts the changed lines.
// When the number of lines itself changed, the difference counts as changed too.
func countChangedLines(before, after []ScriptDialogue) int {
	changed := max(len(after)-len(before), len(before)-len(after))
	for i := range min(len(before), len(after)) {
		if before[i] != after[i] {
			changed++
		}
	}
	return changed
}

// isKnownDialogueKind reports whether a speech-bubble kind is a value the page-composition prompt handles.
// The empty string is treated as speech (the default case of prompts.writePanelDialogues), so it is allowed.
func isKnownDialogueKind(kind string) bool {
	switch kind {
	case "",
		kitcomic.DialogueKindSpeech,
		kitcomic.DialogueKindThought,
		kitcomic.DialogueKindShout,
		kitcomic.DialogueKindNarration,
		kitcomic.DialogueKindSFX:
		return true
	default:
		return false
	}
}
