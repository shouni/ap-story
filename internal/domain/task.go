package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// TaskCommand is the kind of job the pipeline runs.
// Each command corresponds one-to-one with an idempotent go-comic-kit operation (ports.OutlineGenerator and so on).
type TaskCommand string

const (
	// TaskCommandComposeComic generates everything from the manuscript in one go: chapters, script,
	// panels and pages. With StopAfterScript it stops after the script, and the rest is done
	// with TaskCommandRenderComic.
	TaskCommandComposeComic TaskCommand = "compose_comic"
	// TaskCommandRenderComic generates only the panel and page images of an existing state.
	// It is both "continue generating" after checking the script and the resume after a failure or cutoff.
	// Panels and pages already generated are skipped, so however often it runs, only the missing ones are filled.
	//
	// With chapter_id it generates only that chapter's panels and pages. Images are the most
	// expensive part of the process, so one chapter can be tried before moving on to the rest.
	TaskCommandRenderComic TaskCommand = "render_comic"
	// TaskCommandRegenerateChapterScript regenerates only the script (name) of the given chapter.
	TaskCommandRegenerateChapterScript TaskCommand = "regenerate_chapter_script"
	// TaskCommandGenerateDesignSheet generates a character design sheet.
	// job_id is optional (a one-off generation with no state).
	TaskCommandGenerateDesignSheet TaskCommand = "generate_design_sheet"
	// TaskCommandRegeneratePanel generates or regenerates the image of the given panel.
	TaskCommandRegeneratePanel TaskCommand = "regenerate_panel"
	// TaskCommandRegeneratePage composes or recomposes the given page.
	TaskCommandRegeneratePage TaskCommand = "regenerate_page"
)

// Task is a generation job handed to the worker through Cloud Tasks.
// It is serialised to JSON and sent as the task payload.
type Task struct {
	Command   TaskCommand `json:"command"`
	JobID     string      `json:"job_id"`
	CreatedAt time.Time   `json:"created_at"`

	// --- shared by compose_comic / regenerate_chapter_script (script generation) ---
	// SourceURL / SourceText are the input of compose_comic (one of them is required; mutually exclusive).
	SourceURL  string `json:"source_url,omitempty"`
	SourceText string `json:"source_text,omitempty"`
	// ScriptMode is the script prompt's mode (a template name under assets/prompts/outline and
	// chapter). Empty means the default mode.
	ScriptMode string `json:"script_mode,omitempty"`
	// StyleMode selects the art style (a template name under assets/prompts/style).
	// Empty means the default mode. The chosen value is recorded in the state and inherited
	// by later image-generation jobs (render_comic and the regenerate ones).
	StyleMode string `json:"style_mode,omitempty"`
	// TextModel is the Gemini text model used for script generation (chapters and chapter scripts).
	// Empty means the head of GEMINI_MODELS on the worker. It pairs with ModelOverride on the image side,
	// and the web face validates it against the GEMINI_MODELS allowlist.
	TextModel string `json:"text_model,omitempty"`
	// ChapterID is the target chapter of regenerate_chapter_script.
	ChapterID string `json:"chapter_id,omitempty"`
	// With StopAfterScript, compose_comic stops after generating the chapters and the script.
	// It is for checking the content before moving on to expensive image generation; the rest is done with render_comic.
	StopAfterScript bool `json:"stop_after_script,omitempty"`
	// With StopAfterPanels, render_comic stops after generating the panels.
	//
	// A page is a composite of the panels, so this lets the panels be checked before moving on.
	// Sending render_comic again to a state with every panel present skips the generated panels
	// and runs only page composition (no dedicated "pages only" command is needed).
	StopAfterPanels bool `json:"stop_after_panels,omitempty"`

	// --- generate_design_sheet ---
	CharacterIDs []string `json:"character_ids,omitempty"`
	AspectRatio  string   `json:"aspect_ratio,omitempty"`
	Layout       string   `json:"layout,omitempty"`
	// ReferenceURLOverride / VisualCuesOverride are one-off overrides applied only when a single
	// character is chosen (they correspond to go-comic-kit's DesignOverride).
	ReferenceURLOverride string   `json:"reference_url_override,omitempty"`
	VisualCuesOverride   []string `json:"visual_cues_override,omitempty"`

	// --- every image generation (compose_comic / render_comic / generate_design_sheet / the regenerate ones) ---
	// ModelOverride replaces the configured image model (the head of IMAGE_MODELS).
	// Empty means the default model.
	//
	// It is used not only for standalone design sheets but also by compose_comic, where it becomes the
	// model for panels and pages. The chosen value is recorded in the state and inherited by later
	// image-generation jobs (render_comic and the regenerate ones).
	ModelOverride string `json:"model_override,omitempty"`

	// --- regenerate_panel ---
	PanelID string `json:"panel_id,omitempty"`

	// --- regenerate_page ---
	Page int `json:"page,omitempty"`

	// --- shared by regenerate_panel / regenerate_page (corresponds to go-comic-kit's GenerateOptions) ---
	// When Seed is nil, the previous generation condition of the target (GenerationRecord.UsedSeed) is reused.
	Seed *int64 `json:"seed,omitempty"`
	// With EditPrompt it becomes edit mode, taking the existing generated image as input.
	EditPrompt string `json:"edit_prompt,omitempty"`
	// PromptOverride replaces the automatically built prompt.
	PromptOverride string `json:"prompt_override,omitempty"`
}

// ValidateSubmission checks that the minimum input is present before a job is enqueued.
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
		// The target is the whole state, so no input besides job_id is needed.
		// chapter_id is optional and narrows it to that chapter (for a chapter that does not exist,
		// go-comic-kit returns ErrNotFound at run time).
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
		if err := validateDesignLayout(t.Layout, len(t.CharacterIDs)); err != nil {
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

// referenceImageURLPattern is the accepted format of a reference image URL (an optional override).
// It must start with http(s):// or gs:// and end with an image extension.
var referenceImageURLPattern = regexp.MustCompile(`(?i)^(https?://|gs://)\S+\.(png|jpe?g|webp|gif)$`)

// validateReferenceURLOverride validates the format of a reference image URL (an optional override).
// Whether it really is an image is finally decided on the worker after downloading (from the MIME
// type go-comic-kit determines from the fetched content), so this is only a light check that rejects
// obviously invalid values early, at form submission. The empty string (unspecified) is allowed.
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

// TaskName returns the Cloud Tasks task name (the deduplication key for enqueues).
// Commands that redo work include the enqueue time in the name, so deduplication applies only to
// "an enqueue of the same job, command and target arriving in the same second", i.e. only duplicates
// that may be merged, such as Cloud Tasks' at-least-once delivery or a caller's retry.
//
// Naming by target alone would silently drop a regeneration that someone resubmits after seeing
// the result ("I don't like it, once more"). Cloud Tasks keeps the names of executed tasks reserved
// for a nominal hour, in practice much longer, and ALREADY_EXISTS during that time is treated as success
// (gcp-kit's EnqueueWithName), so the caller gets 202 and the job status stays queued with
// nobody processing it. Regeneration is repeated until the result satisfies, so not losing a
// submission takes priority over rejecting duplicates.
//
// Job IDs are alphanumerics and hyphens only (checked by ValidateJobID), and ChapterID/PanelID are
// minted by the system as "ch01" / "ch01-p03", so they satisfy the Cloud Tasks task-name
// character constraint (alphanumerics, hyphens, underscores) without further sanitising.
func (t Task) TaskName() string {
	parts := []string{t.JobID, string(t.Command)}
	if target := t.taskTarget(); target != "" {
		parts = append(parts, target)
	}
	if t.repeatable() {
		parts = append(parts, fmt.Sprintf("t%d", t.CreatedAt.UTC().Unix()))
	}
	return strings.Join(parts, "-")
}

// repeatable reports whether a command is expected to be resubmitted to the same target any number of times.
//
// Only compose_comic is not. It mints a new job ID on every enqueue
// (handlers.EnqueueComic), so its name already changes per enqueue, and a deterministic
// name could only deduplicate a resend of the same request. Conversely, any command added
// from now on belongs on this side by default: a duplicate is safer than a lost submission.
func (t Task) repeatable() bool {
	return t.Command != TaskCommandComposeComic
}

func (t Task) taskTarget() string {
	switch t.Command {
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
