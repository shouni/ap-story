package handlers

import (
	"github.com/shouni/ap-story/internal/domain"
)

// acceptedData は accepted.html テンプレートに渡すデータです。
// 文言・進捗リンクはコマンドごとに異なります（漫画生成は作品詳細、デザインシート
// 単体生成は完了後にキャラクターページの生成履歴に現れるため、そちらへ誘導します）。
type acceptedData struct {
	JobID         string
	Command       string
	Icon          string
	Heading       string
	Message       string
	ProgressURL   string
	ProgressLabel string
}

// newComposeAcceptedData は compose_comic 受付画面の表示データを構築します。
func newComposeAcceptedData(jobID string) acceptedData {
	return acceptedData{
		JobID:         jobID,
		Command:       string(domain.TaskCommandComposeComic),
		Icon:          "bi-book-half",
		Heading:       "台本の生成を開始しました",
		Message:       "章立てとネームを作っています（数分かかります）。できあがった台本を確認してから、作品詳細で画像生成へ進んでください。",
		ProgressURL:   "/history/" + jobID,
		ProgressLabel: "台本を確認",
	}
}

// newDesignSheetAcceptedData は generate_design_sheet 受付画面の表示データを構築します。
// キャラクターを1人だけ選択した場合はそのキャラクターページへ、複数選択（合成）の場合は
// キャラクター一覧へ誘導します。
func newDesignSheetAcceptedData(jobID string, characterIDs []string) acceptedData {
	progressURL := "/characters"
	if len(characterIDs) == 1 {
		progressURL = "/characters/" + characterIDs[0]
	}
	return acceptedData{
		JobID:         jobID,
		Command:       string(domain.TaskCommandGenerateDesignSheet),
		Icon:          "bi-palette",
		Heading:       "デザインシート生成を開始しました",
		Message:       "完了すると、キャラクターページの生成履歴に表示されます（通常は数分以内）。",
		ProgressURL:   progressURL,
		ProgressLabel: "キャラクターページで確認",
	}
}
