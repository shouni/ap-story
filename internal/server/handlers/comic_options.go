package handlers

import (
	"net/http"

	"github.com/shouni/ap-story/internal/adapters/prompts"

	"github.com/shouni/go-serve-kit/respond"
)

// comicOptionsResponse は GET /comic-options のレスポンスです。
//
// フォームの <select> に出しているものと同じ一覧を JSON でも返します。ブラウザは
// 選択肢を見て選べますが、JSON API の呼び出し側（MCP サーバー経由のエージェント）には
// 何が指定できるのか知る手段がありません。投入時の許可リストはこの一覧なので、
// 知らずに送れば 400 になります。
type comicOptionsResponse struct {
	// ScriptModes / StyleModes は説明付きです。モード名だけでは選べません。
	ScriptModes []comicModeOption `json:"script_modes"`
	StyleModes  []comicModeOption `json:"style_modes"`
	// TextModels / ImageModels は先頭が既定モデルです。
	TextModels  []string `json:"text_models"`
	ImageModels []string `json:"image_models"`
}

// comicModeOption は台本モード・スタイルモード1件分の説明です。
// テンプレート側の front matter / styles.json がそのまま出どころです。
type comicModeOption struct {
	Name      string `json:"name"`
	Direction string `json:"direction,omitempty"`
	UseWhen   string `json:"use_when,omitempty"`
	Avoids    string `json:"avoids,omitempty"`
	Category  string `json:"category,omitempty"`
}

// ComicOptions は GET /comic-options を処理し、生成ジョブに指定できる
// モードとモデルの一覧を返します。
func (h *Handler) ComicOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respond.ErrorJSON(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	respond.JSON(w, r, http.StatusOK, comicOptionsResponse{
		ScriptModes: comicModeOptions(h.scriptModes),
		StyleModes:  comicModeOptions(h.styleModes),
		TextModels:  h.geminiModels,
		ImageModels: h.imageModels,
	})
}

func comicModeOptions(modes []prompts.ModeInfo) []comicModeOption {
	options := make([]comicModeOption, 0, len(modes))
	for _, mode := range modes {
		options = append(options, comicModeOption{
			Name:      mode.Name,
			Direction: mode.Direction,
			UseWhen:   mode.UseWhen,
			Avoids:    mode.Avoids,
			Category:  mode.Category,
		})
	}
	return options
}
