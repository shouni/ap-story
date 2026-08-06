package prompts

import (
	"strings"
	"testing"

	characterkit "github.com/shouni/go-character-kit/character"
	"github.com/shouni/go-comic-kit/ports"
)

func testCharacters(t *testing.T) *ports.Characters {
	t.Helper()
	cm, err := characterkit.NewCharacters([]ports.Character{
		{ID: "zundamon", Name: "ずんだもん", ReferenceURL: "gs://b/zunda.png", VisualCues: []string{"green hair"}, IsDefault: true},
		{ID: "metan", Name: "めたん", ReferenceURL: "gs://b/metan.png", VisualCues: []string{"purple hair"}},
	})
	if err != nil {
		t.Fatalf("NewCharacters failed: %v", err)
	}
	return cm
}

// TestPanelPromptNumbersSubjectsInAttachmentOrder は、[Subject N] の番号が参照画像の
// 添付順（SubjectIDs）と一致することを確認します。ずれるとモデルは別人の参照画像を
// 見ながら描くことになります。
func TestPanelPromptNumbersSubjectsInAttachmentOrder(t *testing.T) {
	t.Parallel()
	cm := testCharacters(t)

	system, user, negative, err := PanelPrompt{}.BuildPanel(&ports.PanelPromptData{
		Panel: ports.Panel{
			ID:      "ch01-p01",
			Shot:    "wide",
			Setting: "放課後の音楽室",
			Characters: []ports.PanelCharacter{
				{CharacterID: "zundamon", Prominence: ports.ProminencePrimary, Emotion: "驚き"},
				{CharacterID: "metan", Prominence: ports.ProminenceSecondary},
				{CharacterID: "students", Prominence: ports.ProminenceBackground, Action: "ざわめく"},
			},
		},
		Characters:  cm,
		SubjectIDs:  []string{"zundamon", "metan"},
		StyleSuffix: "cinematic style",
	})
	if err != nil {
		t.Fatalf("BuildPanel() error = %v", err)
	}

	for _, want := range []string{
		"[Subject 1: ずんだもん (green hair)] emotion: 驚き.",
		"[Subject 2: めたん (purple hair)]",
		"Background extras (no reference, generic): students (ざわめく)",
		"Style: cinematic style",
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

// TestPagePromptLayoutAndReferences は、コマ配置マップ・参照番号・セリフ種別の描画指示を
// 確認します（go-comic-kit から移設した作り込み部分の回帰テストです）。
func TestPagePromptLayoutAndReferences(t *testing.T) {
	t.Parallel()
	cm := testCharacters(t)

	data := &ports.PagePromptData{
		Panels: []ports.Panel{
			{
				ID: "ch01-p01", Shot: "wide",
				Characters: []ports.PanelCharacter{{CharacterID: "zundamon", Prominence: ports.ProminencePrimary}},
				Dialogues: []ports.DialogueLine{
					{SpeakerID: "zundamon", Kind: ports.DialogueKindShout, Text: "なんなのだ！？"},
					{Kind: ports.DialogueKindNarration, Text: "その時、事件は起きた。"},
				},
			},
			{
				ID:         "ch01-p02",
				Characters: []ports.PanelCharacter{{CharacterID: "metan", Prominence: ports.ProminencePrimary}},
				Dialogues:  []ports.DialogueLine{{SpeakerID: "metan", Text: "落ち着きなさい。"}},
			},
		},
		Characters:    cm,
		CharacterFile: map[string]int{"zundamon": 1, "metan": 2},
		PanelFile:     map[string]int{"ch01-p01": 3},
		StyleSuffix:   "cinematic style",
	}

	system, user, _, err := PagePrompt{}.BuildPage(data)
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}

	for _, want := range []string{
		"PANEL COUNT: [ 2 ]",
		"PANEL 1: ROW 1, RIGHT column",
		"PANEL 2: ROW 1, LEFT column",
		"SUBJECT [ずんだもん]: Match input_file_1",
		"SUBJECT [めたん]: Match input_file_2",
		"COMPOSITION_GUIDE: Recreate the composition, posing, and background from input_file_3",
		"CHARACTER_IDENTITY: [ ずんだもん ] from input_file_1",
		"SHOUT: Jagged, explosive speech bubble for [ずんだもん]",
		"NARRATION: Rectangular caption box",
		`TEXT_TO_RENDER: "なんなのだ！？"`,
		"Horizontal (Yokogaki) or Vertical", // 短い叫びは横書き許可
		`TEXT_TO_RENDER: "落ち着きなさい。"`,
	} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt does not contain %q\nprompt: %s", want, user)
		}
	}
	if !strings.Contains(system, "READING FLOW: Right-to-Left") || !strings.Contains(system, "cinematic style") {
		t.Error("system prompt missing the format rules or the style suffix")
	}
}

// TestPagePromptFullWidthImpactForOddCount は、奇数コマの最後を全幅の見せゴマにする
// レイアウト規則を確認します。
func TestPagePromptFullWidthImpactForOddCount(t *testing.T) {
	t.Parallel()
	cm := testCharacters(t)

	_, user, _, err := PagePrompt{}.BuildPage(&ports.PagePromptData{
		Panels:     []ports.Panel{{ID: "p1"}, {ID: "p2"}, {ID: "p3"}},
		Characters: cm,
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	if !strings.Contains(user, "PANEL 3: BOTTOM ROW, FULL-WIDTH") {
		t.Errorf("prompt does not place the last odd panel full-width:\n%s", user)
	}
	if !strings.Contains(user, "PANEL 3 [FULL-WIDTH IMPACT]") {
		t.Error("prompt does not mark the last odd panel as an impact panel")
	}
}

// TestPromptEditModePreservesComposition は、編集モードが構図の維持を指示することを確認します。
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
