package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/shouni/ap-story/internal/domain"
)

// writeJSON は status とともに v を JSON エンコードしてレスポンスに書き込みます。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}

// writeErrorJSON は status とエラーメッセージを JSON 形式でレスポンスに書き込みます。
func writeErrorJSON(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// writeStateError は GetState の失敗を JSON のエラー応答へ変換します。
//
// **「まだ無い」と「読めない」を同じ 404 にしません。** 権限不足や GCS 障害まで
// 404 にすると、障害の間ずっと全作品が「ありません」と見え、呼び出し側は待てば
// 直るのか消えたのかを区別できません。読めなかった側は原因をログへ残します
// （応答には出しません。ストレージの内部事情を呼び出し側へ渡さないためです）。
func writeStateError(w http.ResponseWriter, r *http.Request, jobID string, err error) {
	if errors.Is(err, domain.ErrStateNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "comic not found")
		return
	}
	slog.ErrorContext(r.Context(), "failed to load comic state", "job_id", jobID, "error", err)
	writeErrorJSON(w, http.StatusBadGateway, "failed to load comic state")
}
