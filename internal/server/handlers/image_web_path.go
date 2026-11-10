package handlers

import (
	"strings"

	"github.com/shouni/ap-story/internal/domain"
)

// This file converts GCS URIs recorded in the state or characters.json into the paths of the
// serving endpoints that 302-redirect to a signed URL. A URI outside the bucket or with an
// unexpected prefix gives the empty string, and no image link is shown at all.

// comicImageWebPath converts a GCS image URI recorded in the state (gs://bucket/comics/{jobID}/...)
// into the path of the signed-URL redirect endpoint (/jobs/{jobID}/images/...).
// A URI outside the job directory or in an invalid form gives the empty string (no image link).
func (h *Handler) comicImageWebPath(jobID, imageURI string) string {
	prefix, err := domain.JobObjectPrefix(jobID)
	if err != nil {
		return ""
	}
	base := "gs://" + h.bucket + "/" + prefix + "/"
	rel, ok := strings.CutPrefix(imageURI, base)
	if !ok || rel == "" {
		return ""
	}
	return "/jobs/" + jobID + "/images/" + rel
}

// characterImageWebPath converts the GCS URI of a character image recorded in the state
// (gs://bucket/character/..., a shared asset that belongs to no job) into the path of the signed-URL
// redirect endpoint (/characters/images/...).
// A URI outside the bucket or with an unexpected prefix gives the empty string (no image link).
func (h *Handler) characterImageWebPath(imageURI string) string {
	base := "gs://" + h.bucket + "/" + characterImagePrefix + "/"
	rel, ok := strings.CutPrefix(imageURI, base)
	if !ok || rel == "" {
		return ""
	}
	return "/characters/images/" + rel
}

// characterReferenceWebPath converts the GCS URI of a master reference image recorded in characters.json's
// reference_url / reference_urls (gs://bucket/character-reference/...) into the path of the
// signed-URL redirect endpoint (/characters/reference/...).
// A URI outside the bucket or with an unexpected prefix gives the empty string (no image link).
func (h *Handler) characterReferenceWebPath(imageURI string) string {
	base := "gs://" + h.bucket + "/" + characterReferenceImagePrefix + "/"
	rel, ok := strings.CutPrefix(imageURI, base)
	if !ok || rel == "" {
		return ""
	}
	return "/characters/reference/" + rel
}
