package handlers

import (
	"strings"

	"github.com/shouni/ap-story/internal/domain"
)

// 本ファイルは、state や characters.json に記録された GCS URI を、署名 URL へ
// 302 リダイレクトする配信エンドポイントのパスへ変換します。バケット外や想定外の
// プレフィックスの URI は空文字を返し、画像リンク自体を出しません。

// comicImageWebPath は、state に記録された GCS 画像 URI（gs://bucket/comics/{jobID}/...）を
// 署名 URL リダイレクトエンドポイントのパス（/api/comics/{jobID}/images/...）へ変換します。
// ジョブディレクトリ外の URI や不正な形式は空文字を返します（画像リンクを出さない）。
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
	return "/api/comics/" + jobID + "/images/" + rel
}

// characterImageWebPath は、state に記録されたキャラクター画像の GCS URI
// （gs://bucket/character/...、ジョブに依存しない共有アセット）を、署名 URL
// リダイレクトエンドポイントのパス（/api/characters/images/...）へ変換します。
// バケット外や想定外のプレフィックスの URI は空文字を返します（画像リンクを出さない）。
func (h *Handler) characterImageWebPath(imageURI string) string {
	base := "gs://" + h.bucket + "/" + characterImagePrefix + "/"
	rel, ok := strings.CutPrefix(imageURI, base)
	if !ok || rel == "" {
		return ""
	}
	return "/api/characters/images/" + rel
}

// characterReferenceWebPath は、characters.json の reference_url / reference_urls に
// 記録されたマスター参照画像の GCS URI（gs://bucket/character-reference/...）を、
// 署名 URL リダイレクトエンドポイントのパス（/api/characters/reference/...）へ変換します。
// バケット外や想定外のプレフィックスの URI は空文字を返します（画像リンクを出さない）。
func (h *Handler) characterReferenceWebPath(imageURI string) string {
	base := "gs://" + h.bucket + "/" + characterReferenceImagePrefix + "/"
	rel, ok := strings.CutPrefix(imageURI, base)
	if !ok || rel == "" {
		return ""
	}
	return "/api/characters/reference/" + rel
}
