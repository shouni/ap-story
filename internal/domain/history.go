package domain

import "github.com/shouni/go-job-kit/paging"

// ComicHistory は履歴一覧に表示する1作品分のサマリです。
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

// PageMeta は履歴一覧のページング表示に必要なメタデータです。
// 実体は go-job-kit の paging.PageMeta です（JSON の形は従来と同じ）。
type PageMeta = paging.PageMeta

// ComicHistoryPage はページング済みの履歴一覧です。
type ComicHistoryPage struct {
	Items []ComicHistory `json:"items"`
	PageMeta
}

// CharacterDesignHistoryItem は、あるキャラクター単体のデザインシート生成1回分の記録です。
// comic_state.json は読まず、GCS 上の character/{characterID}/ 配下を直接列挙して得るため、
// Seed 等の詳細情報は持ちません（画像とジョブIDのみ）。
type CharacterDesignHistoryItem struct {
	JobID    string `json:"job_id"`
	ImageURL string `json:"image_url"`
}
