package handlers

import (
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/go-serve-kit/respond"
)

// characterImagePrefix は、ジョブ（作品）に依存しない共有キャラクター資産
// （AI生成デザインシート）が置かれる GCS 上のプレフィックス（バケット直下）です。
const characterImagePrefix = "character"

// characterReferenceImagePrefix は、キャラクターのマスター参照画像（characters.json の
// reference_url / reference_urls）が置かれる GCS 上のプレフィックスです。AI生成の
// デザインシート（characterImagePrefix）とは別管理です。
const characterReferenceImagePrefix = "character-reference"

// CharacterImage は GET /characters/images/* を処理し、AI生成の
// デザインシート（gs://{bucket}/character/... 配下）の GCS 署名 URL へ 302 リダイレクトします。
// comics/{jobID}/ 配下のパネル・ページ画像とは異なり jobID を経由しません。
func (h *Handler) CharacterImage(w http.ResponseWriter, r *http.Request) {
	h.redirectCharacterAsset(w, r, characterImagePrefix)
}

// CharacterReferenceImage は GET /characters/reference/* を処理し、
// キャラクターのマスター参照画像（gs://{bucket}/character-reference/... 配下）の
// GCS 署名 URL へ 302 リダイレクトします。
func (h *Handler) CharacterReferenceImage(w http.ResponseWriter, r *http.Request) {
	h.redirectCharacterAsset(w, r, characterReferenceImagePrefix)
}

// redirectCharacterAsset は、指定プレフィックス配下のキャラクター資産を署名 URL へ
// 302 リダイレクトする共通処理です。
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
		slog.ErrorContext(r.Context(), "failed to generate signed URL", "object_path", objectPath, "error", err)
		respond.Error(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	http.Redirect(w, r, signedURL, http.StatusFound)
}

// resolveCharacterAssetPath は、相対パスから GCS 上のキャラクター資産オブジェクトパス
// （バケット名を除く）を安全に組み立てます。生の入力に ".." を含むものは拒否します
// （path.Clean によるルート超えの無害化には頼らない設計。comics 側の同名ロジックと同じ方針）。
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
