package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shouni/ap-story/internal/domain"

	"github.com/shouni/go-serve-kit/respond"
)

// composeComicRequest is the JSON body of POST /jobs (command=compose_comic).
// The form shares the same input fields.
type composeComicRequest struct {
	SourceURL  string `json:"source_url"`
	SourceText string `json:"source_text"`
	ScriptMode string `json:"script_mode"`
	StyleMode  string `json:"style_mode"`
	// TextModel / ImageModel are the models used for script generation and image generation.
	// When omitted, they are filled with the default models (the heads of GEMINI_MODELS / IMAGE_MODELS).
	TextModel  string `json:"text_model"`
	ImageModel string `json:"image_model"`
	// With StopAfterScript it stops after the script. It is for checking the content before
	// moving on to image generation; the rest is done with render_comic.
	StopAfterScript bool `json:"stop_after_script"`
}

// enqueueResponse is the shared response of the enqueue endpoints.
type enqueueResponse struct {
	Status string `json:"status"`
	JobID  string `json:"job_id"`
}

// newComposeTask mints a job ID from the compose_comic input and builds the Task.
// It is the core logic shared by the JSON API and the web form; only the response format differs per caller.
//
// An unspecified model is filled with the default model (the head of the list). It is not left to the
// worker's configuration for the same reason as the aspect ratio (see DefaultDesignSheetAspectRatio). Filled in,
// the state always records which model made the work, and a chapter remake or later image generation can
// inherit the same model. Passed through empty, that work alone would lose its provenance.
func (h *Handler) newComposeTask(req composeComicRequest) (domain.Task, error) {
	jobID, err := domain.NewJobID()
	if err != nil {
		return domain.Task{}, err
	}

	task := domain.Task{
		Command:         domain.TaskCommandComposeComic,
		JobID:           jobID,
		CreatedAt:       time.Now().UTC(),
		SourceURL:       req.SourceURL,
		SourceText:      req.SourceText,
		ScriptMode:      req.ScriptMode,
		StyleMode:       req.StyleMode,
		TextModel:       firstIfEmpty(req.TextModel, h.geminiModels),
		ModelOverride:   firstIfEmpty(req.ImageModel, h.imageModels),
		StopAfterScript: req.StopAfterScript,
	}
	return task, nil
}

// validateChoices checks that the model and prompt-mode choices on the Task fall within the
// allowlist. A browser is restricted to the <select> options, but the JSON API can send
// any string, so they are rejected here before enqueue.
//
// Each command uses different fields, so an empty field passes through as "not specified".
// Every enqueue path (compose / design-sheet / regenerate) goes through this one. Splitting it would
// leave one path unvalidated the next time a command is added (which is exactly what had happened).
//
// Whether a mode really exists (its template) and empty model names are also validated on the worker, but
// that fails only after a Cloud Tasks round trip, so the answer is returned here before sending.
func (h *Handler) validateChoices(task domain.Task) error {
	for _, choice := range []struct {
		kind    string
		value   string
		allowed []string
	}{
		{"台本モード", task.ScriptMode, modeNames(h.scriptModes)},
		{"スタイルモード", task.StyleMode, modeNames(h.styleModes)},
		{"テキストモデル", task.TextModel, h.geminiModels},
		{"画像モデル", task.ModelOverride, h.imageModels},
	} {
		if err := validateAllowed(choice.kind, choice.value, choice.allowed); err != nil {
			return err
		}
	}
	return nil
}

// JobCreate enqueues a new job (POST /jobs).
//
// There is one entry point, split by the shape of the body and the command. JSON is for machines (the MCP
// server) and the form is for the screen; for both, command compose_comic (the default) makes a comic job
// and generate_design_sheet a design-sheet job. Only reading and the response shape differ; minting, validation and enqueueing share one path.
func (h *Handler) JobCreate(w http.ResponseWriter, r *http.Request) {
	if isJSONBody(r) {
		h.createJobJSON(w, r)
		return
	}
	h.createJobForm(w, r)
}

// createJobJSON looks at the JSON body's command and dispatches to a comic or a design sheet.
//
// The body can be read only once, so it peeks at command and then has the same bytes read again.
func (h *Handler) createJobJSON(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCreateJobBody))
	if err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var head struct {
		Command domain.TaskCommand `json:"command"`
	}
	if err := json.Unmarshal(body, &head); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	switch head.Command {
	case "", domain.TaskCommandComposeComic:
		h.createComicJSON(w, r)
	case domain.TaskCommandGenerateDesignSheet:
		h.createDesignSheetJSON(w, r)
	default:
		respond.ErrorJSON(w, r, http.StatusBadRequest, fmt.Sprintf("command は %s / %s のいずれかです",
			domain.TaskCommandComposeComic, domain.TaskCommandGenerateDesignSheet))
	}
}

// createJobForm looks at the form's command and dispatches to a comic or a design sheet.
func (h *Handler) createJobForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	switch command := domain.TaskCommand(strings.TrimSpace(r.PostFormValue("command"))); command {
	case "", domain.TaskCommandComposeComic:
		h.createComicForm(w, r)
	case domain.TaskCommandGenerateDesignSheet:
		h.createDesignSheetForm(w, r)
	default:
		http.Error(w, fmt.Sprintf("command は %s / %s のいずれかです",
			domain.TaskCommandComposeComic, domain.TaskCommandGenerateDesignSheet), http.StatusBadRequest)
	}
}

// maxCreateJobBody is the limit on the JSON body of POST /jobs. The input is only short strings and choices.
const maxCreateJobBody = 64 << 10

// isJSONBody decides from Content-Type whether the body is JSON.
func isJSONBody(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json")
}

// createComicJSON enqueues a compose_comic job from a JSON body (POST /jobs).
// The server mints a new jobID and returns it in the response.
func (h *Handler) createComicJSON(w http.ResponseWriter, r *http.Request) {
	var req composeComicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}

	task, err := h.newComposeTask(req)
	if err != nil {
		respond.ServerErrorJSON(w, r, http.StatusInternalServerError, err, "failed to generate job id")
		return
	}

	if err := task.ValidateSubmission(); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.validateChoices(task); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, err.Error())
		return
	}

	h.enqueueAndRespond(w, r, task)
}

// enqueueAndRespond enqueues the Task and, on success, returns 202 Accepted with the jobID.
func (h *Handler) enqueueAndRespond(w http.ResponseWriter, r *http.Request, task domain.Task) {
	// Write the status first. The worker reads the status before the delivered task, so in the reverse order
	// it would read the previous succeeded and the enqueue would be silently dropped (see recordQueuedStatus).
	h.recordQueuedStatus(r, task)
	if err := h.taskQueue.Enqueue(r.Context(), task); err != nil {
		respond.ServerErrorJSON(w, r, http.StatusInternalServerError, err, "failed to enqueue task", "job_id", task.JobID, "command", task.Command)
		return
	}

	// Location is where to poll for progress. The next URL to call is known without reading the body.
	w.Header().Set("Location", "/jobs/"+task.JobID)
	respond.JSON(w, r, http.StatusAccepted, enqueueResponse{Status: "queued", JobID: task.JobID})
}
