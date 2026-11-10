package handlers

import (
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/go-serve-kit/respond"
)

// characterImagePrefix is the GCS prefix (directly under the bucket) holding shared character
// assets (AI-generated design sheets) that belong to no job (work).
const characterImagePrefix = "character"

// characterReferenceImagePrefix is the GCS prefix holding the characters' master reference images
// (characters.json's reference_url / reference_urls). They are managed separately from the
// AI-generated design sheets (characterImagePrefix).
const characterReferenceImagePrefix = "character-reference"

// CharacterImage handles GET /characters/images/* and 302-redirects to the GCS signed URL of an
// AI-generated design sheet (under gs://{bucket}/character/...).
// Unlike the panel and page images under comics/{jobID}/, it does not go through a jobID.
func (h *Handler) CharacterImage(w http.ResponseWriter, r *http.Request) {
	h.redirectCharacterAsset(w, r, characterImagePrefix)
}

// CharacterReferenceImage handles GET /characters/reference/* and 302-redirects to
// the GCS signed URL of a character's master reference image (under
// gs://{bucket}/character-reference/...).
func (h *Handler) CharacterReferenceImage(w http.ResponseWriter, r *http.Request) {
	h.redirectCharacterAsset(w, r, characterReferenceImagePrefix)
}

// redirectCharacterAsset is the shared code that 302-redirects a character asset under the given
// prefix to a signed URL.
func (h *Handler) redirectCharacterAsset(w http.ResponseWriter, r *http.Request, prefix string) {
	relPath := chi.URLParam(r, "*")
	objectPath, err := resolveCharacterAssetPath(prefix, relPath)
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

// resolveCharacterAssetPath safely builds the GCS object path of a character asset (without the
// bucket name) from a relative path. Raw input containing ".." is rejected
// (by design it does not rely on path.Clean neutralising escapes past the root; the same policy as the comics-side logic of the same name).
func resolveCharacterAssetPath(prefix, relPath string) (string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", errImagePathRequired
	}
	if strings.Contains(relPath, "../") || strings.HasSuffix(relPath, "..") || relPath == ".." {
		return "", errInvalidImagePath
	}

	cleaned := path.Clean("/" + relPath)
	return prefix + cleaned, nil
}
