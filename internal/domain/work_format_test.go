package domain

import (
	"strings"
	"testing"

	kitcomic "github.com/shouni/go-comic-kit/comic"
)

func TestComposeComicValidatesLanguageAndRatio(t *testing.T) {
	t.Parallel()
	base := Task{Command: TaskCommandComposeComic, JobID: "c-20261125-000000-aaaaaaaaaaaa", SourceText: "x"}

	for _, lang := range append(SupportedLangs(), "") {
		task := base
		task.Lang = lang
		if err := task.ValidateSubmission(); err != nil {
			t.Errorf("lang %q rejected: %v", lang, err)
		}
	}
	for _, ratio := range append(ComicAspectRatios(), "") {
		task := base
		task.AspectRatio = ratio
		if err := task.ValidateSubmission(); err != nil {
			t.Errorf("aspect_ratio %q rejected: %v", ratio, err)
		}
	}

	bad := base
	bad.Lang = "fr"
	if err := bad.ValidateSubmission(); err == nil || !strings.Contains(err.Error(), "lang") {
		t.Errorf("an unsupported lang passed (it would be lettered as Japanese without a word): %v", err)
	}
	// The kit can draw 16:9, but the page prompt stacks horizontal tiers, so a comic may not ask for it.
	bad = base
	bad.AspectRatio = "16:9"
	if err := bad.ValidateSubmission(); err == nil || !strings.Contains(err.Error(), "aspect_ratio") {
		t.Errorf("a landscape comic passed: %v", err)
	}
}

func TestDuplicateComicNeedsAnotherWorkToCopy(t *testing.T) {
	t.Parallel()
	const jobID, sourceID = "c-20261125-000000-aaaaaaaaaaaa", "c-20261124-000000-bbbbbbbbbbbb"
	valid := Task{Command: TaskCommandDuplicateComic, JobID: jobID, SourceJobID: sourceID, Lang: LangEnglish}
	if err := valid.ValidateSubmission(); err != nil {
		t.Fatalf("a valid duplicate was rejected: %v", err)
	}

	for name, mutate := range map[string]func(*Task){
		"no source":        func(t *Task) { t.SourceJobID = "" },
		"malformed source": func(t *Task) { t.SourceJobID = "../etc" },
		"copy onto itself": func(t *Task) { t.SourceJobID = t.JobID },
		"unknown language": func(t *Task) { t.Lang = "fr" },
	} {
		task := valid
		mutate(&task)
		if err := task.ValidateSubmission(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// A copy is asked for again on purpose (a second language), so its task name must not collide.
	if !valid.repeatable() {
		t.Error("duplicate_comic is deduplicated by name; a resubmission would be dropped silently")
	}
}

func TestScriptEditAcceptsSungLinesWithoutASpeaker(t *testing.T) {
	t.Parallel()
	state := &kitcomic.MangaState{Panels: []kitcomic.Panel{{ID: "ch01-p01"}}}
	draft := NewScriptDraft(state)
	draft.Panels[0].Dialogues = []ScriptDialogue{{Text: "夜明けまで歌うよ", Kind: kitcomic.DialogueKindLyric}}

	if err := draft.ApplyTo(state, func(string) bool { return true }); err != nil {
		t.Fatalf("a sung line with no speaker was rejected: %v", err)
	}
	if got := state.Panels[0].Dialogues[0]; got.Kind != kitcomic.DialogueKindLyric || got.SpeakerID != "" {
		t.Errorf("saved line = %+v, want a lyric with no speaker", got)
	}

	// Speech still needs someone to say it.
	draft.Panels[0].Dialogues = []ScriptDialogue{{Text: "誰？", Kind: kitcomic.DialogueKindSpeech}}
	if err := draft.ApplyTo(state, func(string) bool { return true }); err == nil {
		t.Error("speech with no speaker was accepted")
	}
}
