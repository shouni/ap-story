package handlers

import (
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/shouni/ap-story/internal/domain"
)

// composeFormData is the data passed to the compose.html template.
// On error the form is shown again with the input kept.
type composeFormData struct {
	SourceURL  string
	SourceText string
	ScriptMode string
	StyleMode  string
	TextModel  string
	// ScriptModes / StyleModes are the options for the script mode and style mode
	// (templates under assets/prompts).
	ScriptModes []selectOption
	StyleModes  []selectOption
	// TextModels / ImageModels are the model options (the head is the default).
	// When empty, the picker itself is not shown.
	TextModels  []selectOption
	ImageModels []selectOption
	// Langs / AspectRatios are the options for the page language and the image ratio.
	Langs        []selectOption
	AspectRatios []selectOption
	// Characters are the cast checkboxes. None ticked means the script mode's own cast.
	Characters   []selectOption
	ErrorMessage string
}

// buildComposeFormData builds the form display data reflecting the input.
// The first display (a zero-value req) and the redisplay on error go through the same assembly.
func (h *Handler) buildComposeFormData(req composeComicRequest) composeFormData {
	return composeFormData{
		SourceURL:   req.SourceURL,
		SourceText:  req.SourceText,
		ScriptMode:  req.ScriptMode,
		StyleMode:   req.StyleMode,
		TextModel:   req.TextModel,
		ScriptModes: modeOptions(h.scriptModes, req.ScriptMode),
		StyleModes:  modeOptions(h.styleModes, req.StyleMode),
		TextModels:  modelOptions(h.geminiModels, req.TextModel),
		Langs: []selectOption{
			{Value: domain.LangJapanese, Label: "日本語（縦書き・右から左）", Selected: req.Lang != domain.LangEnglish},
			{Value: domain.LangEnglish, Label: "English（横書き・左から右）", Selected: req.Lang == domain.LangEnglish},
		},
		AspectRatios: aspectRatioOptions(req.AspectRatio),
		Characters:   h.castOptions(req.CharacterIDs),
	}
}

// aspectRatioOptions lists the ratios a comic can be drawn at. Nothing chosen selects the head,
// and there is no empty option: like the models, the form always sends a concrete value, so the
// work records the ratio it was drawn at instead of whatever the deployment's default was that day.
func aspectRatioOptions(selected string) []selectOption {
	labels := map[string]string{
		"3:4":  "3:4（漫画の1ページ、X の画像）",
		"9:16": "9:16（縦長、ショート動画の素材）",
		"1:1":  "1:1（正方形）",
	}
	ratios := domain.ComicAspectRatios()
	if selected == "" {
		selected = ratios[0]
	}
	options := make([]selectOption, 0, len(ratios))
	for _, ratio := range ratios {
		options = append(options, selectOption{Value: ratio, Label: labels[ratio], Selected: ratio == selected})
	}
	return options
}

// castOptions lists every registered character as a checkbox.
func (h *Handler) castOptions(selected []string) []selectOption {
	all := h.characters.All()
	options := make([]selectOption, 0, len(all))
	for _, char := range all {
		options = append(options, selectOption{
			Value:    char.ID,
			Label:    char.Name,
			Selected: slices.Contains(selected, char.ID),
		})
	}
	return options
}

// ComposeForm handles GET /compose and shows the comic generation form.
func (h *Handler) ComposeForm(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "compose.html", "漫画を生成", h.buildComposeFormData(composeComicRequest{}))
}

// createComicForm handles POST /jobs and enqueues a compose_comic job from the form input.
// Building and validating the task is shared with the JSON API (createComicJSON); only the response
// is HTML (the accepted page, or the form shown again with an error).
func (h *Handler) createComicForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	req := composeComicRequest{
		SourceURL:    strings.TrimSpace(r.PostFormValue("source_url")),
		SourceText:   strings.TrimSpace(r.PostFormValue("source_text")),
		ScriptMode:   strings.TrimSpace(r.PostFormValue("script_mode")),
		StyleMode:    strings.TrimSpace(r.PostFormValue("style_mode")),
		TextModel:    strings.TrimSpace(r.PostFormValue("text_model")),
		Lang:         strings.TrimSpace(r.PostFormValue("lang")),
		AspectRatio:  strings.TrimSpace(r.PostFormValue("aspect_ratio")),
		CharacterIDs: trimmedIDs(r.PostForm["character_ids"]),
		// This form only makes the script. When it is pressed, the outline has not run yet and
		// nobody knows how many panels there will be. Do not have someone approve a payment of unknown size.
		// Images start from the work's detail page, where the panel count is visible (the JSON API can run to the end).
		StopAfterScript: true,
	}
	formData := h.buildComposeFormData(req)

	task, err := h.newComposeTask(req)
	if err != nil {
		slog.Error("failed to generate job id", "error", err)
		formData.ErrorMessage = "ジョブIDの採番に失敗しました。時間をおいて再度お試しください。"
		h.render(w, r, http.StatusInternalServerError, "compose.html", "漫画を生成", formData)
		return
	}

	if err := task.ValidateSubmission(); err != nil {
		formData.ErrorMessage = err.Error()
		h.render(w, r, http.StatusBadRequest, "compose.html", "漫画を生成", formData)
		return
	}

	if err := h.validateChoices(task); err != nil {
		formData.ErrorMessage = err.Error()
		h.render(w, r, http.StatusBadRequest, "compose.html", "漫画を生成", formData)
		return
	}

	// Write the status first (see recordQueuedStatus).
	h.recordQueuedStatus(r, task)
	if err := h.taskQueue.Enqueue(r.Context(), task); err != nil {
		slog.Error("failed to enqueue task", "job_id", task.JobID, "command", task.Command, "error", err)
		formData.ErrorMessage = "ジョブの投入に失敗しました。時間をおいて再度お試しください。"
		h.render(w, r, http.StatusInternalServerError, "compose.html", "漫画を生成", formData)
		return
	}

	h.render(w, r, http.StatusAccepted, "accepted.html", "受付完了", newComposeAcceptedData(task.JobID))
}
