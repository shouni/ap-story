package handlers

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/shouni/ap-story/internal/domain"
)

// DefaultDesignSheetAspectRatio is the aspect ratio used when a design sheet is requested without one.
//
// The default layout is a three-view turnaround, so landscape gets more resolution per figure.
// Make sheets meant as reference anchors for panels and pages at 3:4 (+ single pose).
//
// The kit also has a fallback for an unspecified ratio (ports.Config.AspectRatio), but it is
// filled here rather than left to that, so the form and the JSON API do not answer differently.
const DefaultDesignSheetAspectRatio = "16:9"

// designSheetCharacterOption is one character's option passed to design_sheet_form.html.
type designSheetCharacterOption struct {
	ID      string
	Name    string
	Checked bool
}

// designSheetFormData is the data passed to the design_sheet_form.html template.
// On error the form is shown again with the input (the chosen characters and so on) kept.
type designSheetFormData struct {
	Characters []designSheetCharacterOption
	Models     []selectOption
	// StyleModes are the art-style options. Sheets are generated with the sheet art style
	// (styles.json's design_style), not the panel one.
	StyleModes []selectOption
	StyleMode  string
	// ModelOverride is the chosen model name (empty means the default model).
	ModelOverride string
	AspectRatio   string
	Layout        string
	// ReferenceURLOverride / VisualCuesOverride are one-off overrides of the reference image and
	// visual features (they correspond to go-comic-kit's DesignOverride). They apply only when a single
	// character is chosen and are ignored when several are chosen (a composed design sheet).
	ReferenceURLOverride string
	VisualCuesOverride   string
	ErrorMessage         string
}

// buildDesignSheetFormData builds the form display data reflecting the set of chosen character IDs
// and the model options.
func (h *Handler) buildDesignSheetFormData(selected []string) designSheetFormData {
	checked := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		checked[id] = struct{}{}
	}

	data := designSheetFormData{}
	if h.characters != nil {
		for _, c := range h.characters.All() {
			_, isChecked := checked[c.ID]
			data.Characters = append(data.Characters, designSheetCharacterOption{
				ID: c.ID, Name: c.Name, Checked: isChecked,
			})
		}
	}
	data.Models = modelOptions(h.imageModels, "")
	data.StyleModes = modeOptions(h.styleModes, "")
	return data
}

// splitVisualCues splits newline- or comma-separated input into a list of visual features.
// Empty lines and surrounding whitespace are removed.
func splitVisualCues(raw string) []string {
	if raw == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == ','
	})
	cues := make([]string, 0, len(fields))
	for _, f := range fields {
		if trimmed := strings.TrimSpace(f); trimmed != "" {
			cues = append(cues, trimmed)
		}
	}
	return cues
}

// ComposeDesignSheetForm handles GET /compose/design-sheet and shows the design-sheet form for
// a single character. It is a one-off generation not tied to an existing work (jobID).
// The query parameter ?character_id=xxx preselects a character
// (used when coming from the "New" button on /characters/{characterID}).
func (h *Handler) ComposeDesignSheetForm(w http.ResponseWriter, r *http.Request) {
	preselected := r.URL.Query()["character_id"]
	h.render(w, r, http.StatusOK, "design_sheet_form.html", "デザインシートを生成", h.buildDesignSheetFormData(preselected))
}

// designSheetTaskParams is the input for building a generate_design_sheet job.
// It is shared by the web form (POST /jobs (command=generate_design_sheet)) and the JSON API (POST /jobs (command=generate_design_sheet)).
type designSheetTaskParams struct {
	CharacterIDs         []string
	AspectRatio          string
	Layout               string
	StyleMode            string
	ModelOverride        string
	ReferenceURLOverride string
	VisualCuesOverride   []string
	Seed                 *int64
}

// newDesignSheetTask mints a job ID from the generate_design_sheet input and builds the Task.
// The user never deals with job_id; the server mints a new one (it is a one-off generation with no
// state, so no link to an existing work is needed).
func (h *Handler) newDesignSheetTask(p designSheetTaskParams) (domain.Task, error) {
	jobID, err := domain.NewJobID()
	if err != nil {
		return domain.Task{}, err
	}

	aspectRatio := strings.TrimSpace(p.AspectRatio)
	if aspectRatio == "" {
		aspectRatio = DefaultDesignSheetAspectRatio
	}

	return domain.Task{
		Command:              domain.TaskCommandGenerateDesignSheet,
		JobID:                jobID,
		CreatedAt:            time.Now().UTC(),
		CharacterIDs:         p.CharacterIDs,
		AspectRatio:          aspectRatio,
		Layout:               p.Layout,
		StyleMode:            p.StyleMode,
		ModelOverride:        firstIfEmpty(p.ModelOverride, h.imageModels),
		ReferenceURLOverride: p.ReferenceURLOverride,
		VisualCuesOverride:   p.VisualCuesOverride,
		Seed:                 p.Seed,
	}, nil
}

// resolveDesignSheetSeed prefers an explicit seed; when none is given and exactly one character
// is chosen, it falls back to the seed registered in characters.json. With several characters
// (a composed design sheet) it does not fall back, since there is no single answer for whose seed
// to use.
func (h *Handler) resolveDesignSheetSeed(characterIDs []string, explicit *int64) *int64 {
	if explicit != nil {
		return explicit
	}
	if len(characterIDs) != 1 || h.characters == nil {
		return nil
	}
	char := h.characters.GetCharacter(characterIDs[0])
	if char == nil {
		return nil
	}
	return char.Seed
}

// createDesignSheetForm handles POST /jobs (command=generate_design_sheet) and enqueues a
// generate_design_sheet job from the form input.
func (h *Handler) createDesignSheetForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	characterIDs := r.PostForm["character_ids"]
	aspectRatio := strings.TrimSpace(r.PostFormValue("aspect_ratio"))
	layout := strings.TrimSpace(r.PostFormValue("layout"))
	styleMode := strings.TrimSpace(r.PostFormValue("style_mode"))
	modelOverride := strings.TrimSpace(r.PostFormValue("model_override"))
	referenceURLOverride := strings.TrimSpace(r.PostFormValue("reference_url_override"))
	visualCuesOverrideRaw := strings.TrimSpace(r.PostFormValue("visual_cues_override"))
	formData := h.buildDesignSheetFormData(characterIDs)
	formData.AspectRatio = aspectRatio
	formData.Layout = layout
	formData.StyleMode = styleMode
	formData.StyleModes = modeOptions(h.styleModes, styleMode)
	formData.ModelOverride = modelOverride
	formData.ReferenceURLOverride = referenceURLOverride
	formData.VisualCuesOverride = visualCuesOverrideRaw

	task, err := h.newDesignSheetTask(designSheetTaskParams{
		CharacterIDs:         characterIDs,
		AspectRatio:          aspectRatio,
		Layout:               layout,
		StyleMode:            styleMode,
		ModelOverride:        modelOverride,
		ReferenceURLOverride: referenceURLOverride,
		VisualCuesOverride:   splitVisualCues(visualCuesOverrideRaw),
		Seed:                 h.resolveDesignSheetSeed(characterIDs, nil),
	})
	if err != nil {
		slog.Error("failed to generate job id", "error", err)
		formData.ErrorMessage = "ジョブIDの採番に失敗しました。時間をおいて再度お試しください。"
		h.render(w, r, http.StatusInternalServerError, "design_sheet_form.html", "デザインシートを生成", formData)
		return
	}

	if err := task.ValidateSubmission(); err != nil {
		formData.ErrorMessage = err.Error()
		h.render(w, r, http.StatusBadRequest, "design_sheet_form.html", "デザインシートを生成", formData)
		return
	}

	if err := h.validateChoices(task); err != nil {
		formData.ErrorMessage = err.Error()
		h.render(w, r, http.StatusBadRequest, "design_sheet_form.html", "デザインシートを生成", formData)
		return
	}

	// Write the status first (see recordQueuedStatus).
	h.recordQueuedStatus(r, task)
	if err := h.taskQueue.Enqueue(r.Context(), task); err != nil {
		slog.Error("failed to enqueue task", "job_id", task.JobID, "command", task.Command, "error", err)
		formData.ErrorMessage = "ジョブの投入に失敗しました。時間をおいて再度お試しください。"
		h.render(w, r, http.StatusInternalServerError, "design_sheet_form.html", "デザインシートを生成", formData)
		return
	}

	h.render(w, r, http.StatusAccepted, "accepted.html", "受付完了", newDesignSheetAcceptedData(task.JobID, characterIDs))
}
