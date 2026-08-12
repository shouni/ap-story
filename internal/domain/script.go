package domain

import (
	"fmt"
	"slices"
	"strings"

	kitcomic "github.com/shouni/go-comic-kit/comic"
)

// MaxDialogueRunes は、1つの吹き出しに許すセリフの上限（文字数）です。
//
// 台本プロンプト（assets/prompts/chapter/default.md）が作文に課している「25文字以内、
// 技術語の説明のみ35文字まで」よりずっと緩い値にしてあります。ここは作文の良し悪しでは
// なく、ページ合成が破綻する入力を止めるための番人だからです。25文字を実際に超えている
// セリフは生成済みの台本にも珍しくないので、その基準で弾くと既存の台本を読み込んで
// 保存し直すだけで失敗します。長すぎるセリフへの注意喚起は画面側の役目です。
const MaxDialogueRunes = 120

// MaxDialoguesPerPanel は1コマに置ける吹き出しの数です。
// 台本プロンプトの「1コマの吹き出しは最大3つ」と揃えています。
const MaxDialoguesPerPanel = 3

// ScriptDialogue は台本上の1つの発話です。kitcomic.DialogueLine と同じ形ですが、
// 編集の入出力に使う独立した型として持ちます（state の内部表現が変わっても
// API の形が引きずられないようにするためです）。
type ScriptDialogue struct {
	SpeakerID string `json:"speaker_id"`
	Text      string `json:"text"`
	Kind      string `json:"kind,omitempty"`
}

// ScriptPanel は1コマ分の台本です。
//
// ChapterID / Page / Shot は編集できません。読み手が「どの章のどのページを直しているのか」
// を把握するために載せています。ページ番号が要るのは、セリフを直したときに
// 合成し直す単位がページだからです。
type ScriptPanel struct {
	PanelID   string           `json:"panel_id"`
	ChapterID string           `json:"chapter_id,omitempty"`
	Page      int              `json:"page,omitempty"`
	Shot      string           `json:"shot,omitempty"`
	Dialogues []ScriptDialogue `json:"dialogues"`
}

// ScriptChapter は章の見出しです。台本を通して読むための文脈で、編集対象ではありません。
type ScriptChapter struct {
	ChapterID string `json:"chapter_id"`
	Title     string `json:"title,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

// ScriptDraft は、作品の台本のうち編集できる部分だけを取り出した表現です。
//
// state 全体ではなくこれをやり取りするのは、コマ画像の生成記録（GenerationRecord）や
// ページ成果物を編集の経路に乗せないためです。それらは生成の結果であって入力ではなく、
// 手で書き換えられるようにする理由がありません。
type ScriptDraft struct {
	JobID    string          `json:"job_id"`
	Title    string          `json:"title,omitempty"`
	Chapters []ScriptChapter `json:"chapters,omitempty"`
	Panels   []ScriptPanel   `json:"panels"`
}

// NewScriptDraft は state から編集可能な台本を取り出します。
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

// ApplyTo は台本の編集内容を state のセリフへ反映します。
//
// 差し替わるのは各コマの Dialogues だけです。コマの構成（ID・順序・章・ページ）は
// 生成済みのコマ画像と1対1で対応しているので、ここで動かせてしまうと state と
// 画像の対応が壊れます。そのため「送られてきたコマ列が既存と完全に一致すること」を
// 先に検証し、一致しない要求は部分適用せずに丸ごと拒否します。
func (d ScriptDraft) ApplyTo(state *kitcomic.MangaState, knownSpeaker func(string) bool) error {
	if state == nil {
		return fmt.Errorf("state is required")
	}
	if len(d.Panels) != len(state.Panels) {
		return fmt.Errorf("パネル数が一致しません（要求 %d、実際 %d）。コマの追加・削除はできません", len(d.Panels), len(state.Panels))
	}

	// 検証を全件終えてから書き込みます。途中で失敗して一部だけ反映されると、
	// 呼び出し元から見て「失敗したのに変わっている」状態になるためです。
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

// normalizedDialogues は1コマ分のセリフを検証し、state に書ける形へ整えます。
func (p ScriptPanel) normalizedDialogues(knownSpeaker func(string) bool) ([]kitcomic.DialogueLine, error) {
	if len(p.Dialogues) > MaxDialoguesPerPanel {
		return nil, fmt.Errorf("%s: 吹き出しが %d 個あります（1コマ最大 %d 個）", p.PanelID, len(p.Dialogues), MaxDialoguesPerPanel)
	}

	lines := make([]kitcomic.DialogueLine, 0, len(p.Dialogues))
	for i, line := range p.Dialogues {
		text := strings.TrimSpace(line.Text)
		// 空行は「その吹き出しを消す」意思表示として受け取り、黙って落とします。
		// 画面から1つ分を消したいときに、配列を組み替えさせずに済ませるためです。
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
		// 未知の話者IDは拒否します。go-comic-kit は台本生成時にこれをナレーションへ
		// 落としますが、手で編集した内容を黙って別物にすると、直したつもりの行が
		// 何も言わずナレーションに化けます。書いた本人に返すほうが親切です。
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

// ScriptChange は、台本を書き換えた結果どこが変わったかの要約です。
type ScriptChange struct {
	// ChangedLines は文面・話者・種別のいずれかが変わったセリフの数です。
	ChangedLines int `json:"changed_lines"`
	// AffectedPages は変更のあったコマが載っているページ番号（昇順・重複なし）です。
	//
	// セリフはページ合成のときに画像モデルが描き込むので、保存しただけでは絵は古いままです。
	// 直した文字を絵に載せるために合成し直す対象がこれで、呼び出し側への申し送りになります。
	AffectedPages []int `json:"affected_pages"`
}

// DiffScripts は編集前後の台本を比べ、変更点の要約を返します。
// コマ列が食い違う場合（ApplyTo が拒否する形）は、対応の取れるコマだけを比べます。
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

// countChangedLines は1コマ分のセリフを突き合わせ、変わった行数を数えます。
// 行数そのものが増減した場合は、増減分も変更として数えます。
func countChangedLines(before, after []ScriptDialogue) int {
	changed := max(len(after)-len(before), len(before)-len(after))
	for i := range min(len(before), len(after)) {
		if before[i] != after[i] {
			changed++
		}
	}
	return changed
}

// isKnownDialogueKind は、吹き出しの種別がページ合成プロンプトの扱える値かを返します。
// 空文字は speech として扱われる（prompts.writePanelDialogues の default 節）ため許可します。
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
