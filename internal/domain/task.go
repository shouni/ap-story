package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// TaskCommand はパイプラインで実行するジョブ種別です。
// 各コマンドは go-comic-kit の冪等な操作（ports.OutlineGenerator 等）と1対1で対応します。
type TaskCommand string

const (
	// TaskCommandComposeComic は、原稿から章立て・台本・パネル・ページまで一括で生成する
	// コマンドです。StopAfterScript を指定すると台本までで止まり、続きは
	// TaskCommandRenderComic で行います。
	TaskCommandComposeComic TaskCommand = "compose_comic"
	// TaskCommandRenderComic は、既存 state のパネル画像とページ画像だけを生成するコマンドです。
	// 台本を確認してから画像生成へ進む「続きを生成」と、失敗・打ち切りからの再開を兼ねます。
	// すでに生成済みのコマ・ページは飛ばすため、何度実行しても未生成分だけが埋まります。
	TaskCommandRenderComic TaskCommand = "render_comic"
	// TaskCommandRegenerateChapterScript は、指定章の台本（ネーム）のみを再生成するコマンドです。
	TaskCommandRegenerateChapterScript TaskCommand = "regenerate_chapter_script"
	// TaskCommandGenerateDesignSheet は、キャラクターデザインシートを生成するコマンドです。
	// job_id は省略可（state を伴わない単発生成）です。
	TaskCommandGenerateDesignSheet TaskCommand = "generate_design_sheet"
	// TaskCommandRegeneratePanel は、指定パネルの画像を生成/再生成するコマンドです。
	TaskCommandRegeneratePanel TaskCommand = "regenerate_panel"
	// TaskCommandRegeneratePage は、指定ページを合成/再合成するコマンドです。
	TaskCommandRegeneratePage TaskCommand = "regenerate_page"
)

// Task は Cloud Tasks 経由で Worker に渡される生成ジョブです。
// JSON にシリアライズしてタスクのペイロードとして送信します。
type Task struct {
	Command   TaskCommand `json:"command"`
	JobID     string      `json:"job_id"`
	CreatedAt time.Time   `json:"created_at"`

	// --- compose_comic / regenerate_chapter_script 共通（台本生成） ---
	// SourceURL / SourceText は compose_comic の入力（いずれか必須、排他）です。
	SourceURL  string `json:"source_url,omitempty"`
	SourceText string `json:"source_text,omitempty"`
	// ScriptMode は台本プロンプトのモード（省略時はキット既定）です。
	ScriptMode string `json:"script_mode,omitempty"`
	// StyleMode は画像生成スタイルの選択です。
	StyleMode string `json:"style_mode,omitempty"`
	// ChapterID は regenerate_chapter_script の対象章です。
	ChapterID string `json:"chapter_id,omitempty"`
	// StopAfterScript を指定すると、compose_comic は章立てと台本の生成までで止まります。
	// 高価な画像生成に進む前に内容を確認するための指定で、続きは render_comic で行います。
	StopAfterScript bool `json:"stop_after_script,omitempty"`

	// --- generate_design_sheet ---
	CharacterIDs []string `json:"character_ids,omitempty"`
	AspectRatio  string   `json:"aspect_ratio,omitempty"`
	Layout       string   `json:"layout,omitempty"`
	// ReferenceURLOverride / VisualCuesOverride は単一キャラクター指定時のみ適用される
	// その場限りの上書きです（go-comic-kit の DesignOverride に対応）。
	ReferenceURLOverride string   `json:"reference_url_override,omitempty"`
	VisualCuesOverride   []string `json:"visual_cues_override,omitempty"`
	// ModelOverride は、設定済みの画像生成モデル（既定は品質重視モデル）を差し替えます。
	// 空文字なら既定のモデルを使います。
	ModelOverride string `json:"model_override,omitempty"`

	// --- regenerate_panel ---
	PanelID string `json:"panel_id,omitempty"`

	// --- regenerate_page ---
	Page int `json:"page,omitempty"`

	// --- regenerate_panel / regenerate_page 共通（go-comic-kit の GenerateOptions に対応） ---
	// Seed は nil の場合、対象の前回生成条件（GenerationRecord.UsedSeed）を再利用します。
	Seed *int64 `json:"seed,omitempty"`
	// EditPrompt を指定すると、既存の生成済み画像を入力とした編集モードになります。
	EditPrompt string `json:"edit_prompt,omitempty"`
	// PromptOverride は自動構築されるプロンプトを差し替えます。
	PromptOverride string `json:"prompt_override,omitempty"`
}

// ValidateSubmission は、ジョブ投入前に最低限必要な入力が揃っていることを検証します。
func (t Task) ValidateSubmission() error {
	if strings.TrimSpace(t.JobID) == "" {
		return fmt.Errorf("job_id is required")
	}
	if err := ValidateJobID(t.JobID); err != nil {
		return fmt.Errorf("invalid job_id: %w", err)
	}

	switch t.Command {
	case TaskCommandComposeComic:
		return t.validateComposeComicSubmission()
	case TaskCommandRenderComic:
		// 対象は state 全体なので、job_id 以外に必要な入力はありません。
		return nil
	case TaskCommandRegenerateChapterScript:
		if strings.TrimSpace(t.ChapterID) == "" {
			return fmt.Errorf("chapter_id is required for command %q", TaskCommandRegenerateChapterScript)
		}
		return nil
	case TaskCommandGenerateDesignSheet:
		if len(t.CharacterIDs) == 0 {
			return fmt.Errorf("character_ids is required for command %q", TaskCommandGenerateDesignSheet)
		}
		if err := validateReferenceURLOverride(t.ReferenceURLOverride); err != nil {
			return err
		}
		return nil
	case TaskCommandRegeneratePanel:
		if strings.TrimSpace(t.PanelID) == "" {
			return fmt.Errorf("panel_id is required for command %q", TaskCommandRegeneratePanel)
		}
		return nil
	case TaskCommandRegeneratePage:
		if t.Page <= 0 {
			return fmt.Errorf("page must be a positive integer for command %q", TaskCommandRegeneratePage)
		}
		return nil
	default:
		return fmt.Errorf("unsupported command: %s", t.Command)
	}
}

func (t Task) validateComposeComicSubmission() error {
	if strings.TrimSpace(t.SourceURL) == "" && strings.TrimSpace(t.SourceText) == "" {
		return fmt.Errorf("at least one input is required: source_url or source_text")
	}
	return nil
}

// referenceImageURLPattern は参照画像URL（上書き・任意）として許容する形式です。
// http(s):// または gs:// で始まり、画像拡張子で終わる必要があります。
var referenceImageURLPattern = regexp.MustCompile(`(?i)^(https?://|gs://)\S+\.(png|jpe?g|webp|gif)$`)

// validateReferenceURLOverride は参照画像URL（上書き・任意）の形式を検証します。
// 実際に画像かどうかの最終判定はダウンロード後にワーカー側（gemini-image-kit の
// MIME判定）で行われるため、ここでは明らかに不正な値をフォーム送信時点で早期に
// 弾く軽量チェックに留めます。空文字（未指定）は許可します。
func validateReferenceURLOverride(url string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil
	}
	if !referenceImageURLPattern.MatchString(url) {
		return fmt.Errorf("reference_url_override must be an http(s):// or gs:// URL pointing to an image (.png/.jpg/.jpeg/.webp/.gif)")
	}
	return nil
}

// TaskName は Cloud Tasks の決定的なタスク名（重複 enqueue 排除キー）を返します。
// 同じジョブ・同じコマンド・同じ対象への短時間の重複投入（Cloud Tasks の at-least-once
// 配信や呼び出し元の再試行によるもの）を1つのタスクにまとめます。意図的に同一対象へ
// 続けて再生成をリクエストした場合は、Cloud Tasks 側のタスク名衝突（ALREADY_EXISTS）に
// より一時的に弾かれることがありますが、これは仕様（同一内容の連投を抑止する）です。
// ジョブIDは英数字とハイフンのみ（ValidateJobID で検証済み）、ChapterID/PanelID は
// システム側で "ch01" / "ch01-p03" 形式に採番されるため、追加のサニタイズなしで
// Cloud Tasks のタスク名文字制約（英数字・ハイフン・アンダースコア）を満たします。
func (t Task) TaskName() string {
	target := t.taskTarget()
	if target == "" {
		return fmt.Sprintf("%s-%s", t.JobID, t.Command)
	}
	return fmt.Sprintf("%s-%s-%s", t.JobID, t.Command, target)
}

func (t Task) taskTarget() string {
	switch t.Command {
	case TaskCommandRenderComic:
		// render_comic は「失敗したところから再開する」ためのコマンドなので、
		// 決定的な名前で重複排除すると、正当な再開リクエストが
		// ALREADY_EXISTS として黙って捨てられてしまう（Cloud Tasks の重複排除は
		// 完了後もしばらく効くため、45分のジョブが失敗した直後の再実行がまさに該当する）。
		// 生成済みのコマ・ページは飛ばすので重ねて実行しても無駄にはならない。
		// 取りこぼしを避けるほうを優先して、投入時刻で名前を分ける。
		return fmt.Sprintf("t%d", t.CreatedAt.UTC().Unix())
	case TaskCommandRegenerateChapterScript:
		return t.ChapterID
	case TaskCommandGenerateDesignSheet:
		return strings.Join(t.CharacterIDs, "_")
	case TaskCommandRegeneratePanel:
		return t.PanelID
	case TaskCommandRegeneratePage:
		return fmt.Sprintf("p%d", t.Page)
	default:
		return ""
	}
}
