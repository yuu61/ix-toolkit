package infrastructure

import (
	"math"
	"sort"
	"strings"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

const ruleTolerance = 1.0 // ラスタ化で生じる罫線の太さ・接点のずれ (pt)

type pdfTable struct {
	left, right, top, bottom float64
	rows                     [][]string
	columns                  []float64
	headerPage               int
	headerRows               int
	mergedRows               []bool
}

func (t pdfTable) contains(g glyph) bool {
	return g.centerX() >= t.left && g.centerX() <= t.right && g.centerY() >= t.bottom && g.centerY() <= t.top
}

// mergeRules は太い線の複数画素と、同じ罫線の途切れをまとめる。
func mergeRules(rules []pdfRule) []pdfRule {
	sorted := append([]pdfRule(nil), rules...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].vertical != sorted[j].vertical {
			return !sorted[i].vertical
		}
		return sorted[i].pos < sorted[j].pos
	})
	var out []pdfRule
	for i := 0; i < len(sorted); {
		j := i + 1
		for j < len(sorted) && sorted[j].vertical == sorted[i].vertical && sorted[j].pos-sorted[i].pos <= ruleTolerance {
			j++
		}
		pos := (sorted[i].pos + sorted[j-1].pos) / 2
		group := sorted[i:j]
		sort.Slice(group, func(a, b int) bool { return group[a].lo < group[b].lo })
		cur := group[0]
		cur.pos = pos
		for _, r := range group[1:] {
			if r.lo <= cur.hi+ruleTolerance {
				cur.hi = math.Max(cur.hi, r.hi)
			} else {
				out = append(out, cur)
				cur = r
				cur.pos = pos
			}
		}
		out = append(out, cur)
		i = j
	}
	return out
}

type cellSets []int

func newCellSets(n int) cellSets {
	s := make(cellSets, n)
	for i := range s {
		s[i] = i
	}
	return s
}
func (s cellSets) root(i int) int {
	for s[i] != i {
		s[i] = s[s[i]]
		i = s[i]
	}
	return i
}
func (s cellSets) join(a, b int) { s[s.root(a)] = s.root(b) }

func findPDFTables(pg pdfPage, rules []pdfRule) []pdfTable {
	rules = mergeRules(rules)
	sets := newCellSets(len(rules))
	for i, a := range rules {
		for j := i + 1; j < len(rules); j++ {
			b := rules[j]
			if a.vertical == b.vertical {
				continue
			}
			if a.pos >= b.lo-ruleTolerance && a.pos <= b.hi+ruleTolerance && b.pos >= a.lo-ruleTolerance && b.pos <= a.hi+ruleTolerance {
				sets.join(i, j)
			}
		}
	}
	groups := map[int][]pdfRule{}
	var order []int
	for i, r := range rules {
		key := sets.root(i)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], r)
	}
	var tables []pdfTable
	for _, key := range order {
		if table, ok := tableFromRules(pg, groups[key]); ok {
			tables = append(tables, table)
		}
	}
	// 入れ子の枠などが複数の表候補になった場合、文字を重複させず元の版面へ戻す。
	var disjoint []pdfTable
	for i, a := range tables {
		overlap := false
		for j, b := range tables {
			if i != j && math.Min(a.right, b.right) > math.Max(a.left, b.left) && math.Min(a.top, b.top) > math.Max(a.bottom, b.bottom) {
				overlap = true
				break
			}
		}
		if !overlap {
			disjoint = append(disjoint, a)
		}
	}
	tables = disjoint
	sort.Slice(tables, func(i, j int) bool {
		if tables[i].top == tables[j].top {
			return tables[i].left < tables[j].left
		}
		return tables[i].top > tables[j].top
	})
	return tables
}

// ruleCoverage は辺が全て罫線で覆われるかを返す (1=全て、0=無し、-1=一部)。
// 部分的な辺を「結合セル」と推測しない。
func ruleCoverage(rules []pdfRule, vertical bool, pos, lo, hi float64) int {
	partial := false
	for _, r := range rules {
		if r.vertical != vertical || math.Abs(r.pos-pos) > ruleTolerance {
			continue
		}
		if r.lo <= lo+ruleTolerance && r.hi >= hi-ruleTolerance {
			return 1
		}
		if math.Min(r.hi, hi)-math.Max(r.lo, lo) > ruleTolerance {
			partial = true
		}
	}
	if partial {
		return -1
	}
	return 0
}

func tableFromRules(pg pdfPage, rules []pdfRule) (pdfTable, bool) {
	var xs, ys []float64
	for _, r := range rules {
		if r.vertical {
			xs = append(xs, r.pos)
		} else {
			ys = append(ys, r.pos)
		}
	}
	unique := func(v []float64) []float64 {
		sort.Float64s(v)
		var out []float64
		for _, x := range v {
			if len(out) == 0 || x-out[len(out)-1] > ruleTolerance {
				out = append(out, x)
			}
		}
		return out
	}
	xs, ys = unique(xs), unique(ys)
	if len(xs) < 3 || len(ys) < 3 {
		return pdfTable{}, false
	}
	// 上から下へ行を並べる。
	for i, j := 0, len(ys)-1; i < j; i, j = i+1, j-1 {
		ys[i], ys[j] = ys[j], ys[i]
	}
	nr, nc := len(ys)-1, len(xs)-1
	if nr*nc > 20000 {
		return pdfTable{}, false
	}
	table := pdfTable{left: xs[0], right: xs[nc], top: ys[0], bottom: ys[nr]}
	table.columns = xs
	if table.right-table.left < 24 || table.top-table.bottom < 16 {
		return pdfTable{}, false
	}
	for _, x := range []float64{xs[0], xs[nc]} {
		if ruleCoverage(rules, true, x, ys[nr], ys[0]) != 1 {
			return pdfTable{}, false
		}
	}
	for _, y := range []float64{ys[0], ys[nr]} {
		if ruleCoverage(rules, false, y, xs[0], xs[nc]) != 1 {
			return pdfTable{}, false
		}
	}
	sets := newCellSets(nr * nc)
	for r := 0; r < nr; r++ {
		for c := 0; c < nc; c++ {
			if c+1 < nc {
				switch ruleCoverage(rules, true, xs[c+1], ys[r+1], ys[r]) {
				case -1:
					return pdfTable{}, false
				case 0:
					sets.join(r*nc+c, r*nc+c+1)
				}
			}
			if r+1 < nr {
				switch ruleCoverage(rules, false, ys[r+1], xs[c], xs[c+1]) {
				case -1:
					return pdfTable{}, false
				case 0:
					sets.join(r*nc+c, (r+1)*nc+c)
				}
			}
		}
	}
	// 結合後の領域は長方形で、四辺が閉じている場合だけセルとして採る。
	type span struct{ r0, r1, c0, c1, n int }
	spans := map[int]span{}
	for r := 0; r < nr; r++ {
		for c := 0; c < nc; c++ {
			k := sets.root(r*nc + c)
			s, ok := spans[k]
			if !ok {
				s = span{r, r, c, c, 0}
			}
			s.r0 = min(s.r0, r)
			s.r1 = max(s.r1, r)
			s.c0 = min(s.c0, c)
			s.c1 = max(s.c1, c)
			s.n++
			spans[k] = s
		}
	}
	if len(spans) < 4 {
		return pdfTable{}, false
	}
	for _, s := range spans {
		if (s.r1-s.r0+1)*(s.c1-s.c0+1) != s.n {
			return pdfTable{}, false
		}
		for _, x := range []float64{xs[s.c0], xs[s.c1+1]} {
			if ruleCoverage(rules, true, x, ys[s.r1+1], ys[s.r0]) != 1 {
				return pdfTable{}, false
			}
		}
		for _, y := range []float64{ys[s.r0], ys[s.r1+1]} {
			if ruleCoverage(rules, false, y, xs[s.c0], xs[s.c1+1]) != 1 {
				return pdfTable{}, false
			}
		}
	}
	cellGlyphs := map[int][]glyph{}
	for _, g := range pg.glyphs {
		if !table.contains(g) {
			continue
		}
		c := sort.Search(len(xs), func(i int) bool { return xs[i] > g.centerX() }) - 1
		r := sort.Search(len(ys), func(i int) bool { return ys[i] < g.centerY() }) - 1
		if c < 0 || c >= nc || r < 0 || r >= nr {
			return pdfTable{}, false
		}
		k := sets.root(r*nc + c)
		s := spans[k]
		// 境界を跨ぐ文字があれば、図の線や下線をセル境界と誤認した可能性がある。
		if g.left < xs[s.c0]-ruleTolerance || g.right > xs[s.c1+1]+ruleTolerance || g.top > ys[s.r0]+ruleTolerance || g.bottom < ys[s.r1+1]-ruleTolerance {
			return pdfTable{}, false
		}
		cellGlyphs[k] = append(cellGlyphs[k], g)
	}
	if len(cellGlyphs) < 4 || len(cellGlyphs)*2 < len(spans) {
		return pdfTable{}, false
	}
	texts := map[int]string{}
	for k, gs := range cellGlyphs {
		cell := pg
		cell.glyphs = gs
		var lines []string
		for _, line := range strings.Split(renderPage(cell, crop{}, false), "\n") {
			if t := strings.TrimSpace(line); t != "" {
				lines = append(lines, t)
			}
		}
		texts[k] = strings.Join(lines, "\n")
	}
	table.rows = make([][]string, nr)
	for r := 0; r < nr; r++ {
		table.rows[r] = make([]string, nc)
		for c := 0; c < nc; c++ {
			table.rows[r][c] = texts[sets.root(r*nc+c)]
		}
	}
	// 上段の結合セルと、その下位の列名を一緒に保持する。
	// 全幅の結合セルは表題なので、その次のデータ行までは含めない。
	table.headerRows = 1
	table.mergedRows = make([]bool, nr)
	for _, s := range spans {
		if s.n > 1 {
			for r := s.r0; r <= s.r1; r++ {
				table.mergedRows[r] = true
			}
		}
	}
	for r := 0; r < table.headerRows && r < nr; r++ {
		for _, s := range spans {
			if s.r0 != r {
				continue
			}
			table.headerRows = max(table.headerRows, s.r1+1)
			if s.c1 > s.c0 && s.c1-s.c0+1 < nc {
				table.headerRows = max(table.headerRows, s.r1+2)
			}
		}
	}
	return table, true
}

// sectionTableLines は表を本文から取り除き、その位置に表ブロックを置く。
// 行の空白幅や文末記号で表を分断せず、元ページの位置を保持する。
func sectionTableLines(pg pdfPage, tables []pdfTable, body crop, page int) ([]string, map[int]domain.Block) {
	blocks := map[int]domain.Block{}
	var lines []string
	remaining := pg
	remaining.glyphs = nil
	for _, g := range pg.glyphs {
		if !body.keep(g, pg.width, pg.height) {
			continue
		}
		inside := false
		for _, table := range tables {
			if table.contains(g) {
				inside = true
				break
			}
		}
		if !inside {
			remaining.glyphs = append(remaining.glyphs, g)
		}
	}
	for _, table := range tables {
		above, below := remaining, remaining
		above.glyphs = nil
		below.glyphs = nil
		for _, g := range remaining.glyphs {
			if g.centerY() > table.top {
				above.glyphs = append(above.glyphs, g)
			} else {
				below.glyphs = append(below.glyphs, g)
			}
		}
		lines = append(lines, collapseTableBlanks(splitLines(renderPage(above, crop{}, true)))...)
		block := domain.Block{Kind: domain.BlockTable, Ref: domain.Ref{Page: page}, Rows: table.rows}
		if table.headerPage > 0 && table.headerPage != page {
			block.HeaderRef = domain.Ref{Page: table.headerPage}
		}
		blocks[len(lines)] = block
		lines = append(lines, "")
		remaining = below
	}
	lines = append(lines, collapseTableBlanks(splitLines(renderPage(remaining, crop{}, true)))...)
	return lines, blocks
}

// continuePDFTable は前ページ末から続く、見出しが省かれた表に列名を補う。
// 同じ節・同じ列境界で、間に本文が無く、データの続きだと確認できる場合だけ。
// 曖昧な表は変更しない。補った列名の全段と出典も残す。
func continuePDFTable(previous pdfTable, current *pdfTable, prevPage, page pdfPage, body crop, number int) {
	current.headerPage = number
	if len(previous.rows) == 0 || len(current.rows) == 0 || len(previous.columns) != len(current.columns) {
		return
	}
	for i, x := range previous.columns {
		// 見開きの左右ページで余白が変わるので、表の左端からの相対位置を比べる。
		if math.Abs((x-previous.left)-(current.columns[i]-current.left)) > ruleTolerance {
			return
		}
	}
	for _, g := range prevPage.glyphs {
		if body.keep(g, prevPage.width, prevPage.height) && g.centerY() < previous.bottom {
			return
		}
	}
	for _, g := range page.glyphs {
		if body.keep(g, page.width, page.height) && g.centerY() > current.top {
			return
		}
	}
	if !tableDataContinues(previous, *current) {
		return
	}
	var headers [][]string
	for _, row := range previous.rows[:previous.headerRows] {
		headers = append(headers, append([]string(nil), row...))
	}
	current.rows = append(headers, current.rows...)
	current.headerRows = previous.headerRows
	current.mergedRows = append(append([]bool(nil), previous.mergedRows[:current.headerRows]...), current.mergedRows...)
	current.headerPage = previous.headerPage
}

// tableDataContinues は新しい表題・列名をデータの続きと誤認しないための確認。
// 先頭に結合セルがあれば独自の見出しの可能性があるので継承しない。
// また、前表のデータで反復する値が、現表の先頭数行にも同じ列で続き、
// 列見出しとは異なることを要求する (○/×、RO など)。自由文だけなら推測しない。
func tableDataContinues(previous, current pdfTable) bool {
	h := previous.headerRows
	if h < 1 || h >= len(previous.rows) || len(current.rows) == 0 || len(current.mergedRows) == 0 || current.mergedRows[0] {
		return false
	}
	key := func(s string) string { return strings.Join(strings.Fields(s), "") }
	for c := range previous.rows[0] {
		header := map[string]bool{}
		for _, row := range previous.rows[:h] {
			header[key(row[c])] = true
		}
		values := map[string]int{}
		for _, row := range previous.rows[h:] {
			values[key(row[c])]++
		}
		matches := true
		for _, row := range current.rows[:min(3, len(current.rows))] {
			v := key(row[c])
			if v == "" || header[v] || values[v] < 2 {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}
