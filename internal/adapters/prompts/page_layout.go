package prompts

import (
	"cmp"
	"math"
	"slices"
	"strings"

	"github.com/shouni/go-comic-kit/comic"
)

// The position within a tier. Pages read right to left, so lower-numbered panels go on the right.
const (
	columnFull  = "FULL"
	columnRight = "RIGHT"
	columnLeft  = "LEFT"
)

const (
	// fullWidthWeight is the minimum weight for a full-width feature panel. A panel with no staging is 1.0.
	fullWidthWeight = 1.5
	// widthTiltWeight is the weight difference that tilts a tier to 60:40. Below it, the split is even.
	widthTiltWeight = 0.5
	// How many times the even share a tier's height may swing to. An extreme strip leaves no room for the art.
	minTierShare = 0.7
	maxTierShare = 1.7
	// companionShare is how much the partner panel in the same tier contributes to the tier's height.
	// Deciding by the taller one alone squashes a two-panel tier to the height of a one-panel tier.
	companionShare = 0.3
)

// panelPlacement is the placement of one panel. Tier is the 1-based tier number.
type panelPlacement struct {
	Tier   int
	Tiers  int
	Column string
	Width  int // width within the tier (%)
	Height int // the tier's height relative to the page height (%)
}

// tierView groups by tier. Panels holds the panels' positions (0-based) from right to left.
type tierView struct {
	Number int
	Height int
	Panels []int
}

// planPageLayout decides the page's tier layout from the panels' content.
//
// Long shots, shouts, scene changes and panels with many speech bubbles ask for more area, and are packed into tiers.
// With a uniform two-column grid, the wide panel that always opens a chapter is squashed to half width,
// and the silent close-ups meant to make a pause sit at the same size as the key panels.
func planPageLayout(panels []comic.Panel) []panelPlacement {
	n := len(panels)
	switch n {
	case 0:
		return nil
	case 1:
		return []panelPlacement{{Tier: 1, Tiers: 1, Column: columnFull, Width: 100, Height: 100}}
	}

	weights := make([]float64, n)
	for i := range panels {
		var prev *comic.Panel
		if i > 0 {
			prev = &panels[i-1]
		}
		weights[i] = panelWeight(&panels[i], prev, i == n-1)
	}

	tiers := packTiers(chooseFullWidth(weights))
	heights := tierHeights(tiers, weights)

	out := make([]panelPlacement, n)
	for t, tier := range tiers {
		widths := tierWidths(tier, weights)
		for pos, idx := range tier {
			out[idx] = panelPlacement{
				Tier:   t + 1,
				Tiers:  len(tiers),
				Column: columnOf(pos, len(tier)),
				Width:  widths[pos],
				Height: heights[t],
			}
		}
	}
	return out
}

// groupTiers regroups the placements by tier (for the placement-map output).
func groupTiers(layout []panelPlacement) []tierView {
	if len(layout) == 0 {
		return nil
	}
	views := make([]tierView, layout[0].Tiers)
	for i, p := range layout {
		v := &views[p.Tier-1]
		v.Number, v.Height = p.Tier, p.Height
		v.Panels = append(v.Panels, i)
	}
	return views
}

// columnOf decides left or right from the position within the tier. A tier with one panel is full width.
func columnOf(pos, inTier int) string {
	switch {
	case inTier == 1:
		return columnFull
	case pos == 0:
		return columnRight
	default:
		return columnLeft
	}
}

// panelWeight is the area a panel wants. A panel with no staging is 1.0, adjusted up or down from there.
func panelWeight(panel, prev *comic.Panel, isLast bool) float64 {
	weight := 1.0
	lines := countSpokenLines(panel)

	switch {
	case isWideShot(panel.Shot):
		weight += 1.2
	case isCloseShot(panel.Shot) && lines == 0:
		weight -= 0.4 // a silent close-up makes a pause, so it can be small
	}
	if hasImpactLine(panel) {
		weight += 0.6
	}
	if lines > 1 {
		weight += 0.3 * float64(lines-1) // more speech bubbles hide more of the art and need the width
	}
	if prev != nil && panel.Setting != "" && !strings.EqualFold(panel.Setting, prev.Setting) {
		weight += 0.4 // a scene change needs room to show where it moved to
	}
	if isLast {
		weight += 0.5 // the last panel on a page pulls into the next
	}
	return weight
}

// isWideShot / isCloseShot pick up the script's shot (close-up / medium / wide / bird's-eye)
// including spelling variants. The script allows free text, so a prefix match would miss some.
func isWideShot(shot string) bool {
	return containsAny(shot, "wide", "bird", "establish", "long shot", "aerial", "俯瞰", "引き")
}

func isCloseShot(shot string) bool {
	return containsAny(shot, "close", "bust", "アップ", "寄り")
}

func containsAny(s string, keywords ...string) bool {
	lower := strings.ToLower(s)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// hasImpactLine reports whether the panel contains a shout or a sound effect.
func hasImpactLine(panel *comic.Panel) bool {
	for _, line := range panel.Dialogues {
		if strings.TrimSpace(line.Text) == "" {
			continue
		}
		if line.Kind == comic.DialogueKindShout || line.Kind == comic.DialogueKindSFX {
			return true
		}
	}
	return false
}

// chooseFullWidth picks the full-width panels in order of weight.
func chooseFullWidth(weights []float64) []bool {
	n := len(weights)
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(weights[b], weights[a]) })

	limit := maxFullWidth(n)
	count := 0
	for _, i := range order {
		if count >= limit || weights[i] < fullWidthWeight {
			break
		}
		count++
	}
	// With an odd number of half-width panels, the leftover one's tier ends up full width anyway and only adds a tier. Keep the count even.
	if (n-count)%2 == 1 {
		if count < limit {
			count++
		} else {
			count--
		}
	}

	full := make([]bool, n)
	for _, i := range order[:count] {
		full[i] = true
	}
	return full
}

// maxFullWidth caps the full-width panels on one page. The more there are, the more the page becomes a stack of horizontal strips.
func maxFullWidth(n int) int {
	if n == 3 {
		return 1
	}
	return 2
}

// packTiers packs the panels into tiers, keeping their order. A full-width panel gets a tier of its own.
func packTiers(full []bool) [][]int {
	var tiers [][]int
	for i := 0; i < len(full); {
		if !full[i] && i+1 < len(full) && !full[i+1] {
			tiers = append(tiers, []int{i, i + 1})
			i += 2
			continue
		}
		tiers = append(tiers, []int{i}) // a half-width panel with no partner is promoted to full width
		i++
	}
	return tiers
}

// tierWidths returns the widths (%) within a tier. It tilts to 60:40 only when the weights differ.
func tierWidths(tier []int, weights []float64) []int {
	if len(tier) == 1 {
		return []int{100}
	}
	if len(tier) != 2 {
		widths := make([]int, len(tier))
		for i := range widths {
			widths[i] = 100 / len(tier)
		}
		return widths
	}
	switch diff := weights[tier[0]] - weights[tier[1]]; {
	case diff >= widthTiltWeight:
		return []int{60, 40}
	case -diff >= widthTiltWeight:
		return []int{40, 60}
	default:
		return []int{50, 50}
	}
}

// tierHeights distributes the tiers' heights (%) by weight. The total is always 100.
func tierHeights(tiers [][]int, weights []float64) []int {
	count := len(tiers)
	if count == 1 {
		return []int{100}
	}

	shares := make([]float64, count)
	total := 0.0
	for i, tier := range tiers {
		sum, top := 0.0, 0.0
		for _, idx := range tier {
			sum += weights[idx]
			top = math.Max(top, weights[idx])
		}
		shares[i] = top + companionShare*(sum-top)
		total += shares[i]
	}

	// Limit the swing from the even share, then normalise.
	even := 1.0 / float64(count)
	sum := 0.0
	for i := range shares {
		shares[i] = math.Min(math.Max(shares[i]/total, even*minTierShare), even*maxTierShare)
		sum += shares[i]
	}

	return distributePercent(shares, sum, count)
}

// distributePercent rounds the shares to percentages in steps. The remainder goes to the tiers with the
// largest fractional parts, so the total is always 100 (if rounding left page height over, it would become a blank strip).
func distributePercent(shares []float64, sum float64, count int) []int {
	step := 5
	if count > 20 {
		step = 1
	}
	units := 100 / step

	pct := make([]int, count)
	frac := make([]float64, count)
	assigned := 0
	for i, share := range shares {
		exact := share / sum * float64(units)
		pct[i] = int(exact)
		frac[i] = exact - float64(pct[i])
		assigned += pct[i]
	}

	order := make([]int, count)
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(frac[b], frac[a]) })
	for i := 0; assigned < units; i++ {
		pct[order[i%count]]++
		assigned++
	}

	for i := range pct {
		pct[i] *= step
	}
	return pct
}
