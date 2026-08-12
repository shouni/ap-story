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
		t.Errorf("作品の識別情報が落ちています: %+v", draft)
	}
	if len(draft.Chapters) != 1 || draft.Chapters[0].Title != "第1話" {
		t.Errorf("章の見出しが落ちています: %+v", draft.Chapters)
	}
	if len(draft.Panels) != 2 {
		t.Fatalf("パネル数 = %d, want 2", len(draft.Panels))
	}
	// ページ番号は「直したら何を合成し直すか」を読み手に示すので必ず載せる
	if draft.Panels[0].Page != 1 || draft.Panels[0].ChapterID != "ch01" {
		t.Errorf("章・ページの文脈が落ちています: %+v", draft.Panels[0])
	}
	if draft.Panels[0].Dialogues[0].Text != "元のセリフなのだ" {
		t.Errorf("セリフが取り出せていません: %+v", draft.Panels[0].Dialogues)
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
		t.Errorf("セリフが反映されていません: %q", got)
	}
	// 生成記録はコマ画像との対応そのものなので、台本の編集で触ってはならない
	if state.Panels[0].Generation == nil || state.Panels[0].Generation.UsedSeed != 42 {
		t.Errorf("生成記録が失われています: %+v", state.Panels[0].Generation)
	}
	if state.Panels[0].Shot != "medium" || state.Panels[0].Page != 1 {
		t.Errorf("コマの構成が書き換わっています: %+v", state.Panels[0])
	}
}

func TestApplyToRejectsChangesToThePanelLineup(t *testing.T) {
	t.Parallel()

	speakers := knownSpeakers("zundamon", "metan")
	cases := []struct {
		name   string
		mutate func(*ScriptDraft)
	}{
		{"コマの削除", func(d *ScriptDraft) { d.Panels = d.Panels[:1] }},
		{"コマの追加", func(d *ScriptDraft) { d.Panels = append(d.Panels, ScriptPanel{PanelID: "ch01-p03"}) }},
		{"コマの並べ替え", func(d *ScriptDraft) { d.Panels[0], d.Panels[1] = d.Panels[1], d.Panels[0] }},
		{"パネルIDの改名", func(d *ScriptDraft) { d.Panels[0].PanelID = "ch09-p09" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			state := testState()
			draft := NewScriptDraft(state)
			tt.mutate(&draft)

			if err := draft.ApplyTo(state, speakers); err == nil {
				t.Fatal("コマの構成変更が通っています")
			}
		})
	}
}

func TestApplyToLeavesTheStateUntouchedWhenAnyPanelIsInvalid(t *testing.T) {
	t.Parallel()

	state := testState()
	draft := NewScriptDraft(state)
	draft.Panels[0].Dialogues[0].Text = "1コマ目は正しい"
	draft.Panels[1].Dialogues[0].Kind = "whisper" // 未知の kind

	if err := draft.ApplyTo(state, knownSpeakers("zundamon", "metan")); err == nil {
		t.Fatal("不正な kind が通っています")
	}
	// 部分適用されると「失敗したのに変わっている」状態になる
	if got := state.Panels[0].Dialogues[0].Text; got != "元のセリフなのだ" {
		t.Errorf("失敗したのに1コマ目が書き換わっています: %q", got)
	}
}

func TestApplyToRejectsAnUnknownSpeaker(t *testing.T) {
	t.Parallel()

	// go-comic-kit は台本生成時に未知の話者をナレーションへ落とすが、手で書いた行を
	// 黙って別物にすると直したつもりの行が何も言わず化ける。編集経路では突き返す。
	state := testState()
	draft := NewScriptDraft(state)
	draft.Panels[0].Dialogues[0].SpeakerID = "dareka"

	err := draft.ApplyTo(state, knownSpeakers("zundamon", "metan"))
	if err == nil {
		t.Fatal("未知の話者が通っています")
	}
	if !strings.Contains(err.Error(), "dareka") {
		t.Errorf("どの話者が問題か分からないエラーです: %v", err)
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
				t.Errorf("話者なしの %q が通っています", tt.kind)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("話者なしの %q が拒否されています: %v", tt.kind, err)
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
		t.Errorf("空行が残っています: %+v", state.Panels[0].Dialogues)
	}
}

func TestApplyToRejectsATextLongEnoughToBreakThePage(t *testing.T) {
	t.Parallel()

	state := testState()
	draft := NewScriptDraft(state)
	draft.Panels[0].Dialogues[0].Text = strings.Repeat("あ", MaxDialogueRunes+1)

	if err := draft.ApplyTo(state, knownSpeakers("zundamon", "metan")); err == nil {
		t.Fatal("上限を超えるセリフが通っています")
	}
}

func TestApplyToAcceptsTheOverBudgetLinesTheGeneratorAlreadyProduces(t *testing.T) {
	t.Parallel()

	// 台本プロンプトは25文字以内を求めるが、生成済みの台本はそれを普通に超えている。
	// その基準で弾くと、既存の台本を読んで保存し直すだけで失敗する。
	state := testState()
	draft := NewScriptDraft(state)
	draft.Panels[0].Dialogues[0].Text = strings.Repeat("あ", 30)

	if err := draft.ApplyTo(state, knownSpeakers("zundamon", "metan")); err != nil {
		t.Fatalf("30文字のセリフが拒否されました: %v", err)
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
		t.Fatal("吹き出しの上限を超える要求が通っています")
	}
}
