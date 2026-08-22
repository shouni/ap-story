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

// TestPanelPromptNumbersSubjectsInAttachmentOrder は、[Subject N] の番号が参照画像の
// 添付順（SubjectIDs）と一致することを確認します。ずれるとモデルは別人の参照画像を
// 見ながら描くことになります。
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
		"Style: Japanese anime style", // 既定プリセットの画風指定が後置される
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
		// 叫びを含む見せゴマなので、2コマとも全幅で縦に積む
		"TIER 1 (65% of the page height): PANEL 1 = FULL WIDTH.",
		"TIER 2 (35% of the page height): PANEL 2 = FULL WIDTH.",
		// コマ画像がある側が正解なので、立ち絵の一覧は出さない（下の専用テスト参照）。
		"SUBJECT [めたん]: Match input_file_2",
		"COMPOSITION_GUIDE: Recreate the composition, posing, background, and character appearance from input_file_3",
		// コマ画像がある側が正解。立ち絵は細部の補助に降りる。
		"CHARACTER_IDENTITY: [ ずんだもん ] already appears in input_file_3",
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
	if !strings.Contains(system, "READING FLOW: Right-to-Left") || !strings.Contains(system, "### ARTISTIC STYLE ###") {
		t.Error("system prompt missing the format rules or the style section")
	}
}

// TestPagePromptFullWidthImpactForOddCount は、演出指定のないページでも最後のコマが
// 全幅の見せゴマになることを確認します（ページ最後は次への引きなので広く取ります）。
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

// TestPageLayoutFollowsPanelContent は、割り付けがコマの中身で決まることを確認します。
// 一律の2列グリッドだと、章頭に必ず来る引きの画が半幅に潰れます。
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
		{0, columnFull},  // 章頭の引き
		{1, columnRight}, // 掛け合い。相方の無言の寄りより広く取る
		{2, columnLeft},
		{5, columnFull}, // 叫びで締める見せゴマ
	} {
		if got := layout[tc.panel].Column; got != tc.column {
			t.Errorf("PANEL %d の位置 = %s, want %s", tc.panel+1, got, tc.column)
		}
	}
	if layout[1].Width <= layout[2].Width {
		t.Errorf("掛け合いのコマが無言の寄りより広くない: %d%% vs %d%%", layout[1].Width, layout[2].Width)
	}
	if layout[5].Height <= layout[2].Height {
		t.Errorf("締めのコマの段が説明の段より高くない: %d%% vs %d%%", layout[5].Height, layout[2].Height)
	}
}

// 段の高さの合計は必ず 100% です。端数が残ると、そこがページ下端の空白の帯になります。
func TestPageLayoutTierHeightsFillThePage(t *testing.T) {
	t.Parallel()

	for n := 1; n <= 12; n++ {
		panels := make([]comic.Panel, n)
		for i := range panels {
			panels[i] = comic.Panel{ID: fmt.Sprintf("p%d", i+1)}
		}
		layout := planPageLayout(panels)
		if len(layout) != n {
			t.Fatalf("n=%d: 割り付けが %d 件", n, len(layout))
		}

		total := 0
		for _, tier := range groupTiers(layout) {
			if tier.Height <= 0 {
				t.Errorf("n=%d: TIER %d の高さが %d%%", n, tier.Number, tier.Height)
			}
			total += tier.Height
			width := 0
			for _, i := range tier.Panels {
				width += layout[i].Width
			}
			if width != 100 {
				t.Errorf("n=%d: TIER %d の幅の合計が %d%%", n, tier.Number, width)
			}
		}
		if total != 100 {
			t.Errorf("n=%d: 段の高さの合計が %d%%", n, total)
		}
	}
}

// 配置マップとコマごとの指示は同じ割り付けから書くこと。別々に組み立てると、
// 同じページに食い違う位置指示が並びます。
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
		t.Fatalf("配置マップに載ったコマが %d 件:\n%s", len(mapTier), user)
	}
	for _, m := range regexp.MustCompile(`### PANEL (\d+) \[[^\]]+\]\n- POSITION: Tier (\d+) of`).FindAllStringSubmatch(user, -1) {
		panel, _ := strconv.Atoi(m[1])
		tier, _ := strconv.Atoi(m[2])
		if mapTier[panel] != tier {
			t.Errorf("PANEL %d: 配置マップは TIER %d、コマの指示は TIER %d", panel, mapTier[panel], tier)
		}
		delete(mapTier, panel)
	}
	if len(mapTier) != 0 {
		t.Errorf("配置マップにあってコマの指示に無い: %v", mapTier)
	}
}

// 狭いコマに吹き出しが複数入るときは、そのことを伝えます。絵が全部隠れます。
func TestPagePromptFlagsBalloonCrowdingInNarrowPanels(t *testing.T) {
	t.Parallel()

	chatter := []comic.DialogueLine{
		{SpeakerID: "zundamon", Kind: comic.DialogueKindSpeech, Text: "どうなってるのだ"},
		{SpeakerID: "metan", Kind: comic.DialogueKindSpeech, Text: "落ち着きなさい"},
	}
	// 引きで始まり引きで締めるページ。真ん中の段だけが半幅になる。
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
		t.Fatalf("前提が崩れている: 半幅のコマが無い（%+v）", layout)
	}
	if !strings.Contains(user, "BALLOON SPACE: 2 balloons must fit inside this narrow panel") {
		t.Errorf("狭いコマの吹き出し過密を伝えていない:\n%s", user)
	}

	// 全幅のコマには要らない。狭くないので言うだけ雑音になる。
	_, single, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{
		Panels: panels[1:2], Characters: testCharacters(t),
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	if strings.Contains(single, "BALLOON SPACE") {
		t.Error("全幅のコマに狭さの注意が出ている")
	}
}

// コマの無いページは作れません。0コマのまま組むと「PANEL COUNT: [ 0 ]」を渡すことになります。
func TestPagePromptRejectsEmptyPage(t *testing.T) {
	t.Parallel()

	if _, _, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{Characters: testCharacters(t)}); err == nil {
		t.Error("0コマのページがエラーにならない")
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

// testStyles は埋め込みの画風プリセットを読み込みます。
// プロンプト実装は画風の解決をプリセットに委ねるため、テストでも本物を使います
// （文言そのものの検証は style_test.go 側で行います）。
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

// 1コマに複数の発話があるときは読む順序を指示すること。
// 順序を言わないと吹き出しの配置が絵の都合で決まり、掛け合いが逆順に読めます。
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
		t.Errorf("複数の吹き出しがあるのに読み順の指示が無い:\n%s", user)
	}
	// 話者ごとに誰の吹き出しかが伝わること
	for _, want := range []string{"ずんだもん", "めたん", `"全自動なのだ！"`, `"勘違いよ。"`} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt missing %q", want)
		}
	}

	// 1つだけのときは余計な指示を出さない
	single := multi
	single.Dialogues = multi.Dialogues[:1]
	_, user, _, err = testPagePrompt(t).BuildPage(&ports.PagePromptData{
		Panels: []comic.Panel{single}, Characters: chars,
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	if strings.Contains(user, "BALLOON ORDER") {
		t.Error("吹き出しが1つなのに読み順の指示が出ている")
	}
}

// ページはフキダシを描くのが仕事なので、構図メモに紛れた「文字を描くな」を渡してはいけません。
// 同じプロンプトで「描くな」と「この文字を描け」を同時に指示することになります。
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
			t.Errorf("ページのプロンプトに %q が残っている:\n%s", unwanted, user)
		}
	}
	// 構図そのものは残すこと
	if !strings.Contains(user, "dramatic rim lighting") {
		t.Error("構図の記述まで落としている")
	}
	if !strings.Contains(user, "TEXT_TO_RENDER") {
		t.Error("描くべき文字の指示が消えている")
	}
}

// コマ側は逆で、フキダシを描かせない指定が要ります（専用のネガティブプロンプトが担当）。
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
			t.Errorf("コマのネガティブプロンプトに %q が無い", want)
		}
	}
}

// ページにとっての正解は、既に生成されたコマ画像です。立ち絵は比率も絵柄も違う別の絵で、
// 優先させると寄せ直しが起きます。コマ画像が無いときだけ立ち絵が唯一の手がかりになります。
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
		t.Errorf("コマ画像を正解として指示していない:\n%s", withGuide)
	}
	if strings.Contains(withGuide, "MUST match input_file_1 exactly") {
		t.Error("立ち絵を優先させる指示が残っている")
	}

	// コマ画像が無いページでは、立ち絵が唯一の手がかりになる。
	_, withoutGuide, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{
		Panels: []comic.Panel{panel}, Characters: chars,
		CharacterFile: map[string]int{"zundamon": 1},
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	if !strings.Contains(withoutGuide, "MUST match input_file_1 exactly") {
		t.Errorf("コマ画像が無いのに立ち絵を使う指示が出ていない:\n%s", withoutGuide)
	}
}

// コマ画像に写っているキャラは、そのコマが手本です。
// 立ち絵の一覧を重ねて出すと、コマ単位の指示と競合します（説明文も700字近くあります）。
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
		t.Errorf("コマ画像があるのに立ち絵の一覧が出ている:\n%s", covered)
	}
	if strings.Contains(covered, "green hair") {
		t.Error("立ち絵の説明文がページのプロンプトに残っている")
	}

	// コマ画像が無いキャラは、立ち絵が唯一の手本なので残す。
	_, uncovered, _, err := testPagePrompt(t).BuildPage(&ports.PagePromptData{
		Panels: []comic.Panel{panel}, Characters: testCharacters(t),
		CharacterFile: map[string]int{"zundamon": 1},
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	for _, want := range []string{"CHARACTER MASTER REFERENCES", "no panel guide", "green hair"} {
		if !strings.Contains(uncovered, want) {
			t.Errorf("コマ画像が無いのに %q が出ていない", want)
		}
	}
}
