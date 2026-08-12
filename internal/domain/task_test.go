package domain

import (
	"testing"
	"time"
)

func TestValidateSubmissionRequiresJobID(t *testing.T) {
	t.Parallel()

	task := Task{Command: TaskCommandComposeComic, SourceText: "text"}
	if err := task.ValidateSubmission(); err == nil {
		t.Error("ValidateSubmission without job_id succeeded, want error")
	}

	task.JobID = "../escape"
	if err := task.ValidateSubmission(); err == nil {
		t.Error("ValidateSubmission with invalid job_id succeeded, want error")
	}
}

func TestValidateSubmissionComposeComic(t *testing.T) {
	t.Parallel()

	base := Task{Command: TaskCommandComposeComic, JobID: "job-1"}
	if err := base.ValidateSubmission(); err == nil {
		t.Error("ValidateSubmission without source succeeded, want error")
	}

	withURL := base
	withURL.SourceURL = "gs://bucket/article.md"
	if err := withURL.ValidateSubmission(); err != nil {
		t.Errorf("ValidateSubmission with SourceURL failed: %v", err)
	}

	withText := base
	withText.SourceText = "元文章"
	if err := withText.ValidateSubmission(); err != nil {
		t.Errorf("ValidateSubmission with SourceText failed: %v", err)
	}
}

func TestValidateSubmissionPerCommandRequiredFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		task    Task
		wantErr bool
	}{
		{"chapter script missing chapter_id", Task{Command: TaskCommandRegenerateChapterScript, JobID: "job-1"}, true},
		{"chapter script ok", Task{Command: TaskCommandRegenerateChapterScript, JobID: "job-1", ChapterID: "ch01"}, false},
		{"design sheet missing character_ids", Task{Command: TaskCommandGenerateDesignSheet, JobID: "job-1"}, true},
		{"design sheet ok", Task{Command: TaskCommandGenerateDesignSheet, JobID: "job-1", CharacterIDs: []string{"zundamon"}}, false},
		{"design sheet reference override empty ok", Task{Command: TaskCommandGenerateDesignSheet, JobID: "job-1", CharacterIDs: []string{"zundamon"}, ReferenceURLOverride: ""}, false},
		{"design sheet reference override https png ok", Task{Command: TaskCommandGenerateDesignSheet, JobID: "job-1", CharacterIDs: []string{"zundamon"}, ReferenceURLOverride: "https://example.com/reference.png"}, false},
		{"design sheet reference override gs webp ok", Task{Command: TaskCommandGenerateDesignSheet, JobID: "job-1", CharacterIDs: []string{"zundamon"}, ReferenceURLOverride: "gs://bucket/reference.webp"}, false},
		{"design sheet reference override non-image extension", Task{Command: TaskCommandGenerateDesignSheet, JobID: "job-1", CharacterIDs: []string{"zundamon"}, ReferenceURLOverride: "https://example.com/reference.pdf"}, true},
		{"design sheet reference override no scheme", Task{Command: TaskCommandGenerateDesignSheet, JobID: "job-1", CharacterIDs: []string{"zundamon"}, ReferenceURLOverride: "example.com/reference.png"}, true},
		{"design sheet reference override not a url", Task{Command: TaskCommandGenerateDesignSheet, JobID: "job-1", CharacterIDs: []string{"zundamon"}, ReferenceURLOverride: "not a url"}, true},
		{"panel missing panel_id", Task{Command: TaskCommandRegeneratePanel, JobID: "job-1"}, true},
		{"panel ok", Task{Command: TaskCommandRegeneratePanel, JobID: "job-1", PanelID: "ch01-p01"}, false},
		{"page missing page", Task{Command: TaskCommandRegeneratePage, JobID: "job-1"}, true},
		{"page zero", Task{Command: TaskCommandRegeneratePage, JobID: "job-1", Page: 0}, true},
		{"page ok", Task{Command: TaskCommandRegeneratePage, JobID: "job-1", Page: 1}, false},
		{"unknown command", Task{Command: "unknown", JobID: "job-1"}, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.task.ValidateSubmission()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSubmission() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTaskNameIsDeterministicAndTargetsAreDistinct(t *testing.T) {
	t.Parallel()

	submittedAt := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC) // t1785492000

	cases := []struct {
		name string
		task Task
		want string
	}{
		{
			"compose_comic has no target and no submission suffix",
			Task{Command: TaskCommandComposeComic, JobID: "job-1", CreatedAt: submittedAt},
			"job-1-compose_comic",
		},
		{
			"chapter script includes chapter id",
			Task{Command: TaskCommandRegenerateChapterScript, JobID: "job-1", ChapterID: "ch01", CreatedAt: submittedAt},
			"job-1-regenerate_chapter_script-ch01-t1785492000",
		},
		{
			"design sheet includes joined character ids",
			Task{Command: TaskCommandGenerateDesignSheet, JobID: "job-1", CharacterIDs: []string{"zundamon", "metan"}, CreatedAt: submittedAt},
			"job-1-generate_design_sheet-zundamon_metan-t1785492000",
		},
		{
			"panel includes panel id",
			Task{Command: TaskCommandRegeneratePanel, JobID: "job-1", PanelID: "ch01-p03", CreatedAt: submittedAt},
			"job-1-regenerate_panel-ch01-p03-t1785492000",
		},
		{
			"page includes page number",
			Task{Command: TaskCommandRegeneratePage, JobID: "job-1", Page: 2, CreatedAt: submittedAt},
			"job-1-regenerate_page-p2-t1785492000",
		},
		{
			"render_comic has no target beyond the submission",
			Task{Command: TaskCommandRenderComic, JobID: "job-1", CreatedAt: submittedAt},
			"job-1-render_comic-t1785492000",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.task.TaskName(); got != tt.want {
				t.Errorf("TaskName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTaskNameDiffersAcrossDifferentTargetsOfSameJobAndCommand(t *testing.T) {
	t.Parallel()

	a := Task{Command: TaskCommandRegeneratePanel, JobID: "job-1", PanelID: "ch01-p01"}
	b := Task{Command: TaskCommandRegeneratePanel, JobID: "job-1", PanelID: "ch01-p02"}
	if a.TaskName() == b.TaskName() {
		t.Errorf("different panels produced the same TaskName: %q", a.TaskName())
	}
}

func TestRenderComicValidation(t *testing.T) {
	// render_comic の対象は state 全体なので job_id 以外の入力は不要
	task := Task{Command: TaskCommandRenderComic, JobID: "job-abc"}
	if err := task.ValidateSubmission(); err != nil {
		t.Errorf("ValidateSubmission() = %v, want nil", err)
	}

	missing := Task{Command: TaskCommandRenderComic}
	if err := missing.ValidateSubmission(); err == nil {
		t.Error("job_id 無しの render_comic が通っている")
	}
}

func TestTaskNameDiffersPerSubmissionForRepeatableCommands(t *testing.T) {
	t.Parallel()

	// 同じ対象への投げ直し（再開、および「気に入らないからもう一度」）が
	// Cloud Tasks の重複排除で黙って捨てられないこと。
	bases := []Task{
		{Command: TaskCommandRenderComic, JobID: "job-abc"},
		{Command: TaskCommandRegenerateChapterScript, JobID: "job-abc", ChapterID: "ch02"},
		{Command: TaskCommandRegeneratePanel, JobID: "job-abc", PanelID: "ch02-p07"},
		{Command: TaskCommandRegeneratePage, JobID: "job-abc", Page: 3},
		{Command: TaskCommandGenerateDesignSheet, JobID: "job-abc", CharacterIDs: []string{"zundamon"}},
	}
	for _, base := range bases {
		t.Run(string(base.Command), func(t *testing.T) {
			first := base
			first.CreatedAt = time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
			second := base
			second.CreatedAt = time.Date(2026, 7, 31, 10, 5, 0, 0, time.UTC)

			if first.TaskName() == second.TaskName() {
				t.Errorf("投入時刻が違うのに同じタスク名になっている: %q", first.TaskName())
			}

			// 同一投入（同じ時刻）は従来どおり1つにまとめられる
			same := base
			same.CreatedAt = first.CreatedAt
			if first.TaskName() != same.TaskName() {
				t.Errorf("同一投入で名前が変わっている: %q vs %q", first.TaskName(), same.TaskName())
			}
		})
	}
}

func TestComposeComicTaskNameIgnoresSubmissionTime(t *testing.T) {
	t.Parallel()

	// compose_comic は投入のたびに新しいジョブIDを採番するので名前はもともと
	// 投入ごとに変わる。時刻まで混ぜると、同一リクエストの再送すら排除できなくなる。
	base := Task{Command: TaskCommandComposeComic, JobID: "job-abc"}

	first := base
	first.CreatedAt = time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	retried := base
	retried.CreatedAt = time.Date(2026, 7, 31, 10, 5, 0, 0, time.UTC)

	if first.TaskName() != retried.TaskName() {
		t.Errorf("同じジョブの再送が別タスクになっている: %q vs %q", first.TaskName(), retried.TaskName())
	}
}
