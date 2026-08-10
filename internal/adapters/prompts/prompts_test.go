package prompts

import (
	"strings"
	"testing"

	"github.com/shouni/go-comic-kit/comic"

	"github.com/shouni/go-comic-kit/ports"
)

func TestScriptPromptsBuildOutline(t *testing.T) {
	t.Parallel()

	p, err := NewScriptPrompts()
	if err != nil {
		t.Fatalf("NewScriptPrompts failed: %v", err)
	}

	out, err := p.BuildOutline(ModeDefault, &ports.OutlinePromptData{
		InputText:       "テスト元文章",
		CharacterRoster: "- zundamon: ずんだもん",
		MaxChapters:     3,
	})
	if err != nil {
		t.Fatalf("BuildOutline failed: %v", err)
	}
	for _, want := range []string{"ずんだもん", "めたん", "つむぎ", "テスト元文章", "3"} {
		if !strings.Contains(out, want) {
			t.Errorf("outline prompt missing %q, got: %s", want, out)
		}
	}
}

func TestScriptPromptsBuildOutlineDefaultsToDefaultMode(t *testing.T) {
	t.Parallel()

	p, err := NewScriptPrompts()
	if err != nil {
		t.Fatalf("NewScriptPrompts failed: %v", err)
	}

	out, err := p.BuildOutline("", &ports.OutlinePromptData{InputText: "x", MaxChapters: 1})
	if err != nil {
		t.Fatalf("BuildOutline with empty mode failed: %v", err)
	}
	if !strings.Contains(out, "x") {
		t.Errorf("outline prompt = %q, want it to contain input text", out)
	}
}

func TestScriptPromptsBuildOutlineUnknownMode(t *testing.T) {
	t.Parallel()

	p, err := NewScriptPrompts()
	if err != nil {
		t.Fatalf("NewScriptPrompts failed: %v", err)
	}

	if _, err := p.BuildOutline("does-not-exist", &ports.OutlinePromptData{}); err == nil {
		t.Error("BuildOutline with unknown mode succeeded, want error")
	}
}

func TestScriptPromptsBuildChapterScript(t *testing.T) {
	t.Parallel()

	p, err := NewScriptPrompts()
	if err != nil {
		t.Fatalf("NewScriptPrompts failed: %v", err)
	}

	out, err := p.BuildChapterScript(ModeDefault, &ports.ChapterPromptData{
		WorkTitle:       "テスト作品",
		WorkDescription: "あらすじ",
		OutlineDigest:   "第1章",
		Chapter: comic.Chapter{
			ID:      "ch1",
			Title:   "章タイトル",
			Summary: "章の狙い",
		},
		CharacterRoster: "- zundamon: ずんだもん",
		MaxPanels:       10,
	})
	if err != nil {
		t.Fatalf("BuildChapterScript failed: %v", err)
	}
	for _, want := range []string{"ずんだもん", "めたん", "つむぎ", "テスト作品", "章タイトル", "10"} {
		if !strings.Contains(out, want) {
			t.Errorf("chapter prompt missing %q, got: %s", want, out)
		}
	}
}
