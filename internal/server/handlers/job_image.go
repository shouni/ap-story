package handlers

import (
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/domain"

	"github.com/shouni/go-serve-kit/respond"
)

// signedURLExpiration is the lifetime of the signed URLs used to serve images.
const signedURLExpiration = 30 * time.Minute

var (
	errImagePathRequired = errors.New("image path is required")
	errInvalidImagePath  = errors.New("invalid image path")
)

// JobImage handles GET /jobs/{jobID}/images/* and 302-redirects to the GCS signed URL of the
// given image (design sheet, panel or page).
// The app never serves the image bytes itself.
func (h *Handler) JobImage(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	if err := domain.ValidateJobID(jobID); err != nil {
		respond.Error(w, r, http.StatusBadRequest, "invalid job id")
		return
	}

	relPath := chi.URLParam(r, "*")
	objectPath, err := resolveComicImagePath(jobID, relPath)
	if err != nil {
		respond.Error(w, r, http.StatusBadRequest, err.Error())
		return
	}

	objectURI := remoteio.BuildURI(remoteio.SchemeGCS, h.bucket, objectPath)
	signedURL, err := h.signer.SignURL(r.Context(), objectURI, http.MethodGet, signedURLExpiration)
	if err != nil {
		respond.ServerError(w, r, http.StatusInternalServerError, err, "failed to generate signed URL", "object_path", objectPath)
		return
	}

	http.Redirect(w, r, signedURL, http.StatusFound)
}

// resolveComicImagePath safely builds the GCS image object path (without the bucket name) from the
// jobID and a relative path. A path leaving the job directory
// (containing "..") is rejected.
func resolveComicImagePath(jobID, relPath string) (string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", errImagePathRequired
	}
	// path.Clean neutralises an escape past the root such as "/../escape" into "/escape",
	// silently accepting an unintended path, so ".." segments are rejected in the raw input.
	// It checks per segment rather than by substring, so that a legitimate file name such as
	// "image..01.png" is not rejected by mistake.
	if strings.Contains(relPath, "../") || strings.HasSuffix(relPath, "..") || relPath == ".." {
		return "", errInvalidImagePath
	}

	prefix, err := domain.JobObjectPrefix(jobID)
	if err != nil {
		return "", err
	}

	cleaned := path.Clean("/" + relPath)
	return prefix + cleaned, nil
}
