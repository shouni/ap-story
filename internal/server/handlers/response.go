package handlers

import (
	"errors"
	"net/http"

	"github.com/shouni/ap-story/internal/domain"

	"github.com/shouni/go-serve-kit/respond"
)

// writeStateError converts a GetState failure into a JSON error response.
//
// **"Not there yet" and "unreadable" are not the same 404.** Making missing permissions or a GCS outage
// a 404 too would make every work look "not there" for the whole outage, and the caller could not tell
// whether waiting would fix it or it was gone. The unreadable side logs its cause
// (it is not put in the response, so storage internals are not handed to the caller).
func writeStateError(w http.ResponseWriter, r *http.Request, jobID string, err error) {
	if errors.Is(err, domain.ErrStateNotFound) {
		respond.ErrorJSON(w, r, http.StatusNotFound, "comic not found")
		return
	}
	respond.ServerErrorJSON(w, r, http.StatusBadGateway, err, "failed to load comic state", "job_id", jobID)
}
