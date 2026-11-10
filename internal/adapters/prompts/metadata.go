package prompts

import (
	"fmt"
	"strings"

	"github.com/shouni/go-prompt-kit/frontmatter"
	"go.yaml.in/yaml/v3"
)

// ModeInfo is a mode's description, written in the front matter at the head of a prompt template.
//
// The mode name (file name) alone does not say "what the mode does", and the person choosing
// ends up opening the template body. This field lets the template itself carry the form's options
// and the description for agents; the key names match the sister projects'
// mode definitions of the same kind.
//
// Templates without front matter are allowed too (giving a ModeInfo with only Name).
// The description is not forced on template authors; if written, it shows on screen.
type ModeInfo struct {
	// Name is the mode name (the file name without its extension). It is not written in the front matter.
	Name string `yaml:"-"`
	// Direction sums up the mode's direction in a phrase (used as the option label).
	Direction string `yaml:"direction"`
	// UseWhen says when to choose it.
	UseWhen string `yaml:"use_when"`
	// Avoids is what the mode avoids, or what it is not suited for.
	Avoids string `yaml:"avoids"`
	// Category is the display group the options are bundled into (optional).
	Category string `yaml:"category"`
}

// Label returns the text shown for the option.
func (m ModeInfo) Label() string {
	if m.Direction == "" {
		return m.Name
	}
	return m.Name + " — " + m.Direction
}

// Hint returns the text used as the option's supplementary description (the title attribute).
func (m ModeInfo) Hint() string {
	var parts []string
	if m.UseWhen != "" {
		parts = append(parts, "向いている用途: "+m.UseWhen)
	}
	if m.Avoids != "" {
		parts = append(parts, "避けること: "+m.Avoids)
	}
	return strings.Join(parts, " / ")
}

// parseModeInfo reads the front matter into a ModeInfo.
// When the front matter is empty it returns a ModeInfo with only the name.
func parseModeInfo(name, front string) (ModeInfo, error) {
	info := ModeInfo{Name: name}
	if err := frontmatter.Decode(front, &info, yaml.Unmarshal); err != nil {
		return ModeInfo{}, fmt.Errorf("failed to parse the front matter (mode: %s): %w", name, err)
	}
	return info, nil
}
