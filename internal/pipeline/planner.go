package pipeline

import (
	"fmt"

	"github.com/shouni/ap-story/internal/domain"
)

// Planner decides the step sequence (execution plan) to run for a Task.
// Runner only fetches the plan and runs it in order; the knowledge of which steps make up each
// command is gathered in the Planner.
type Planner interface {
	Plan(task *domain.Task) ([]Step, error)
}

// DefaultPlanner is the production Planner that returns the standard step sequence per command.
type DefaultPlanner struct{}

// Plan returns the standard step sequence for the command. Each case returns that command's
// complete step sequence, so the differences between commands can be seen in one place.
//
// compose_comic saves with SaveStateStep at each break in the process (script → panels → pages),
// and each stage skips what is already done. compose_comic is only enqueued with a newly minted
// job ID, so an existing state means a re-run from a Cloud Tasks redelivery, and starting over
// there would discard saved output and pay the image generation cost twice.
// The regenerate commands all load the target state, update only the relevant part and save it
// back to the same place (go-comic-kit's operations rewrite *MangaState directly and return it,
// which makes regeneration idempotent).
func (DefaultPlanner) Plan(task *domain.Task) ([]Step, error) {
	if task == nil {
		return nil, fmt.Errorf("task is nil")
	}
	switch task.Command {
	case domain.TaskCommandComposeComic:
		// Save at each break in the process. Image generation runs dozens of times in one job, so
		// saving everything at the end would lose all results to a failure midway.
		steps := []Step{
			LoadStateIfExistsStep{},
			OutlineStep{},
			AllChapterScriptsStep{},
			SaveStateStep{},
		}
		if task.StopAfterScript {
			// The workflow of checking the script before moving on to image generation. render_comic does the rest.
			return steps, nil
		}
		return append(steps, renderSteps(true)...), nil
	case domain.TaskCommandRenderComic:
		// Fill in only what is missing from the existing state. Both "continue generating" after checking
		// the script and resuming after a failure or cutoff are this one command.
		steps := []Step{LoadStateStep{}}
		if task.StopAfterPanels {
			// The workflow of checking the panels before moving on to pages. Page composition runs
			// when the same command is sent again (the generated panels are skipped).
			return append(steps, AllPanelsStep{SkipGenerated: true}, SaveStateStep{}), nil
		}
		return append(steps, renderSteps(true)...), nil
	case domain.TaskCommandRegenerateChapterScript:
		return []Step{
			LoadStateStep{},
			SingleChapterScriptStep{},
			SaveStateStep{},
		}, nil
	case domain.TaskCommandGenerateDesignSheet:
		return []Step{
			LoadStateStepOptional{},
			DesignSheetStep{},
			SaveStateStep{},
		}, nil
	case domain.TaskCommandRegeneratePanel:
		return []Step{
			LoadStateStep{},
			SinglePanelStep{},
			SaveStateStep{},
		}, nil
	case domain.TaskCommandRegeneratePage:
		return []Step{
			LoadStateStep{},
			SinglePageStep{},
			SaveStateStep{},
		}, nil
	default:
		return nil, fmt.Errorf("no step plan for command %q", task.Command)
	}
}

// renderSteps returns the step sequence of the image-generation phase (panels → pages).
// When skipGenerated is true, panels and pages already generated are skipped.
func renderSteps(skipGenerated bool) []Step {
	return []Step{
		AllPanelsStep{SkipGenerated: skipGenerated},
		SaveStateStep{},
		AllPagesStep{SkipGenerated: skipGenerated},
		SaveStateStep{},
	}
}
