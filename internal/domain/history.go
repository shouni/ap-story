package domain

import "github.com/shouni/go-job-kit/paging"

// ComicHistory is the summary of one work shown in the history list.
type ComicHistory struct {
	JobID        string `json:"job_id"`
	Title        string `json:"title"`
	StyleMode    string `json:"style_mode,omitempty"`
	ChapterCount int    `json:"chapter_count"`
	PanelCount   int    `json:"panel_count"`
	PageCount    int    `json:"page_count"`
	CreatedAt    string `json:"created_at,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

// PageMeta is the metadata the history list needs for paging.
// It is go-job-kit's paging.PageMeta (the JSON shape is unchanged).
type PageMeta = paging.PageMeta

// ComicHistoryPage is a paged history list.
type ComicHistoryPage struct {
	Items []ComicHistory `json:"items"`
	PageMeta
}

// CharacterDesignHistoryItem records one design-sheet generation for a single character.
// It is obtained by listing character/{characterID}/ on GCS directly, without reading
// comic_state.json, so it carries no details such as Seed (only the image and the job ID).
type CharacterDesignHistoryItem struct {
	JobID    string `json:"job_id"`
	ImageURL string `json:"image_url"`
}
