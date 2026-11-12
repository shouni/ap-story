package domain

import (
	"strings"
	"testing"

	characterkit "github.com/shouni/go-character-kit/character"
	"github.com/shouni/go-comic-kit/ports"
)

// The layout values are the kit's: "single" is go-comic-kit's single view, and the supplementary
// sheets are named like go-character-kit's sheet kinds, so a generated sheet goes into the
// characters.json `sheets` entry of the same name.
func TestDesignLayoutsMatchTheKits(t *testing.T) {
	t.Parallel()

	if DesignLayoutSingle != ports.DesignLayoutSingleView {
		t.Errorf("DesignLayoutSingle = %q, want go-comic-kit's %q", DesignLayoutSingle, ports.DesignLayoutSingleView)
	}
	kinds := characterkit.SheetKinds()
	for _, layout := range []string{DesignLayoutExpressions, DesignLayoutOutfit, DesignLayoutParts} {
		found := false
		for _, kind := range kinds {
			if kind == layout {
				found = true
			}
		}
		if !found {
			t.Errorf("layout %q is not a go-character-kit sheet kind %v", layout, kinds)
		}
	}
}

func TestDesignSheetLayoutValidation(t *testing.T) {
	t.Parallel()

	task := func(layout string, ids ...string) *Task {
		return &Task{Command: TaskCommandGenerateDesignSheet, JobID: "c-20261112-120000-0123456789ab", CharacterIDs: ids, Layout: layout}
	}
	for _, layout := range DesignLayouts {
		if err := task(layout, "das_akari").ValidateSubmission(); err != nil {
			t.Errorf("layout %q for one character: %v", layout, err)
		}
	}
	if err := task("", "das_akari", "das_hikari").ValidateSubmission(); err != nil {
		t.Errorf("turnaround for two characters: %v", err)
	}
	if err := task("part", "das_akari").ValidateSubmission(); err == nil || !strings.Contains(err.Error(), "unknown layout") {
		t.Errorf("typo'd layout: err = %v, want unknown layout", err)
	}
	if err := task(DesignLayoutExpressions, "das_akari", "das_hikari").ValidateSubmission(); err == nil ||
		!strings.Contains(err.Error(), "one character at a time") {
		t.Errorf("expressions for two characters: err = %v, want one character at a time", err)
	}
}
