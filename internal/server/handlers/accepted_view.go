package handlers

import (
	"github.com/shouni/ap-story/internal/domain"
)

// acceptedData is the data passed to the accepted.html template.
// The wording and the progress link differ per command (comic generation goes to the work's detail page;
// a standalone design sheet appears in the character page's history once done, so it leads there).
type acceptedData struct {
	JobID         string
	Command       string
	Icon          string
	Heading       string
	Message       string
	ProgressURL   string
	ProgressLabel string
}

// newComposeAcceptedData builds the display data for the compose_comic accepted page.
func newComposeAcceptedData(jobID string) acceptedData {
	return acceptedData{
		JobID:         jobID,
		Command:       string(domain.TaskCommandComposeComic),
		Icon:          "bi-book-half",
		Heading:       "台本の生成を開始しました",
		Message:       "章立てとネームを作っています（数分かかります）。できあがった台本を確認してから、作品詳細で画像生成へ進んでください。",
		ProgressURL:   "/jobs/" + jobID,
		ProgressLabel: "台本を確認",
	}
}

// newDesignSheetAcceptedData builds the display data for the generate_design_sheet accepted page.
// With a single character chosen it leads to that character's page; with several (composed)
// it leads to the character list.
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
