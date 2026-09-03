package handlers

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/shouni/ap-story/internal/domain"
)

// DefaultDesignSheetAspectRatio は、比率を指定せずにデザインシートを依頼したときに使う比率です。
//
// 既定のレイアウトが3面図ターンアラウンドなので、横長のほうが1体あたりの解像度を稼げます。
// パネル・ページの参照アンカーにするシートは 3:4（+ 単一ポーズ）で作ってください。
//
// キット側にも未指定時のフォールバック（ports.Config.AspectRatio）がありますが、
// そこへ落とさずここで埋めるのは、フォームと JSON API で答えが変わらないようにするためです。
const DefaultDesignSheetAspectRatio = "16:9"

// designSheetCharacterOption は design_sheet_form.html に渡す1キャラクター分の選択肢です。
type designSheetCharacterOption struct {
	ID      string
	Name    string
	Checked bool
}

// designSheetFormData は design_sheet_form.html テンプレートに渡すデータです。
// エラー時は入力値（選択済みキャラクター等）を保持したままフォームを再表示します。
type designSheetFormData struct {
	Characters []designSheetCharacterOption
	Models     []selectOption
	// StyleModes は画風の選択肢です。シートはパネル用ではなくシート用の画風指定
	// （styles.json の design_style）で生成されます。
	StyleModes []selectOption
	StyleMode  string
	// ModelOverride は選択されたモデル名です（空文字なら既定モデル）。
	ModelOverride string
	AspectRatio   string
	Layout        string
	// ReferenceURLOverride / VisualCuesOverride は、その場限りの参照画像・見た目特徴の
	// 上書きです（go-comic-kit の DesignOverride に対応）。キャラクターを1人だけ選択した
	// 場合のみ適用され、複数選択時（合成デザインシート）は無視されます。
	ReferenceURLOverride string
	VisualCuesOverride   string
	ErrorMessage         string
}

// buildDesignSheetFormData は選択済みキャラクターIDの集合とモデル選択肢を反映した
// フォーム表示用データを構築します。
func (h *Handler) buildDesignSheetFormData(selected []string) designSheetFormData {
	checked := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		checked[id] = struct{}{}
	}

	data := designSheetFormData{}
	if h.characters != nil {
		for _, c := range h.characters.All() {
			_, isChecked := checked[c.ID]
			data.Characters = append(data.Characters, designSheetCharacterOption{
				ID: c.ID, Name: c.Name, Checked: isChecked,
			})
		}
	}
	data.Models = modelOptions(h.imageModels, "")
	data.StyleModes = modeOptions(h.styleModes, "")
	return data
}

// splitVisualCues は、改行またはカンマ区切りの入力を見た目特徴のリストへ分割します。
// 空行・前後の空白は除去します。
func splitVisualCues(raw string) []string {
	if raw == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == ','
	})
	cues := make([]string, 0, len(fields))
	for _, f := range fields {
		if trimmed := strings.TrimSpace(f); trimmed != "" {
			cues = append(cues, trimmed)
		}
	}
	return cues
}

// ComposeDesignSheetForm は GET /compose/design-sheet を処理し、キャラクター単体のデザインシート
// 生成フォームを表示します。既存の作品（jobID）に紐づかない単発生成です。
// クエリパラメータ ?character_id=xxx で選択済み状態にできます
// （/characters/{characterID} の「新規生成」ボタンから遷移する際に使用）。
func (h *Handler) ComposeDesignSheetForm(w http.ResponseWriter, r *http.Request) {
	preselected := r.URL.Query()["character_id"]
	h.render(w, r, http.StatusOK, "design_sheet_form.html", "デザインシートを生成", h.buildDesignSheetFormData(preselected))
}

// designSheetTaskParams は generate_design_sheet ジョブ構築の入力です。
// Web フォーム（POST /jobs（command=generate_design_sheet））と JSON API（POST /jobs（command=generate_design_sheet））で共有します。
type designSheetTaskParams struct {
	CharacterIDs         []string
	AspectRatio          string
	Layout               string
	StyleMode            string
	ModelOverride        string
	ReferenceURLOverride string
	VisualCuesOverride   []string
	Seed                 *int64
}

// newDesignSheetTask は generate_design_sheet の入力からジョブ ID を採番して Task を構築します。
// job_id はユーザーに意識させず、サーバー側で新規採番します（state を伴わない単発生成のため
// 既存作品との紐づけは不要）。
func (h *Handler) newDesignSheetTask(p designSheetTaskParams) (domain.Task, error) {
	jobID, err := domain.NewJobID()
	if err != nil {
		return domain.Task{}, err
	}

	aspectRatio := strings.TrimSpace(p.AspectRatio)
	if aspectRatio == "" {
		aspectRatio = DefaultDesignSheetAspectRatio
	}

	return domain.Task{
		Command:              domain.TaskCommandGenerateDesignSheet,
		JobID:                jobID,
		CreatedAt:            time.Now().UTC(),
		CharacterIDs:         p.CharacterIDs,
		AspectRatio:          aspectRatio,
		Layout:               p.Layout,
		StyleMode:            p.StyleMode,
		ModelOverride:        firstIfEmpty(p.ModelOverride, h.imageModels),
		ReferenceURLOverride: p.ReferenceURLOverride,
		VisualCuesOverride:   p.VisualCuesOverride,
		Seed:                 p.Seed,
	}, nil
}

// resolveDesignSheetSeed は明示的な seed 指定を優先し、未指定かつキャラクターを1人だけ
// 指定している場合に characters.json 側の登録 seed へフォールバックします。複数キャラクター
// 指定時（合成デザインシート）はどちらのキャラクターの seed を使うべきか一意に決まらないため
// フォールバックしません。
func (h *Handler) resolveDesignSheetSeed(characterIDs []string, explicit *int64) *int64 {
	if explicit != nil {
		return explicit
	}
	if len(characterIDs) != 1 || h.characters == nil {
		return nil
	}
	char := h.characters.GetCharacter(characterIDs[0])
	if char == nil {
		return nil
	}
	return char.Seed
}

// createDesignSheetForm は POST /jobs（command=generate_design_sheet） を処理し、フォーム入力から
// generate_design_sheet ジョブを投入します。
func (h *Handler) createDesignSheetForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	characterIDs := r.PostForm["character_ids"]
	aspectRatio := strings.TrimSpace(r.PostFormValue("aspect_ratio"))
	layout := strings.TrimSpace(r.PostFormValue("layout"))
	styleMode := strings.TrimSpace(r.PostFormValue("style_mode"))
	modelOverride := strings.TrimSpace(r.PostFormValue("model_override"))
	referenceURLOverride := strings.TrimSpace(r.PostFormValue("reference_url_override"))
	visualCuesOverrideRaw := strings.TrimSpace(r.PostFormValue("visual_cues_override"))
	formData := h.buildDesignSheetFormData(characterIDs)
	formData.AspectRatio = aspectRatio
	formData.Layout = layout
	formData.StyleMode = styleMode
	formData.StyleModes = modeOptions(h.styleModes, styleMode)
	formData.ModelOverride = modelOverride
	formData.ReferenceURLOverride = referenceURLOverride
	formData.VisualCuesOverride = visualCuesOverrideRaw

	task, err := h.newDesignSheetTask(designSheetTaskParams{
		CharacterIDs:         characterIDs,
		AspectRatio:          aspectRatio,
		Layout:               layout,
		StyleMode:            styleMode,
		ModelOverride:        modelOverride,
		ReferenceURLOverride: referenceURLOverride,
		VisualCuesOverride:   splitVisualCues(visualCuesOverrideRaw),
		Seed:                 h.resolveDesignSheetSeed(characterIDs, nil),
	})
	if err != nil {
		slog.Error("failed to generate job id", "error", err)
		formData.ErrorMessage = "ジョブIDの採番に失敗しました。時間をおいて再度お試しください。"
		h.render(w, r, http.StatusInternalServerError, "design_sheet_form.html", "デザインシートを生成", formData)
		return
	}

	if err := task.ValidateSubmission(); err != nil {
		formData.ErrorMessage = err.Error()
		h.render(w, r, http.StatusBadRequest, "design_sheet_form.html", "デザインシートを生成", formData)
		return
	}

	if err := h.validateChoices(task); err != nil {
		formData.ErrorMessage = err.Error()
		h.render(w, r, http.StatusBadRequest, "design_sheet_form.html", "デザインシートを生成", formData)
		return
	}

	// 状態を先に書く（recordQueuedStatus 参照）。
	h.recordQueuedStatus(r, task)
	if err := h.taskQueue.Enqueue(r.Context(), task); err != nil {
		slog.Error("failed to enqueue task", "job_id", task.JobID, "command", task.Command, "error", err)
		formData.ErrorMessage = "ジョブの投入に失敗しました。時間をおいて再度お試しください。"
		h.render(w, r, http.StatusInternalServerError, "design_sheet_form.html", "デザインシートを生成", formData)
		return
	}

	h.render(w, r, http.StatusAccepted, "accepted.html", "受付完了", newDesignSheetAcceptedData(task.JobID, characterIDs))
}
