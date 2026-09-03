package handlers

import (
	"github.com/shouni/ap-story/internal/domain"
)

// characterSummaryResponse は GET /characters の1キャラクター分のレスポンスです。
// 画像 URL は state 同様 gs:// URI のまま返します（署名 URL 変換は
// /api/characters/reference/* リダイレクトエンドポイントの責務です）。
type characterSummaryResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ReferenceURL string `json:"reference_url,omitempty"`
}

// characterDetailResponse は GET /characters/{characterID} のレスポンスです。
type characterDetailResponse struct {
	ID            string                              `json:"id"`
	Name          string                              `json:"name"`
	ReferenceURL  string                              `json:"reference_url,omitempty"`
	ReferenceURLs map[string]string                   `json:"reference_urls,omitempty"`
	History       []domain.CharacterDesignHistoryItem `json:"history"`
}
