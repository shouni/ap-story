package prompts

import (
	"strings"
	"testing"

	"github.com/shouni/go-comic-kit/comic"
	"github.com/shouni/go-comic-kit/ports"
	"github.com/stretchr/testify/require"

	characterkit "github.com/shouni/go-character-kit/character"
)

func TestStylesLoadsPresets(t *testing.T) {
	t.Parallel()

	s, err := NewStyles()
	require.NoError(t, err)
	require.Contains(t, s.Modes(), ModeDefault)

	for _, mode := range s.Modes() {
		style, err := s.Get(mode)
		require.NoError(t, err, "mode=%s", mode)
		require.NotEmpty(t, style.Suffix, "mode=%s", mode)
		require.NotEmpty(t, style.Direction, "mode=%s: 選択肢の説明がありません", mode)
	}
}

// 空文字は既定モードを指します。フォーム未選択と worker 側の解決を一致させるためです。
func TestStylesEmptyModeIsDefault(t *testing.T) {
	t.Parallel()

	s, err := NewStyles()
	require.NoError(t, err)

	empty, err := s.Get("")
	require.NoError(t, err)
	def, err := s.Get(ModeDefault)
	require.NoError(t, err)
	require.Equal(t, def, empty)
}

// 未知のモードは既定へ落とさずエラーにします（指定したつもりの画風で生成されない事故を防ぐ）。
func TestStylesUnknownModeFails(t *testing.T) {
	t.Parallel()

	s, err := NewStyles()
	require.NoError(t, err)

	_, err = s.Get("no-such-style")
	require.Error(t, err)
}

// 画風とネガティブプロンプトが噛み合っていること。共通側に画風の語が残っていると、
// モノクロのプリセットが自分の指定と正面衝突します。
func TestStylePresetsDoNotContradictSharedNegativePrompts(t *testing.T) {
	t.Parallel()

	s, err := NewStyles()
	require.NoError(t, err)

	for _, shared := range []string{panelNegativePrompt, pageNegativePrompt} {
		for _, word := range []string{"monochrome", "greyscale", "screentone", "color"} {
			require.NotContains(t, shared, word,
				"共通のネガティブプロンプトに画風の語 %q が残っています（プリセット側で持ってください）", word)
		}
	}

	mono, err := s.Get("manga_mono")
	require.NoError(t, err)
	require.Contains(t, mono.Suffix, "monochrome")
	require.NotContains(t, mono.Negative, "monochrome")
}

// パネルとページのプロンプトが、選ばれた画風の指定とネガティブを反映すること。
func TestPromptsUseSelectedStyle(t *testing.T) {
	t.Parallel()

	styles, err := NewStyles()
	require.NoError(t, err)
	mono, err := styles.Get("manga_mono")
	require.NoError(t, err)

	chars, err := characterkit.NewCharacters([]comic.Character{
		{ID: "zundamon", Name: "ずんだもん", ReferenceURL: "gs://b/z.png", VisualCues: []string{"green hair"}, IsDefault: true},
	})
	require.NoError(t, err)

	panel := comic.Panel{
		ID:           "ch01-p01",
		VisualAnchor: "sunset classroom",
		Characters:   []comic.PanelCharacter{{CharacterID: "zundamon", Prominence: comic.ProminencePrimary}},
	}

	_, panelUser, panelNegative, err := PanelPrompt{Styles: styles}.BuildPanel(&ports.PanelPromptData{
		Panel:      panel,
		Characters: chars,
		SubjectIDs: []string{"zundamon"},
		StyleMode:  "manga_mono",
	})
	require.NoError(t, err)
	require.Contains(t, panelUser, mono.Suffix)
	require.NotContains(t, panelUser, "設定の画風")
	require.Contains(t, panelNegative, mono.Negative)
	require.Contains(t, panelNegative, "bad anatomy", "共通のネガティブプロンプトが落ちています")

	pageSystem, _, pageNegative, err := PagePrompt{Styles: styles}.BuildPage(&ports.PagePromptData{
		Panels:     []comic.Panel{panel},
		Characters: chars,
		StyleMode:  "manga_mono",
	})
	require.NoError(t, err)
	require.Contains(t, pageSystem, mono.Suffix)
	require.Contains(t, pageNegative, mono.Negative)
	require.Contains(t, pageNegative, "extra panels", "共通のネガティブプロンプトが落ちています")
}

// 未知のモードはプロンプト構築の時点でエラーにします。
func TestPanelPromptRejectsUnknownStyleMode(t *testing.T) {
	t.Parallel()

	styles, err := NewStyles()
	require.NoError(t, err)

	chars, err := characterkit.NewCharacters([]comic.Character{
		{ID: "zundamon", Name: "ずんだもん", ReferenceURL: "gs://b/z.png", VisualCues: []string{"green hair"}, IsDefault: true},
	})
	require.NoError(t, err)

	_, _, _, err = PanelPrompt{Styles: styles}.BuildPanel(&ports.PanelPromptData{
		Panel:      comic.Panel{ID: "ch01-p01"},
		Characters: chars,
		StyleMode:  "no-such-style",
	})
	require.Error(t, err)
}

func TestSplitFrontMatter(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		content   string
		wantFront string
		wantBody  string
	}{
		"front matter あり": {
			content:   "---\ndirection: \"x\"\n---\nbody line\n",
			wantFront: "direction: \"x\"",
			wantBody:  "body line\n",
		},
		"front matter なし": {
			content:   "body only\n",
			wantFront: "",
			wantBody:  "body only\n",
		},
		"閉じデリミタなしは本文扱い": {
			content:   "---\ndirection: \"x\"\n",
			wantFront: "",
			wantBody:  "---\ndirection: \"x\"\n",
		},
		"本文中の区切り線は残す": {
			content:   "---\ndirection: \"x\"\n---\nbefore\n\n---\n\nafter\n",
			wantFront: "direction: \"x\"",
			wantBody:  "before\n\n---\n\nafter\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			front, body := splitFrontMatter(tc.content)
			require.Equal(t, tc.wantFront, front)
			require.Equal(t, tc.wantBody, body)
		})
	}
}

// 台本モードの選択肢は front matter から説明を引きます。
func TestScriptPromptsModeInfosCarryFrontMatter(t *testing.T) {
	t.Parallel()

	p, err := NewScriptPrompts()
	require.NoError(t, err)

	infos := p.ModeInfos()
	require.NotEmpty(t, infos)

	var def ModeInfo
	for _, info := range infos {
		if info.Name == ModeDefault {
			def = info
		}
	}
	require.Equal(t, ModeDefault, def.Name)
	require.NotEmpty(t, def.Direction, "既定モードの front matter に direction がありません")
	require.True(t, strings.HasPrefix(def.Label(), ModeDefault))
	require.Contains(t, def.Hint(), "向いている用途")
}

// デザインシートはパネル用ではなくシート用の画風指定を使うこと。
// パネル用には cinematic lighting 等の演出が入っており、シートに焼くと
// その照明が参照経由で下流の全生成へ伝染します。
func TestDesignSheetUsesSheetSafeStyle(t *testing.T) {
	t.Parallel()

	styles, err := NewStyles()
	require.NoError(t, err)

	for _, mode := range styles.Modes() {
		style, err := styles.Get(mode)
		require.NoError(t, err)

		_, user, negative, err := (DesignPrompt{Styles: styles}).BuildDesignSheet(&ports.DesignSheetPromptData{
			Descriptions: []string{"ずんだもん (green hair)"},
			StyleMode:    mode,
		})
		require.NoError(t, err, "mode=%s", mode)
		require.Contains(t, user, style.DesignSuffix, "mode=%s: シート用の画風指定が使われていません", mode)
		require.NotContains(t, user, "cinematic lighting", "mode=%s: 演出照明がシートに焼き付いています", mode)
		require.NotContains(t, user, "rim lighting", "mode=%s: 演出照明がシートに焼き付いています", mode)
		require.Contains(t, negative, "color swatches", "mode=%s: 共通のネガティブプロンプトが落ちています", mode)
	}
}

// プリセットのシート用画風には演出語を入れないこと（テンプレート側の番人）。
func TestDesignStylesCarryNoLightingDirection(t *testing.T) {
	t.Parallel()

	styles, err := NewStyles()
	require.NoError(t, err)

	for _, mode := range styles.Modes() {
		style, err := styles.Get(mode)
		require.NoError(t, err)
		for _, word := range []string{"cinematic", "rim light", "dramatic", "lens flare", "color grading"} {
			require.NotContains(t, strings.ToLower(style.DesignSuffix), word,
				"mode=%s: design_style に演出語 %q が入っています", mode, word)
		}
	}
}
