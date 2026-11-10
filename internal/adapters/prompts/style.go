package prompts

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shouni/ap-story/assets"
)

// Style is one art-style preset, held in assets/prompts/styles.json.
//
// It is JSON rather than a template like the script prompts because each entry is a pair of
// "the art-style instruction (Suffix)" and "what to avoid in that style (Negative)".
// Putting two equal bodies into a template's body and header invites fixing only one of them.
type Style struct {
	// Name is the style mode name (the form's selected value, the value recorded in the state).
	Name string `json:"name"`
	// Direction / UseWhen / Avoids / Category describe it to the person choosing.
	// The key names match the sister projects' mode definitions of the same kind.
	Direction string `json:"direction"`
	UseWhen   string `json:"use_when"`
	Avoids    string `json:"avoids"`
	Category  string `json:"category"`
	// Suffix is the art-style instruction added to image-generation prompts.
	Suffix string `json:"style"`
	// DesignSuffix is the art-style instruction for design sheets, kept separate from Suffix.
	//
	// A sheet is the identity anchor for other outputs, so baking in a Suffix with staging (cinematic lighting,
	// rim light, film grain and so on) spreads that lighting through the reference to every downstream
	// generation. Write the same look with only the staging removed.
	DesignSuffix string `json:"design_style"`
	// Negative is what to avoid in that art style. It is added to the shared negative prompt
	// (the suppression of composition, text and breakage held by the prompt implementations).
	//
	// It is per style because writing an art-style word such as "monochrome" on the shared side would make
	// the instructions clash head-on the moment a style allowing monochrome is added.
	Negative string `json:"negative"`
}

// Info converts to the description for the selection UI (the same shape as the script modes).
func (s Style) Info() ModeInfo {
	return ModeInfo{
		Name:      s.Name,
		Direction: s.Direction,
		UseWhen:   s.UseWhen,
		Avoids:    s.Avoids,
		Category:  s.Category,
	}
}

// Styles is the list of art-style presets. It keeps the order written in the JSON
// (so the author can decide the order of the options).
type Styles struct {
	list  []Style
	byKey map[string]Style
}

// NewStyles loads the embedded styles.json and builds Styles.
// A missing default mode, duplicate names and empty required fields are rejected here (to fail at startup).
func NewStyles() (*Styles, error) {
	data, err := assets.Prompts.ReadFile(assets.StylesJSONPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load the art-style presets: %w", err)
	}

	var list []Style
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("failed to parse the art-style presets: %w", err)
	}

	byKey := make(map[string]Style, len(list))
	for _, style := range list {
		switch {
		case strings.TrimSpace(style.Name) == "":
			return nil, fmt.Errorf("an art-style preset has an entry with no name")
		case strings.TrimSpace(style.Suffix) == "":
			return nil, fmt.Errorf("art-style preset %q has no style", style.Name)
		case strings.TrimSpace(style.DesignSuffix) == "":
			return nil, fmt.Errorf("art-style preset %q has no design_style", style.Name)
		}
		if _, dup := byKey[style.Name]; dup {
			return nil, fmt.Errorf("art-style preset %q is duplicated", style.Name)
		}
		byKey[style.Name] = style
	}
	if _, ok := byKey[ModeDefault]; !ok {
		return nil, fmt.Errorf("the art-style presets have no default mode %q", ModeDefault)
	}

	return &Styles{list: list, byKey: byKey}, nil
}

// Modes returns the selectable style mode names in the order written. It is used for form validation.
func (s *Styles) Modes() []string {
	names := make([]string, 0, len(s.list))
	for _, style := range s.list {
		names = append(names, style.Name)
	}
	return names
}

// ModeInfos returns the descriptions of the selectable style modes in the order written. They are used for the form's options.
func (s *Styles) ModeInfos() []ModeInfo {
	infos := make([]ModeInfo, 0, len(s.list))
	for _, style := range s.list {
		infos = append(infos, style.Info())
	}
	return infos
}

// resolveStyle resolves the art-style mode passed from the kit into the art-style instruction and the negative prompt.
// Panels, pages and design sheets make the same decision, so it lives in one place.
// suffixOf switches which of the preset's art-style instructions is used.
//
// styles is required. The art-style wording exists only in the presets, so without it generation would
// run with no art style. Rather than silently dropping the art style, prompt construction fails.
func resolveStyle(styles *Styles, mode, baseNegative string, suffixOf func(Style) string) (suffix, negative string, err error) {
	if styles == nil {
		return "", "", fmt.Errorf("no art-style presets are configured")
	}
	style, err := styles.Get(mode)
	if err != nil {
		return "", "", err
	}
	if style.Negative == "" {
		return suffixOf(style), baseNegative, nil
	}
	return suffixOf(style), baseNegative + ", " + style.Negative, nil
}

// imageSuffix / designSuffix choose which art-style instruction is passed to resolveStyle.
func imageSuffix(s Style) string  { return s.Suffix }
func designSuffix(s Style) string { return s.DesignSuffix }

// Get returns the preset for a style mode name.
// The empty string means the default mode. An unknown mode is an error, as for script modes
// (silently falling back to the default would hide that generation did not use the intended style).
func (s *Styles) Get(mode string) (Style, error) {
	if mode == "" {
		mode = ModeDefault
	}
	style, ok := s.byKey[mode]
	if !ok {
		return Style{}, fmt.Errorf("unknown style mode: %s", mode)
	}
	return style, nil
}
