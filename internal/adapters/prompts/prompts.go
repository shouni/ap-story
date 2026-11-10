// Package prompts implements ap-story's own Outline/ChapterScript/DesignSheet prompts as
// go-comic-kit's ports.OutlinePrompt / ports.ChapterScriptPrompt / ports.DesignSheetPrompt.
// The casting of Zundamon, Metan and Tsumugi and the world (SF-mecha-action-style tech-learning comics)
// follow the structure of the sister project's dialogue prompts.
package prompts

import (
	"fmt"
	"slices"

	"github.com/shouni/go-comic-kit/ports"
	promptkit "github.com/shouni/go-prompt-kit/prompts"

	"github.com/shouni/ap-story/assets"
)

// ModeDefault is the name of the default prompt mode.
const ModeDefault = "default"

// ScriptPrompts are ap-story's own Outline/ChapterScript prompts.
type ScriptPrompts struct {
	outline *promptkit.Builder
	chapter *promptkit.Builder
	// infos is the front matter of the outline templates. A script mode's description is written
	// on the outline side (so that one mode's description is not split across two files).
	infos map[string]ModeInfo
}

var (
	_ ports.OutlinePrompt       = (*ScriptPrompts)(nil)
	_ ports.ChapterScriptPrompt = (*ScriptPrompts)(nil)
)

// NewScriptPrompts loads the embedded templates and builds ScriptPrompts.
//
// A script mode looks up the template of that mode name for both the outline and the chapter script,
// so a mode present on only one side is rejected at construction. This moves the failure of
// "no template" from the middle of generating a chapter, after the mode was offered as an option, forward to startup.
func NewScriptPrompts() (*ScriptPrompts, error) {
	outline, infos, err := loadPrompts(assets.OutlinePromptDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load the outline templates: %w", err)
	}
	chapter, _, err := loadPrompts(assets.ChapterPromptDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load the chapter-script templates: %w", err)
	}
	outlineModes, chapterModes := sortedModes(outline), sortedModes(chapter)
	if !slices.Equal(outlineModes, chapterModes) {
		return nil, fmt.Errorf("script modes differ between outline and chapter script: outline=%v chapter=%v", outlineModes, chapterModes)
	}
	return &ScriptPrompts{outline: outline, chapter: chapter, infos: infos}, nil
}

// Modes returns the selectable script mode names in name order. It is used for form validation.
func (p *ScriptPrompts) Modes() []string {
	return sortedModes(p.outline)
}

// ModeInfos returns the descriptions of the selectable script modes in name order. They are used for the form's options.
func (p *ScriptPrompts) ModeInfos() []ModeInfo {
	return sortedInfos(p.outline, p.infos)
}

// BuildOutline builds the outline-generation prompt.
func (p *ScriptPrompts) BuildOutline(mode string, data *ports.OutlinePromptData) (string, error) {
	return execute(p.outline, mode, data)
}

// BuildChapterScript builds the per-chapter script-generation prompt.
func (p *ScriptPrompts) BuildChapterScript(mode string, data *ports.ChapterPromptData) (string, error) {
	return execute(p.chapter, mode, data)
}

// loadPrompts loads the .md files under the embedded directory by mode name (the file name without
// its extension), splits the front matter off the body and returns the templates and mode descriptions.
//
// WithFrontMatter is used because the front matter must be cut off before templating so that
// it does not leak into the prompt body. What is cut off can be read from
// Builder.FrontMatter, so loading happens only once.
func loadPrompts(dir string) (*promptkit.Builder, map[string]ModeInfo, error) {
	builder, err := promptkit.LoadFS(assets.Prompts, dir,
		promptkit.WithExtensions(".md"),
		promptkit.WithFrontMatter(),
	)
	if err != nil {
		return nil, nil, err
	}

	modes := builder.Modes()
	infos := make(map[string]ModeInfo, len(modes))
	for _, mode := range modes {
		info, err := parseModeInfo(mode, builder.FrontMatter(mode))
		if err != nil {
			return nil, nil, err
		}
		infos[mode] = info
	}

	return builder, infos, nil
}

// sortedModes returns the mode names of the loaded templates in name order.
// go-prompt-kit's Modes() is in registration order, so it is fixed here to keep the on-screen order
// independent of the embedded files' traversal order.
func sortedModes(builder *promptkit.Builder) []string {
	modes := builder.Modes()
	slices.Sort(modes)
	return modes
}

// sortedInfos returns the mode descriptions of the loaded templates in name order.
func sortedInfos(builder *promptkit.Builder, infos map[string]ModeInfo) []ModeInfo {
	modes := sortedModes(builder)
	out := make([]ModeInfo, 0, len(modes))
	for _, mode := range modes {
		info, ok := infos[mode]
		if !ok {
			info = ModeInfo{Name: mode}
		}
		out = append(out, info)
	}
	return out
}

// execute builds the prompt after mapping an unspecified mode to the default.
// An unknown mode is an error rather than falling back to the default. This keeps a mistyped
// mode name from silently passing as the default prompt; go-prompt-kit's WithDefaultMode is
// deliberately not used.
func execute(builder *promptkit.Builder, mode string, data any) (string, error) {
	if mode == "" {
		mode = ModeDefault
	}
	out, err := builder.Build(mode, data)
	if err != nil {
		return "", fmt.Errorf("failed to build the prompt (mode: %s): %w", mode, err)
	}
	return out, nil
}
