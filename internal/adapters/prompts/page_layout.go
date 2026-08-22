package prompts

import (
	"cmp"
	"math"
	"slices"
	"strings"

	"github.com/shouni/go-comic-kit/comic"
)

// 段内での位置です。右開きなので、番号の小さいコマが右に来ます。
const (
	columnFull  = "FULL"
	columnRight = "RIGHT"
	columnLeft  = "LEFT"
)

const (
	// fullWidthWeight は全幅の見せゴマに回す重みの下限です。演出のないコマは 1.0。
	fullWidthWeight = 1.5
	// widthTiltWeight は段内を 60:40 に振る重みの差です。これ未満なら半々。
	widthTiltWeight = 0.5
	// 段の高さは均等割りの何倍まで振れてよいか。極端な帯にすると絵が入りません。
	minTierShare = 0.7
	maxTierShare = 1.7
	// companionShare は、同じ段に並ぶ相方のコマが段の高さへ寄与する割合です。
	// 高い方だけで決めると、2コマ入る段が1コマの段と同じ高さに潰れます。
	companionShare = 0.3
)

// panelPlacement は1コマの割り付けです。Tier は1始まりの段番号。
type panelPlacement struct {
	Tier   int
	Tiers  int
	Column string
	Width  int // 段内での幅（%）
	Height int // ページ高さに対する、その段の高さ（%）
}

// tierView は段ごとのまとめです。Panels はコマの位置（0始まり）を右から左の順で持ちます。
type tierView struct {
	Number int
	Height int
	Panels []int
}

// planPageLayout はコマの内容からページの段組みを決めます。
//
// 引きの画・叫び・場面転換・吹き出しの多いコマほど広い面積を要求し、それを段に詰めます。
// 一律の2列グリッドだと、章頭に必ず来る wide のコマが半幅に潰れ、間を取るための
// 無言の寄りが決めゴマと同じ大きさで並びます。
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

// groupTiers は割り付けを段ごとにまとめ直します（配置マップの出力用）。
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

// columnOf は段内の位置から左右を決めます。1コマだけの段は全幅です。
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

// panelWeight はコマが欲しがる面積です。演出のないコマを 1.0 として増減します。
func panelWeight(panel, prev *comic.Panel, isLast bool) float64 {
	weight := 1.0
	lines := countSpokenLines(panel)

	switch {
	case isWideShot(panel.Shot):
		weight += 1.2
	case isCloseShot(panel.Shot) && lines == 0:
		weight -= 0.4 // 無言の寄りは間を作るコマなので、小さくてよい
	}
	if hasImpactLine(panel) {
		weight += 0.6
	}
	if lines > 1 {
		weight += 0.3 * float64(lines-1) // 吹き出しが増えるほど絵を隠す。その分の幅が要る
	}
	if prev != nil && panel.Setting != "" && !strings.EqualFold(panel.Setting, prev.Setting) {
		weight += 0.4 // 場面転換。どこへ移ったのかを見せる広さが要る
	}
	if isLast {
		weight += 0.5 // ページ最後は次への引き
	}
	return weight
}

// isWideShot / isCloseShot は台本の shot（close-up / medium / wide / bird's-eye）を
// 表記ゆれごと拾います。台本は自由記述もできるため、前方一致では取りこぼします。
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

// hasImpactLine は叫び・効果音を含むコマかを返します。
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

// chooseFullWidth は全幅にするコマを重み順に選びます。
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
	// 半幅が奇数だと余った1コマの段が結局全幅になり、段だけが増える。数を揃えておく。
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

// maxFullWidth は1ページに置く全幅コマの上限です。増やすほどページが横帯の積み重ねになります。
func maxFullWidth(n int) int {
	if n == 3 {
		return 1
	}
	return 2
}

// packTiers は並び順を保ったままコマを段へ詰めます。全幅コマは単独の段になります。
func packTiers(full []bool) [][]int {
	var tiers [][]int
	for i := 0; i < len(full); {
		if !full[i] && i+1 < len(full) && !full[i+1] {
			tiers = append(tiers, []int{i, i + 1})
			i += 2
			continue
		}
		tiers = append(tiers, []int{i}) // 相方のいない半幅はそのまま全幅に上げる
		i++
	}
	return tiers
}

// tierWidths は段内の幅（%）を返します。重みに差があるときだけ 60:40 に振ります。
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

// tierHeights は段の高さ（%）を重みから配分します。合計は必ず 100 です。
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

	// 均等割りからの振れ幅を抑えてから正規化する。
	even := 1.0 / float64(count)
	sum := 0.0
	for i := range shares {
		shares[i] = math.Min(math.Max(shares[i]/total, even*minTierShare), even*maxTierShare)
		sum += shares[i]
	}

	return distributePercent(shares, sum, count)
}

// distributePercent は取り分を刻み単位の百分率へ丸めます。端数は小数部の大きい段から配り、
// 合計を必ず 100 にします（丸め誤差でページの高さが余ると、そこが空白の帯になります）。
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
