package handlers

import (
	"github.com/shouni/ap-story/internal/domain"
)

// characterSummaryResponse is one character in the response of GET /characters.
// Image URLs are returned as gs:// URIs, as in the state (converting to signed URLs is
// the job of the /api/characters/reference/* redirect endpoint).
type characterSummaryResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ReferenceURL string `json:"reference_url,omitempty"`
}

// characterDetailResponse is the response of GET /characters/{characterID}.
type characterDetailResponse struct {
	ID            string                              `json:"id"`
	Name          string                              `json:"name"`
	ReferenceURL  string                              `json:"reference_url,omitempty"`
	ReferenceURLs map[string]string                   `json:"reference_urls,omitempty"`
	History       []domain.CharacterDesignHistoryItem `json:"history"`
}
