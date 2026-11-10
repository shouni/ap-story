package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/shouni/go-serve-kit/respond"

	"github.com/shouni/ap-story/internal/domain"
)

// This file gathers the paths where people and machines look at the same thing.
//
// Keeping two implementations that differ only in representation makes the screen and the machine-readable
// result disagree when only one is fixed. Fetching and validation happen once, and Accept decides HTML or JSON
// at the end. Things only one kind of reader has (input forms, regeneration instructions, image
// transfers and so on) are separate resources and do not live here.

// JobList returns the list of works (compose_comic jobs) (GET /jobs?page=).
func (h *Handler) JobList(w http.ResponseWriter, r *http.Request) {
	page := parseHistoryPage(r)
	historyPage, err := h.repository.ListHistoryPage(r.Context(), page, defaultHistoryPageSize)
	if err != nil {
		respond.ServerError(w, r, http.StatusInternalServerError, err, "failed to list comic history")
		return
	}

	if respond.WantsJSON(w, r) {
		respond.JSON(w, r, http.StatusOK, historyPage)
		return
	}
	h.render(w, r, http.StatusOK, "history.html", "作品履歴", historyPage)
}

// jobDocument is the JSON response of GET /jobs/{jobID}.
//
// On top of the progress (JobStatus), for a job whose state can be read the work's state goes in the same document.
// The same URL reads in the same shape from just after enqueue until delete, so the caller never
// switches URLs. The state is nested because it also has job_id and title of its own, and laid flat
// encoding/json would drop both keys.
type jobDocument struct {
	domain.JobStatus
	Comic any `json:"comic,omitempty"`
}

// Job returns one job (GET /jobs/{jobID}).
//
// If the state (comic_state.json) can be read, it is the work's details; if not yet, the progress. The screen
// shows the "in progress or missing" notice, and a machine gets the status if recorded, or 404 if not.
// A design-sheet job has no state, so it is always the progress (its artifacts are listed
// under /characters/{characterID}).
func (h *Handler) Job(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if err := domain.ValidateJobID(jobID); err != nil {
		respond.Error(w, r, http.StatusBadRequest, "invalid job id")
		return
	}

	status := h.loadJobStatus(r, jobID)
	state, err := h.repository.GetState(r.Context(), jobID)

	if respond.WantsJSON(w, r) {
		switch {
		case err == nil:
			respond.JSON(w, r, http.StatusOK, jobDocument{JobStatus: completedStatus(jobID, status), Comic: state})
		case status != nil && errors.Is(err, domain.ErrStateNotFound):
			respond.JSON(w, r, http.StatusOK, *status)
		default:
			writeStateError(w, r, jobID, err)
		}
		return
	}

	if err != nil {
		if !errors.Is(err, domain.ErrStateNotFound) {
			slog.ErrorContext(r.Context(), "failed to load comic state", "job_id", jobID, "error", err)
			http.Error(w, "作品の読み込みに失敗しました。時間をおいて開き直してください。", http.StatusBadGateway)
			return
		}
		code := http.StatusNotFound
		if status != nil {
			// If there is a record, it is "not yet" rather than "missing", so the notice is a success response.
			code = http.StatusOK
		}
		h.render(w, r, code, "history_pending.html", "処理中または未存在", historyPendingData{JobID: jobID})
		return
	}
	h.render(w, r, http.StatusOK, "history_detail.html", state.Title, h.buildDetailData(jobID, state))
}

// loadJobStatus returns the recorded progress. It is nil when there is none (a job older than the status
// feature) or it cannot be read. Unreadable is not an error because the work's details can be drawn from the state.
func (h *Handler) loadJobStatus(r *http.Request, jobID string) *domain.JobStatus {
	if h.jobStatus == nil {
		return nil
	}
	status, err := h.jobStatus.Get(r.Context(), jobID)
	if err != nil {
		if !errors.Is(err, domain.ErrJobStatusNotFound) {
			slog.WarnContext(r.Context(), "failed to load job status", "job_id", jobID, "error", err)
		}
		return nil
	}
	return &status
}

// completedStatus returns the status of a job whose state could be read. Even a job with no record
// has at least its script done once its state can be read.
func completedStatus(jobID string, status *domain.JobStatus) domain.JobStatus {
	if status != nil {
		return *status
	}
	var completed domain.JobStatus
	completed.JobID = jobID
	completed.State = domain.JobStateSucceeded
	return completed
}

// Characters returns every character in characters.json.
//
// The screen rewrites thumbnail URLs to web paths, but machines get the reference URLs exactly
// as defined. A rewritten URL is tied to the screen's image endpoint, so for a machine
// it would not say where the material lives.
func (h *Handler) Characters(w http.ResponseWriter, r *http.Request) {
	if respond.WantsJSON(w, r) {
		items := make([]characterSummaryResponse, 0, h.characters.Len())
		for _, c := range h.characters.All() {
			items = append(items, characterSummaryResponse{
				ID:           c.ID,
				Name:         c.Name,
				ReferenceURL: c.ReferenceURL,
			})
		}
		respond.JSON(w, r, http.StatusOK, items)
		return
	}

	items := make([]characterListItem, 0, h.characters.Len())
	for _, c := range h.characters.All() {
		items = append(items, characterListItem{
			ID:                c.ID,
			Name:              c.Name,
			ThumbnailImageURL: h.characterReferenceWebPath(c.ReferenceURL),
		})
	}
	h.render(w, r, http.StatusOK, "characters.html", "Characters", items)
}

// Character returns the master reference images and the design-sheet history.
//
// The screen cuts the history at characterDetailHistoryLimit items, but machines get everything.
// The screen's limit is for a readable list, not a reason to drop the one image the caller
// is looking for.
func (h *Handler) Character(w http.ResponseWriter, r *http.Request) {
	characterID := chi.URLParam(r, "characterID")
	char := h.characters.GetCharacter(characterID)
	if char == nil {
		respond.Error(w, r, http.StatusNotFound, "character not found")
		return
	}

	if respond.WantsJSON(w, r) {
		history, err := h.repository.ListCharacterDesignHistory(r.Context(), characterID)
		if err != nil {
			respond.ServerError(w, r, http.StatusInternalServerError, err, "failed to list character design history", "character_id", characterID)
			return
		}
		respond.JSON(w, r, http.StatusOK, characterDetailResponse{
			ID:            char.ID,
			Name:          char.Name,
			ReferenceURL:  char.ReferenceURL,
			ReferenceURLs: char.ReferenceURLs,
			History:       history,
		})
		return
	}

	data := characterDetailData{
		CharacterID: characterID,
		Name:        char.Name,
		References:  h.buildCharacterReferences(char.ReferenceURL, char.ReferenceURLs),
	}

	history, err := h.buildCharacterHistoryImages(r, characterID)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	data.HistoryTotal = len(history)
	if len(history) > characterDetailHistoryLimit {
		history = history[:characterDetailHistoryLimit]
	}
	data.History = history

	h.render(w, r, http.StatusOK, "character_detail.html", char.Name, data)
}
