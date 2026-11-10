// Package assets provides the embedded resources edited on a different cycle from the code
// (HTML templates, CSS/JavaScript, prompt templates).
// The layout is the same as in the sister projects.
//
// This package is only a place to keep things. Loading, parsing and assembling are the users'
// job (internal/server/handlers, internal/adapters/prompts). Adding processing here would
// lose its single role: being where the embedded resources live.
package assets

import "embed"

const (
	// OutlinePromptDir is the embedded path of the outline-generation prompts.
	OutlinePromptDir = "prompts/outline"
	// ChapterPromptDir is the embedded path of the chapter-script generation prompts.
	ChapterPromptDir = "prompts/chapter"
	// StylesJSONPath is the embedded path of the art-style presets (style modes).
	// Unlike the script prompts, each entry is structured data determined by a pair of the art-style
	// instruction and the negative prompt, so it is kept as JSON rather than a template.
	StylesJSONPath = "prompts/styles.json"
)

var (
	// Templates holds the full set of HTML templates that make up the screens.
	//go:embed templates/*.html
	Templates embed.FS

	// StaticFiles holds the static files served to the browser, such as CSS/JavaScript.
	//go:embed static
	StaticFiles embed.FS

	// Prompts holds the script-generation prompt templates and the art-style presets.
	// For the templates, the directory name is the generation stage and the file name (without extension) is the mode name.
	//go:embed prompts/outline/*.md prompts/chapter/*.md prompts/styles.json
	Prompts embed.FS
)
