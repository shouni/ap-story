package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/shouni/ap-story/internal/domain"

	"github.com/shouni/go-serve-kit/respond"
)

// JobScript handles GET /jobs/{jobID}/script and returns only the editable part of the script
// (the chapter headings and each panel's dialogue).
//
// The whole state is available from GET /jobs/{jobID}, but that includes the generation records and
// the assembled prompts and exceeds 100KB. It is heavy to read through for proofreading, and
// it is not in a shape that can be edited and sent back.
func (h *Handler) JobScript(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if err := domain.ValidateJobID(jobID); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, "invalid job id")
		return
	}

	state, err := h.repository.GetState(r.Context(), jobID)
	if err != nil {
		writeStateError(w, r, jobID, err)
		return
	}

	respond.JSON(w, r, http.StatusOK, domain.NewScriptDraft(state))
}

// JobScriptUpdate handles PUT /jobs/{jobID}/script and applies the dialogue of the submitted script
// to the state. Only the dialogue is applied; the panel structure cannot be moved
// (see domain.ScriptDraft.ApplyTo).
//
// Saving leaves the page images as they were. The image model draws the dialogue in at page
// composition, so putting the fixed text into the art requires recomposing the affected pages.
// Their page numbers are returned as affected_pages to tell the caller that saving is not
// the end of it.
func (h *Handler) JobScriptUpdate(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if err := domain.ValidateJobID(jobID); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, "invalid job id")
		return
	}

	var draft domain.ScriptDraft
	if err := json.NewDecoder(r.Body).Decode(&draft); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// When a generation job and an edit run at the same time, the state is "always overwrite, always latest",
	// so the later write wins and the loser's changes vanish without a trace. go-comic-kit's store has no
	// entry for conditional writes (GCS's ifGenerationMatch), so refusing requests likely to overlap
	// here is the only defence.
	if err := h.rejectWhileJobIsRunning(r, jobID); err != nil {
		respond.ErrorJSON(w, r, http.StatusConflict, err.Error())
		return
	}

	state, err := h.repository.GetState(r.Context(), jobID)
	if err != nil {
		writeStateError(w, r, jobID, err)
		return
	}

	before := domain.NewScriptDraft(state)
	if err := draft.ApplyTo(state, h.isKnownSpeaker); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, err.Error())
		return
	}

	after := domain.NewScriptDraft(state)
	change := domain.DiffScripts(before, after)

	// If nothing changed, nothing is written. Overwriting the state on every save would open the window
	// for a race with a running job for a request that changes nothing.
	if change.ChangedLines > 0 {
		if err := h.repository.SaveState(r.Context(), jobID, state); err != nil {
			respond.ServerErrorJSON(w, r, http.StatusInternalServerError, err, "failed to save comic script", "job_id", jobID)
			return
		}
	}

	respond.JSON(w, r, http.StatusOK, scriptUpdateResponse{
		JobID:        jobID,
		ScriptChange: change,
		Script:       after,
	})
}

// scriptUpdateResponse is the result of a script update.
type scriptUpdateResponse struct {
	JobID string `json:"job_id"`
	domain.ScriptChange
	// Script is the script after saving. Normalisation such as removing empty lines applies, so it
	// is not necessarily identical to what was sent.
	Script domain.ScriptDraft `json:"script"`
}

// isKnownSpeaker reports whether a speaker ID is a registered character.
func (h *Handler) isKnownSpeaker(id string) bool {
	if h.characters == nil {
		return true
	}
	return h.characters.GetCharacter(id) != nil
}

// rejectWhileJobIsRunning refuses edits while the target job is running.
//
// When no status is recorded (jobStatus unset, or a work made before status recording) it
// lets the request through. Stopping edits because the status cannot be followed would leave old
// works with no record unfixable forever.
func (h *Handler) rejectWhileJobIsRunning(r *http.Request, jobID string) error {
	if h.jobStatus == nil {
		return nil
	}
	status, err := h.jobStatus.Get(r.Context(), jobID)
	if err != nil {
		return nil
	}
	if status.State == domain.JobStateQueued || status.State == domain.JobStateRunning {
		return errors.New("生成ジョブの実行中は台本を編集できません（完了してから保存し直してください）")
	}
	return nil
}
