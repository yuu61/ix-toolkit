package infrastructure

import (
	"math"
	"strconv"
	"strings"
)

// pdfBodyFontSize は本文帯で最も多く使われる文字サイズを返す。
// 表だけのページや章扉に左右されないよう、冊子全体から求める。
func pdfBodyFontSize(pages []pdfPage, body crop) float64 {
	counts := map[int]int{}
	for _, pg := range pages {
		for _, g := range pg.glyphs {
			if g.fontSize > 0 && body.keep(g, pg.width, pg.height) {
				counts[int(math.Round(g.fontSize*100))]++
			}
		}
	}
	size, count := 0, 0
	for s, n := range counts {
		if n > count || n == count && s > size {
			size, count = s, n
		}
	}
	return float64(size) / 100
}

func headingTextKey(s string) string { return strings.Join(strings.Fields(s), "") }

// pdfHeadingLines は番号に見える行から、本文より大きな字で組まれた見出しを選ぶ。
// 字面の高さやフォントの太さではなく、PDF の変換行列を反映した文字サイズを使う。
// FD の本文は 10.56pt、下位見出しは 11.04pt。2% の余裕は丸め誤差を除くため。
func pdfHeadingLines(pg pdfPage, body crop, bodySize float64) map[string]bool {
	out := map[string]bool{}
	var gs []glyph
	for _, g := range pg.glyphs {
		if body.keep(g, pg.width, pg.height) {
			gs = append(gs, g)
		}
	}
	lines := groupLines(gs)
	unit := columnUnit(lines)
	for _, ln := range lines {
		s := renderFlowLine(ln, lineUnit(ln.glyphs, unit))
		if !headingRe.MatchString(s) || tocLeaderRe.MatchString(s) {
			continue
		}
		for _, g := range ln.glyphs {
			if g.r >= '0' && g.r <= '9' {
				if bodySize > 0 && g.fontSize > bodySize*1.02 {
					out[headingTextKey(s)] = true
				}
				break
			}
		}
	}
	return out
}

func isPageHeading(pg Page, text, number string) bool {
	chapter, _, _ := strings.Cut(number, ".")
	n, _ := strconv.Atoi(chapter)
	if pg.chapter > 0 && n != pg.chapter {
		return false
	}
	return pg.headings == nil || pg.headings[headingTextKey(text)]
}
