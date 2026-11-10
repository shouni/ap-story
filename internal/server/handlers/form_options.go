package handlers

import (
	"fmt"
	"slices"
	"strings"

	"github.com/shouni/ap-story/internal/adapters/prompts"
)

// selectOption is one item of a form's <select>.
// It is shared by the model pickers (GEMINI_MODELS / IMAGE_MODELS) and the prompt mode pickers
// (script mode and style mode).
type selectOption struct {
	Value string
	Label string
	// Hint is the option's supplementary description (the title attribute). When empty, the attribute is omitted.
	Hint     string
	Selected bool
}

// modelOptions converts a list of model names into options. The head is the default model.
// There is no per-purpose split, so the list becomes the options as is.
//
// There is no empty "use the default model" option. The browser is always made to send a concrete name,
// so that the state's record reliably shows which model made the work
// (empty would still run, falling to the worker's configuration, but it would leave no record to trace later).
// Nothing chosen is treated as choosing the head = the default model, the same approach as mode selection.
func modelOptions(models []string, selected string) []selectOption {
	if selected == "" && len(models) > 0 {
		selected = models[0]
	}
	options := make([]selectOption, 0, len(models))
	for i, model := range models {
		label := model
		if i == 0 {
			label = "既定: " + model
		}
		options = append(options, selectOption{
			Value:    model,
			Label:    label,
			Selected: model == selected,
		})
	}
	return options
}

// modeOptions converts a list of prompt modes into options. The label and supplementary description
// come from the template's front matter (prompts.ModeInfo).
//
// Nothing chosen (the empty string) is treated as choosing the default mode. The worker also maps the
// empty string to the default mode, so the screen and the actual generation do not disagree on the choice.
func modeOptions(modes []prompts.ModeInfo, selected string) []selectOption {
	if selected == "" {
		selected = prompts.ModeDefault
	}
	options := make([]selectOption, 0, len(modes))
	for _, mode := range modes {
		label := mode.Label()
		if mode.Name == prompts.ModeDefault {
			label += "（既定）"
		}
		options = append(options, selectOption{
			Value:    mode.Name,
			Label:    label,
			Hint:     mode.Hint(),
			Selected: mode.Name == selected,
		})
	}
	return options
}

// modeNames extracts just the names from a list of modes. It is used to validate the chosen value.
func modeNames(modes []prompts.ModeInfo) []string {
	names := make([]string, 0, len(modes))
	for _, mode := range modes {
		names = append(names, mode.Name)
	}
	return names
}

// firstIfEmpty returns the head of the list (= the default) when the value is empty.
// The list is empty only when a setting is missing, and that is rejected by the startup validation.
func firstIfEmpty(value string, list []string) string {
	if strings.TrimSpace(value) != "" || len(list) == 0 {
		return value
	}
	return list[0]
}

// validateAllowed checks that the given value is in the allowlist.
// The empty string means "use the default" and is always valid.
//
// It lives here rather than in domain.Task's validation because the allowlist comes from env and the
// templates, which the domain layer does not know. A browser is restricted to the <select> options, but
// the JSON API can send any string.
func validateAllowed(kind, value string, allowed []string) error {
	value = strings.TrimSpace(value)
	if value == "" || slices.Contains(allowed, value) {
		return nil
	}
	return fmt.Errorf("不正な%sです: %s", kind, value)
}
