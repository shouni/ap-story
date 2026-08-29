package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/shouni/go-remote-io/remoteio"

	"github.com/shouni/ap-story/internal/domain"

	"github.com/shouni/go-serve-kit/respond"
)

// signedURLExpiration は画像配信用の署名 URL の有効期限です。
const signedURLExpiration = 30 * time.Minute

var (
	errImagePathRequired = errors.New("image path is required")
	errInvalidImagePath  = errors.New("invalid image path")
)

// RedirectComicImage は GET /api/comics/{jobID}/images/* を処理し、指定された画像
// （デザインシート・パネル・ページ）の GCS 署名 URL へ 302 リダイレクトします。
// 画像バイト列そのものはアプリから配信しません。
func (h *Handler) RedirectComicImage(w http.ResponseWriter, r *http.Request) {
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
		slog.ErrorContext(r.Context(), "failed to generate signed URL", "object_path", objectPath, "error", err)
		respond.Error(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	http.Redirect(w, r, signedURL, http.StatusFound)
}

// resolveComicImagePath は jobID と相対パスから、GCS 上の画像オブジェクトパス
// （バケット名を除く）を安全に組み立てます。ジョブディレクトリの外に出るパス
// （".." を含むもの）は拒否します。
func resolveComicImagePath(jobID, relPath string) (string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", errImagePathRequired
	}
	// path.Clean は "/../escape" のようなルート超えを "/escape" へ無害化してしまい、
	// 意図しないパスを黙って受理してしまうため、生の入力の時点で ".." セグメントを拒否する。
	// 部分一致ではなくセグメント単位で判定し、"image..01.png" のような正当なファイル名を
	// 誤って弾かないようにする。
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
