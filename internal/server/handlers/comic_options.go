package handlers

import (
	"net/http"

	"github.com/shouni/ap-story/internal/adapters/prompts"

	"github.com/shouni/go-serve-kit/respond"
)

// comicOptionsResponse is the response of GET /comic-options.
//
// It returns as JSON the same lists shown in the form's <select>. A browser can choose
// by looking at the options, but a JSON API caller (an agent through the MCP server) has
// no way to know what can be specified. The allowlist at enqueue is this list, so
// sending blind gives a 400.
type comicOptionsResponse struct {
	// ScriptModes / StyleModes come with descriptions. A mode name alone is not enough to choose.
	ScriptModes []comicModeOption `json:"script_modes"`
	StyleModes  []comicModeOption `json:"style_modes"`
	// The head of TextModels / ImageModels is the default model.
	TextModels  []string `json:"text_models"`
	ImageModels []string `json:"image_models"`
}

// comicModeOption describes one script mode or style mode.
// Its source is the template's front matter / styles.json as is.
type comicModeOption struct {
	Name      string `json:"name"`
	Direction string `json:"direction,omitempty"`
	UseWhen   string `json:"use_when,omitempty"`
	Avoids    string `json:"avoids,omitempty"`
	Category  string `json:"category,omitempty"`
}

// ComicOptions handles GET /comic-options and returns the modes and models
// a generation job can specify.
func (h *Handler) ComicOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respond.ErrorJSON(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	respond.JSON(w, r, http.StatusOK, comicOptionsResponse{
		ScriptModes: comicModeOptions(h.scriptModes),
		StyleModes:  comicModeOptions(h.styleModes),
		TextModels:  h.geminiModels,
		ImageModels: h.imageModels,
	})
}

func comicModeOptions(modes []prompts.ModeInfo) []comicModeOption {
	options := make([]comicModeOption, 0, len(modes))
	for _, mode := range modes {
		options = append(options, comicModeOption{
			Name:      mode.Name,
			Direction: mode.Direction,
			UseWhen:   mode.UseWhen,
			Avoids:    mode.Avoids,
			Category:  mode.Category,
		})
	}
	return options
}
