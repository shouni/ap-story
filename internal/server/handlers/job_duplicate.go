package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/shouni/ap-story/internal/domain"

	"github.com/shouni/go-serve-kit/respond"
)

// duplicateComicRequest is the JSON body of POST /jobs/{jobID}/duplicate.
type duplicateComicRequest struct {
	// Lang is the language the copy's pages are lettered in (ja / en). Empty means Japanese.
	Lang string `json:"lang"`
}

// JobDuplicate handles POST /jobs/{jobID}/duplicate. It mints a new job that copies the given
// work's script and panel images, to be lettered in the requested language.
//
// The copy starts with the source's dialogue as it is. The order from here is: translate it
// with PUT /jobs/{newJobID}/script, then send render_comic, which finds every panel drawn and
// composes only the pages. Nothing is translated here, because a translation that reads well is
// a judgement about each line and belongs to whoever checks the script.
func (h *Handler) JobDuplicate(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "jobID")
	if err := domain.ValidateJobID(sourceID); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, "invalid job id")
		return
	}

	var req duplicateComicRequest
	// An empty body is a copy in the default language.
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		respond.ErrorJSON(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// A copy taken while the source is being generated would hold whatever panels happened to
	// be finished at that moment, and nothing on the copy would say it is incomplete.
	if h.jobInFlight(r, sourceID) {
		respond.ErrorJSON(w, r, http.StatusConflict, "生成ジョブの実行中は複製できません（完了してからやり直してください）")
		return
	}
	// Refuse a source that is not there before minting anything: the worker would fail the same
	// way, but it would leave a failed job with a new ID behind.
	if _, err := h.repository.GetState(r.Context(), sourceID); err != nil {
		writeStateError(w, r, sourceID, err)
		return
	}

	jobID, err := domain.NewJobID()
	if err != nil {
		respond.ServerErrorJSON(w, r, http.StatusInternalServerError, err, "failed to generate job id")
		return
	}
	task := domain.Task{
		Command:     domain.TaskCommandDuplicateComic,
		JobID:       jobID,
		CreatedAt:   time.Now().UTC(),
		SourceJobID: sourceID,
		Lang:        strings.TrimSpace(req.Lang),
	}
	if err := task.ValidateSubmission(); err != nil {
		respond.ErrorJSON(w, r, http.StatusBadRequest, err.Error())
		return
	}

	h.enqueueAndRespond(w, r, task)
}
