package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/shouni/go-serve-kit/respond"
)

// designSheetAPIRequest is the JSON body of POST /jobs (command=generate_design_sheet).
// The web form (POST /jobs (command=generate_design_sheet)) shares the same input fields through the core logic (newDesignSheetTask).
type designSheetAPIRequest struct {
	CharacterIDs         []string `json:"character_ids"`
	AspectRatio          string   `json:"aspect_ratio,omitempty"`
	Layout               string   `json:"layout,omitempty"`
	StyleMode            string   `json:"style_mode,omitempty"`
	ModelOverride        string   `json:"model_override,omitempty"`
	ReferenceURLOverride string   `json:"reference_url_override,omitempty"`
	VisualCuesOverride   []string `json:"visual_cues_override,omitempty"`
	// Seed is optional. When unset and exactly one character is given, it falls back automatically to
	// that character's seed registered in characters.json (see createDesignSheetJSON).
	Seed *int64 `json:"seed,omitempty"`
}

// createDesignSheetJSON enqueues a generate_design_sheet job from a JSON body
// (POST /jobs, command=generate_design_sheet). The server mints a new job_id.
func (h *Handler) createDesignSheetJSON(w http.ResponseWriter, r *http.Request) {
	var req designSheetAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}

	params := designSheetTaskParams(req)
	params.Seed = h.resolveDesignSheetSeed(params.CharacterIDs, params.Seed)

	task, err := h.newDesignSheetTask(params)
	if err != nil {
		respond.ServerErrorJSON(w, r, http.StatusInternalServerError, err, "failed to generate job id")
		return
	}

	if err := task.ValidateSubmission(); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// Model names and art styles are restricted by the allowlist. A browser is restricted by the <select>,
	// but this JSON path can send any string, so this is the only gate.
	if err := h.validateChoices(task); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, err.Error())
		return
	}

	h.enqueueAndRespond(w, r, task)
}
