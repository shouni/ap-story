package domain

import (
	"fmt"
	"slices"
)

// Design sheet layouts (Task.Layout for generate_design_sheet).
//
// The turnaround and the single pose are identity anchors: full body only, because insets and
// detached parts on an anchor made the model split or fuse figures. The other three are the
// supplementary sheets go-character-kit keeps by kind (`sheets` in characters.json), so their
// values are the kit's kind names: one sheet each, beside the anchor rather than on it.
const (
	DesignLayoutTurnaround  = ""            // front, side and back, full body (the default)
	DesignLayoutSingle      = "single"      // one front-facing full-body pose (ports.DesignLayoutSingleView)
	DesignLayoutExpressions = "expressions" // the same face in several moods, head and shoulders
	DesignLayoutOutfit      = "outfit"      // the costume, front and back, full body
	DesignLayoutParts       = "parts"       // accessories and props laid out singly, no person
)

// DesignLayouts lists every accepted layout, in the order the form offers them.
var DesignLayouts = []string{
	DesignLayoutTurnaround, DesignLayoutSingle, DesignLayoutExpressions, DesignLayoutOutfit, DesignLayoutParts,
}

// IsSupplementaryDesignLayout reports whether the layout is one of the supplementary sheets
// (expressions, outfit, parts) rather than an identity anchor.
func IsSupplementaryDesignLayout(layout string) bool {
	return layout == DesignLayoutExpressions || layout == DesignLayoutOutfit || layout == DesignLayoutParts
}

// validateDesignLayout refuses an unknown layout, and a supplementary sheet for more than one
// character. An unknown value used to fall through to the turnaround silently, so a typo cost a
// generation of the wrong sheet. Expressions or parts for several characters at once would put
// their faces or accessories side by side, which is how features get mixed between characters.
func validateDesignLayout(layout string, characters int) error {
	if !slices.Contains(DesignLayouts, layout) {
		return fmt.Errorf("unknown layout %q (use one of: turnaround (empty), single, expressions, outfit, parts)", layout)
	}
	if IsSupplementaryDesignLayout(layout) && characters != 1 {
		return fmt.Errorf("layout %q is drawn for one character at a time (got %d)", layout, characters)
	}
	return nil
}
