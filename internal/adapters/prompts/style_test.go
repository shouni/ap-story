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
		require.NotEmpty(t, style.Direction, "mode=%s: the option has no description", mode)
	}
}

// The empty string means the default mode, so that an unselected form and the worker's resolution agree.
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

// An unknown mode is an error rather than falling back to the default (preventing generation in an art style other than the one intended).
func TestStylesUnknownModeFails(t *testing.T) {
	t.Parallel()

	s, err := NewStyles()
	require.NoError(t, err)

	_, err = s.Get("no-such-style")
	require.Error(t, err)
}

// The art style and the negative prompt fit together. If art-style words remained on the shared side,
// a monochrome preset would clash head-on with its own instruction.
func TestStylePresetsDoNotContradictSharedNegativePrompts(t *testing.T) {
	t.Parallel()

	s, err := NewStyles()
	require.NoError(t, err)

	for _, shared := range []string{panelNegativePrompt, pageNegativePrompt} {
		for _, word := range []string{"monochrome", "greyscale", "screentone", "color"} {
			require.NotContains(t, shared, word,
				"the shared negative prompt still has the art-style word %q (keep it in the preset)", word)
		}
	}

	mono, err := s.Get("manga_mono")
	require.NoError(t, err)
	require.Contains(t, mono.Suffix, "monochrome")
	require.NotContains(t, mono.Negative, "monochrome")
}

// The panel and page prompts reflect the chosen art style's instruction and negative.
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
	require.Contains(t, panelNegative, "bad anatomy", "the shared negative prompt was dropped")

	pageSystem, _, pageNegative, err := PagePrompt{Styles: styles}.BuildPage(&ports.PagePromptData{
		Panels:     []comic.Panel{panel},
		Characters: chars,
		StyleMode:  "manga_mono",
	})
	require.NoError(t, err)
	require.Contains(t, pageSystem, mono.Suffix)
	require.Contains(t, pageNegative, mono.Negative)
	require.Contains(t, pageNegative, "extra panels", "the shared negative prompt was dropped")
}

// An unknown mode is an error at prompt construction.
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

// The script mode options take their descriptions from the front matter.
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
	require.NotEmpty(t, def.Direction, "the default mode's front matter has no direction")
	require.True(t, strings.HasPrefix(def.Label(), ModeDefault))
	require.Contains(t, def.Hint(), "向いている用途")
}

// Design sheets use the sheet art style, not the panel one.
// The panel one includes staging such as cinematic lighting, and baked into a sheet that lighting
// spreads through the reference to every downstream generation.
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
		require.Contains(t, user, style.DesignSuffix, "mode=%s: the sheet art style is not used", mode)
		require.NotContains(t, user, "cinematic lighting", "mode=%s: staging lighting is baked into the sheet", mode)
		require.NotContains(t, user, "rim lighting", "mode=%s: staging lighting is baked into the sheet", mode)
		require.Contains(t, negative, "color swatches", "mode=%s: the shared negative prompt was dropped", mode)
	}
}

// The preset's sheet art style must contain no staging words (a guard on the template side).
func TestDesignStylesCarryNoLightingDirection(t *testing.T) {
	t.Parallel()

	styles, err := NewStyles()
	require.NoError(t, err)

	for _, mode := range styles.Modes() {
		style, err := styles.Get(mode)
		require.NoError(t, err)
		for _, word := range []string{"cinematic", "rim light", "dramatic", "lens flare", "color grading"} {
			require.NotContains(t, strings.ToLower(style.DesignSuffix), word,
				"mode=%s: design_style contains the staging word %q", mode, word)
		}
	}
}
