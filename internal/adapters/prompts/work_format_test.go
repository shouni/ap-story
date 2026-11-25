package prompts

import (
	"slices"
	"strings"
	"testing"

	characterkit "github.com/shouni/go-character-kit/character"
	"github.com/shouni/go-comic-kit/comic"
	"github.com/shouni/go-comic-kit/ports"
)

// This file covers what a work chooses for itself: the language its pages are lettered in,
// sung lines, the page's shape, the script mode's format, and the supplementary sheets a
// panel borrows.

// twoPanelPage is a page whose two panels share a tier, so that left and right matter.
func twoPanelPage(t *testing.T, lang string) *ports.PagePromptData {
	t.Helper()
	return &ports.PagePromptData{
		Panels: []comic.Panel{
			{
				ID:         "ch01-p01",
				Characters: []comic.PanelCharacter{{CharacterID: "zundamon", Prominence: comic.ProminencePrimary}},
				Dialogues: []comic.DialogueLine{
					{SpeakerID: "zundamon", Text: "It still burns."},
					{SpeakerID: "metan", Text: "Then keep it."},
				},
			},
			{ID: "ch01-p02"},
			{ID: "ch01-p03"},
		},
		Characters:  testCharacters(t),
		Lang:        lang,
		AspectRatio: "3:4",
	}
}

func TestPagePromptLettersEnglishLeftToRight(t *testing.T) {
	t.Parallel()

	ja := twoPanelPage(t, "")
	jaSystem, jaUser, _, err := testPagePrompt(t).BuildPage(ja)
	if err != nil {
		t.Fatalf("BuildPage(ja) error = %v", err)
	}
	en := twoPanelPage(t, "en")
	enSystem, enUser, _, err := testPagePrompt(t).BuildPage(en)
	if err != nil {
		t.Fatalf("BuildPage(en) error = %v", err)
	}

	if !strings.Contains(jaSystem, "READING FLOW: Right-to-Left") || !strings.Contains(enSystem, "READING FLOW: Left-to-Right") {
		t.Errorf("the system prompt does not follow the language:\nja: %s\nen: %s", jaSystem, enSystem)
	}
	if strings.Contains(enSystem, "Right-to-Left") || strings.Contains(enUser, "Right-to-Left") ||
		strings.Contains(enUser, "right-to-left") || strings.Contains(enUser, "Tategaki") || strings.Contains(enUser, "Japanese") {
		t.Errorf("the English page still carries Japanese lettering rules:\n%s", enUser)
	}
	for _, want := range []string{
		"READING ORDER: Western Style (Left-to-Right, then Top-to-Bottom).",
		"read left-to-right then top-to-bottom within this panel",
		"TEXT_DIRECTION: Horizontal, left to right",
		"LANGUAGE: Latin letters. Spell every word exactly as written",
	} {
		if !strings.Contains(enUser, want) {
			t.Errorf("the English page does not contain %q\n%s", want, enUser)
		}
	}

	// The same tier, mirrored: the panel read first sits on the right in Japanese and on the left in English.
	jaFirst := tierLineWith(t, jaUser, "PANEL 1 =")
	enFirst := tierLineWith(t, enUser, "PANEL 1 =")
	if !strings.Contains(jaFirst, "PANEL 1 = RIGHT") || !strings.Contains(enFirst, "PANEL 1 = LEFT") {
		t.Errorf("the first panel's side does not follow the reading direction:\nja: %s\nen: %s", jaFirst, enFirst)
	}
	// Sizes and tier membership must not depend on the language, only the sides.
	if tierSizes(enFirst) != tierSizes(jaFirst) {
		t.Errorf("the tier's sizes changed with the language:\nja: %s\nen: %s", jaFirst, enFirst)
	}
}

func tierLineWith(t *testing.T, prompt, marker string) string {
	t.Helper()
	for _, line := range strings.Split(prompt, "\n") {
		if strings.Contains(line, "TIER") && strings.Contains(line, marker) {
			return line
		}
	}
	t.Fatalf("no tier line contains %q in:\n%s", marker, prompt)
	return ""
}

// tierSizes keeps only the digits and percent signs of a tier line.
func tierSizes(line string) string {
	return strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '%' {
			return r
		}
		return -1
	}, line)
}

func TestPagePromptDrawsSungLinesWithoutABalloon(t *testing.T) {
	t.Parallel()
	_, user, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{
		Panels: []comic.Panel{{
			ID: "ch01-p01",
			Dialogues: []comic.DialogueLine{
				{Kind: comic.DialogueKindLyric, Text: "夜明けまで歌うよ"},
				{Kind: comic.DialogueKindLyric, Text: "Fly away!"},
			},
		}},
		Characters: testCharacters(t),
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}

	if got := strings.Count(user, "- LYRIC: Sung words. Free-floating lettering with NO balloon"); got != 2 {
		t.Errorf("sung lines drawn as lyrics = %d, want 2\n%s", got, user)
	}
	if strings.Contains(user, "NARRATION") || strings.Contains(user, "SPEECH") {
		t.Errorf("a sung line with no speaker fell back to a caption or a balloon:\n%s", user)
	}

	japanese, english := lineBlock(t, user, "夜明けまで歌うよ"), lineBlock(t, user, "Fly away!")
	if !strings.Contains(japanese, "LANGUAGE: Japanese characters") || !strings.Contains(japanese, "Vertical (Tategaki) or Horizontal") {
		t.Errorf("the Japanese sung line lost its lettering:\n%s", japanese)
	}
	// An English hook on a Japanese page: told it is "Japanese characters, vertical", the model invents kana.
	if !strings.Contains(english, "LANGUAGE: Latin letters") || !strings.Contains(english, "TEXT_DIRECTION: Horizontal, left to right") {
		t.Errorf("a line with no Japanese characters is not set as Latin text:\n%s", english)
	}
}

// lineBlock returns the TEXT_TO_RENDER line for the given text and the lettering lines after it.
func lineBlock(t *testing.T, prompt, text string) string {
	t.Helper()
	lines := strings.Split(prompt, "\n")
	for i, line := range lines {
		if strings.Contains(line, "TEXT_TO_RENDER") && strings.Contains(line, text) {
			return strings.Join(lines[i:min(i+4, len(lines))], "\n")
		}
	}
	t.Fatalf("no TEXT_TO_RENDER line for %q in:\n%s", text, prompt)
	return ""
}

func TestPagePromptWordsThePageShape(t *testing.T) {
	t.Parallel()
	for ratio, want := range map[string]string{
		"":     "ONE single portrait manga page image.",
		"3:4":  "ONE single portrait manga page image.",
		"9:16": "ONE single tall portrait (9:16) manga page image.",
		"1:1":  "ONE single square (1:1) manga page image.",
	} {
		_, user, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{
			Panels: []comic.Panel{{ID: "ch01-p01"}}, Characters: testCharacters(t), AspectRatio: ratio,
		})
		if err != nil {
			t.Fatalf("BuildPage(%q) error = %v", ratio, err)
		}
		if !strings.Contains(user, want) {
			t.Errorf("ratio %q: prompt does not contain %q\n%s", ratio, want, user)
		}
	}
}

func sheetCharacters(t *testing.T) *comic.Characters {
	t.Helper()
	cm, err := characterkit.NewCharacters([]comic.Character{
		{
			ID: "zundamon", Name: "ずんだもん", ReferenceURL: "gs://b/zunda.png", IsDefault: true,
			VisualCues: []string{"green hair", "soft smooth gradients"},
		},
		{ID: "metan", Name: "めたん", ReferenceURL: "gs://b/metan.png", VisualCues: []string{"purple hair"}},
	})
	if err != nil {
		t.Fatalf("NewCharacters failed: %v", err)
	}
	return cm
}

func TestPanelPromptExplainsSupplementarySheets(t *testing.T) {
	t.Parallel()
	_, user, _, err := testPanelPrompt(t).BuildPanel(&ports.PanelPromptData{
		Panel:      comic.Panel{ID: "ch01-p01", Shot: "close-up"},
		Characters: sheetCharacters(t),
		SubjectIDs: []string{"metan", "zundamon"},
		Sheets: []ports.PanelSheetRef{
			{CharacterID: "zundamon", Kind: characterkit.SheetExpressions, File: 3},
			{CharacterID: "metan", Kind: characterkit.SheetOutfit, File: 4},
			{CharacterID: "nobody", Kind: characterkit.SheetOutfit, File: 5},
			{CharacterID: "metan", Kind: characterkit.SheetParts, File: 6},
		},
	})
	if err != nil {
		t.Fatalf("BuildPanel() error = %v", err)
	}

	for _, want := range []string{
		// The subject number is the position in SubjectIDs; the file number is the kit's.
		"[Reference image 3: Subject 2's expression sheet (the same face in several moods)] Not another subject.",
		"[Reference image 4: Subject 1's outfit sheet (the costume, front and back)] Not another subject.",
		"never reproduce the sheet's layout",
	} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt does not contain %q\n%s", want, user)
		}
	}
	if strings.Contains(user, "Reference image 5") || strings.Contains(user, "Reference image 6") {
		t.Errorf("a sheet with no subject, or of a kind a panel cannot use, was described:\n%s", user)
	}
}

func TestPanelPromptPutsTheStyleAboveRenderingWordsInTheCues(t *testing.T) {
	t.Parallel()
	build := func(subjects ...string) string {
		_, user, _, err := testPanelPrompt(t).BuildPanel(&ports.PanelPromptData{
			Panel: comic.Panel{ID: "ch01-p01"}, Characters: sheetCharacters(t), SubjectIDs: subjects,
		})
		if err != nil {
			t.Fatalf("BuildPanel() error = %v", err)
		}
		return user
	}

	with := build("zundamon")
	priority, style := strings.Index(with, cuePriority), strings.Index(with, "\nStyle: ")
	if priority < 0 || style < 0 || priority > style {
		t.Errorf("the priority line must come after the subjects and before the style:\n%s", with)
	}
	if subject := strings.Index(with, "[Subject 1"); subject < 0 || subject > priority {
		t.Errorf("the priority line must follow the subject it qualifies:\n%s", with)
	}
	if strings.Contains(build(), cuePriority) {
		t.Error("a panel with no subject carries a line about subject descriptions")
	}
}

func TestScriptModesDeclareTheirFormat(t *testing.T) {
	t.Parallel()
	p, err := NewScriptPrompts()
	if err != nil {
		t.Fatalf("NewScriptPrompts() error = %v", err)
	}

	// The default mode is written for these three by name. Without a cast the script model is
	// also shown every character added to characters.json for other work.
	if got, want := p.ModeInfo("").Cast, []string{"zundamon", "metan", "tsumugi"}; !slices.Equal(got, want) {
		t.Errorf("default cast = %v, want %v", got, want)
	}
	song := p.ModeInfo("song_story")
	if song.MaxChapters != 1 || song.MaxPanels < 4 || song.MaxPanels > comic.DefaultMaxPanelsPerPage {
		t.Errorf("song_story format = %d chapter(s), %d panels; want one chapter that fits one page (4-%d panels)",
			song.MaxChapters, song.MaxPanels, comic.DefaultMaxPanelsPerPage)
	}
	// It names no cast and no work: who appears is the job's choice, and the story is the manuscript's.
	if len(song.Cast) != 0 {
		t.Errorf("song_story cast = %v, want none", song.Cast)
	}
	if got := p.ModeInfo("no-such-mode"); got.MaxChapters != 0 || len(got.Cast) != 0 {
		t.Errorf("an unknown mode has a format: %+v", got)
	}
}

func TestScriptPromptsSwitchLanguageWithoutChangingJapanese(t *testing.T) {
	t.Parallel()
	p, err := NewScriptPrompts()
	if err != nil {
		t.Fatalf("NewScriptPrompts() error = %v", err)
	}

	for _, mode := range p.Modes() {
		outline := func(lang string) string {
			out, err := p.BuildOutline(mode, &ports.OutlinePromptData{InputText: "本文", CharacterRoster: "- id: a", MaxChapters: 3, Lang: lang})
			if err != nil {
				t.Fatalf("BuildOutline(%s, %q) error = %v", mode, lang, err)
			}
			return out
		}
		chapter := func(lang string) string {
			out, err := p.BuildChapterScript(mode, &ports.ChapterPromptData{
				WorkTitle: "T", Chapter: comic.Chapter{ID: "ch01"}, CharacterRoster: "- id: a", MaxPanels: 6, Lang: lang,
			})
			if err != nil {
				t.Fatalf("BuildChapterScript(%s, %q) error = %v", mode, lang, err)
			}
			return out
		}

		// "ja" and the empty string are the same work: one that asked for nothing is Japanese.
		if outline("") != outline("ja") || chapter("") != chapter("ja") {
			t.Errorf("mode %s: an explicit ja changes the prompt", mode)
		}
		for name, pair := range map[string][2]string{
			"outline": {outline(""), outline("en")},
			"chapter": {chapter(""), chapter("en")},
		} {
			if strings.Contains(pair[0], "英語で") {
				t.Errorf("mode %s: the Japanese %s prompt asks for English", mode, name)
			}
			if !strings.Contains(pair[1], "英語で") {
				t.Errorf("mode %s: the English %s prompt never asks for English", mode, name)
			}
		}
	}
}

func TestSongStoryPromptKeepsTheLyricsAndNamesNoWork(t *testing.T) {
	t.Parallel()
	p, err := NewScriptPrompts()
	if err != nil {
		t.Fatalf("NewScriptPrompts() error = %v", err)
	}
	chapter, err := p.BuildChapterScript("song_story", &ports.ChapterPromptData{
		Chapter: comic.Chapter{ID: "ch01", SourceExcerpt: "夜明けまで歌うよ"}, CharacterRoster: "- id: singer", MaxPanels: 6,
	})
	if err != nil {
		t.Fatalf("BuildChapterScript() error = %v", err)
	}
	outline, err := p.BuildOutline("song_story", &ports.OutlinePromptData{InputText: "歌詞", CharacterRoster: "- id: singer", MaxChapters: 1})
	if err != nil {
		t.Fatalf("BuildOutline() error = %v", err)
	}

	for _, want := range []string{
		`kind: "lyric"`,       // the kind the page prompt letters without a balloon
		"原文のまま一字も変えずに",        // a quoted lyric is never reworded
		"4〜6 コマで1ページ",         // the limit reaches the wording
		"`narration` は使わないこと", // the story is told by the art
	} {
		if !strings.Contains(chapter, want) {
			t.Errorf("the chapter prompt does not contain %q", want)
		}
	}
	// The mode serves any song: who sings and what happens arrive in the manuscript and the
	// job's cast, never in the mode.
	for _, text := range []string{outline, chapter} {
		for _, name := range []string{"ずんだもん", "めたん", "つむぎ", "zundamon", "metan", "tsumugi"} {
			if strings.Contains(text, name) {
				t.Errorf("the song_story prompt names a character (%s)", name)
			}
		}
	}
}
