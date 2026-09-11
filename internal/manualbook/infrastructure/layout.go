package infrastructure

import (
	"math"
	"sort"
	"strings"
)

// 文字と外接矩形から版面のテキストを組み直す。pdftotext の -table / -layout /
// -raw と、-marginl/r/t/b に当たる部分がここにある。
//
// 桁を座標から決める点が肝になる。行ごとの空きをそのまま空白にすると (-layout
// 相当)、罫線で組まれた表の行がずれる。機能説明書 1-7 の諸元表を Xpdf の
// -layout で読むと
//
//	VLAN 設定数    (空)  8  -  1000※1  32  同左
//
// となり、正しい 32/32/32/32/32/1000※1/32/32 と機種の対応が全部ずれた。
// ページ全体で同じ桁幅の格子に載せると (-table 相当)、同じ表が正しく出る。
// 抽出は成功しているように見えるので、この崩れは出力を見ても分からない。

// crop は 4 辺から捨てる幅 (ポイント)。pdftotext の -marginl/r/t/b に当たる。
// 0 の辺は捨てない。
type crop struct {
	left, right, top, bottom float64
}

// keep は文字がこの切り出しに残るかを返す。判定は文字の中心で行う。
// 端に掛かった文字を落とすか残すかで段組みの取りこぼしが増減するため、
// 外接矩形ではなく中心を使う。
func (c crop) keep(g glyph, w, h float64) bool {
	x, y := g.centerX(), g.centerY()
	if c.left > 0 && x < c.left {
		return false
	}
	if c.right > 0 && x > w-c.right {
		return false
	}
	if c.top > 0 && y > h-c.top {
		return false
	}
	if c.bottom > 0 && y < c.bottom {
		return false
	}
	return true
}

// textLine は 1 行分の文字と、その行が占める縦の範囲。
type textLine struct {
	glyphs      []glyph
	center      float64
	top, bottom float64
}

// renderPage はページ 1 枚を文字列にする。
//
// grid が true なら桁を揃える (-table 相当)。false なら空きを 1 個の空白に
// 潰す (-raw 相当)。ヘッダ・フッタ帯は後者で読む。
func renderPage(pg pdfPage, c crop, grid bool) string {
	var gs []glyph
	for _, g := range pg.glyphs {
		if c.keep(g, pg.width, pg.height) {
			gs = append(gs, g)
		}
	}
	if len(gs) == 0 {
		return ""
	}

	lines := groupLines(gs)
	if len(lines) == 0 {
		return ""
	}

	unit := columnUnit(lines)
	originX := math.Inf(1)
	for _, g := range gs {
		if g.left < originX {
			originX = g.left
		}
	}
	spacing := lineSpacing(lines)

	starts := snapStarts(lines, originX, unit)

	var b strings.Builder
	for i, ln := range lines {
		if i > 0 {
			n := blankLines(lines[i-1].center-ln.center, spacing)
			if grid && bulletParagraphBreak(lines, i, unit) {
				// FD の collapseTableBlanks を通しても段落の境界を残す。
				n = max(n, 2)
			}
			for ; n > 0; n-- {
				b.WriteByte('\n')
			}
		}
		u := lineUnit(ln.glyphs, unit)
		if grid {
			b.WriteString(renderGridLine(ln, originX, unit, u, starts[i]))
		} else {
			b.WriteString(renderFlowLine(ln, u))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// bulletParagraphBreak は箇条書き直後の広い行間を、リスト内の行送りで測る。
// CRM は項目名の前後に空きが多く、ページ全体の中央値が本文の行送りより
// 大きい。その中央値だけではリスト後の本文が最後の項目に連結される。
// 本文が箇条書きの左に戻る場合も区切る。本文の字下げが深くなる場合は
// 位置だけでは決めず、行間を使う。
func bulletParagraphBreak(lines []textLine, i int, unit float64) bool {
	isBulletLine := func(j int) bool {
		ln := lines[j]
		return isBullet(renderFlowLine(ln, lineUnit(ln.glyphs, unit)))
	}
	if i < 1 || isBulletLine(i) || !isBulletLine(i-1) {
		return false
	}
	if lines[i].glyphs[0].left < lines[i-1].glyphs[0].left-unit*0.5 {
		return true
	}
	start := i - 1
	for start > 0 && isBulletLine(start-1) {
		start--
	}
	if i-start < 2 {
		return false
	}
	// 行の中心は p/g などの字面で上下する。短いリストの隣接差の中央値では
	// そのずれが残るため、先頭から末尾までの距離を行数で割って均す。
	spacing := (lines[start].center - lines[i-1].center) / float64(i-start-1)
	return blankLines(lines[i-1].center-lines[i].center, spacing) > 0
}

// snapStarts は行頭の桁を揃える。
//
// PDFium が返すのは字面の左端なので、同じ位置から始まる行でも行頭の字が違うと
// 1 桁ゆれる。実測ではコマンドリファレンスの入力形式で、同じ x=284.59 から
// 始まる "chap ..." と "no chap ..." が 3 桁目と 4 桁目に分かれた。字下げの
// 深い行は折り返しの続きとして直前の行に繋がれるので、この 1 桁のゆれが
// "chap challenge-timeout TIME no chap challenge-timeout" という 1 つの
// コマンドを作ってしまう。
//
// 揃えるのは「座標がほぼ同じなのに桁が 1 つずれた行」だけに限る。桁の近い
// 行頭をまとめて寄せると、折り返しの続きを表す 1 桁の字下げまで消えて、
// 逆に別のコマンドが 1 行に繋がる。
func snapStarts(lines []textLine, originX, unit float64) []int {
	seen := map[int]int{} // 半桁に丸めた x -> 割り当てた桁
	out := make([]int, len(lines))
	for i, ln := range lines {
		x := ln.glyphs[0].left
		key := int(math.Round((x - originX) / (unit * 0.5)))
		if col, ok := seen[key]; ok {
			out[i] = col
			continue
		}
		col := int(math.Round((x - originX) / unit))
		// 隣の丸め位置に既に割り当てがあれば、それに合わせる。
		if c, ok := seen[key-1]; ok && abs(c-col) <= 1 {
			col = c
		} else if c, ok := seen[key+1]; ok && abs(c-col) <= 1 {
			col = c
		}
		seen[key] = col
		out[i] = col
	}
	return out
}

// groupLines は文字を行にまとめる。
//
// 中心の近さで寄せてはいけない。外接矩形は字によって上下にずれる。句読点
// (。、) は全角の枠の下に寄り、下に伸びる字 (q u p g y) は枠から出る。中心で
// 測ると同じ行の「。」や「q」が 1 文字だけ下の行に落ち、"dot1x quarantine" が
// "d o t 1 x  a ra n tin e" と "q u" の 2 行に割れた。
//
// 縦の重なりで測れば、小さい字も背の高い字も同じ行に残る。
func groupLines(gs []glyph) []textLine {
	sorted := make([]glyph, len(gs))
	copy(sorted, gs)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].top > sorted[j].top
	})

	var lines []textLine
	for _, g := range sorted {
		placed := false
		for i := len(lines) - 1; i >= 0 && i >= len(lines)-3; i-- {
			lineH := lines[i].top - lines[i].bottom
			overlap := math.Min(lines[i].top, g.top) - math.Max(lines[i].bottom, g.bottom)
			// 字面が平たい字 (_ ー 、) は行の下に外れて重ならないことがある。
			// これを別の行にすると本文から抜け落ちる ("ECHO_REQUEST" が
			// "ECHO REQUEST" になった)。少しの外れは同じ行として許す。
			flat := g.height() < 0.35*lineH && overlap > -0.25*lineH
			if flat || overlap > 0.35*math.Min(lineH, g.height()) {
				lines[i].glyphs = append(lines[i].glyphs, g)
				lines[i].top = math.Max(lines[i].top, g.top)
				lines[i].bottom = math.Min(lines[i].bottom, g.bottom)
				lines[i].center = (lines[i].top + lines[i].bottom) / 2
				placed = true
				break
			}
		}
		if !placed {
			lines = append(lines, textLine{
				glyphs: []glyph{g}, center: g.centerY(), top: g.top, bottom: g.bottom,
			})
		}
	}
	for i := range lines {
		sort.SliceStable(lines[i].glyphs, func(a, b int) bool {
			return lines[i].glyphs[a].left < lines[i].glyphs[b].left
		})
	}
	return lines
}

// columnUnit は 1 桁の幅を返す。半角 1 文字分にあたる。
//
// 文字の外接矩形の幅で測ってはいけない。句読点や "l" のように字面の狭い字が
// 2 割を超えるので、それを 1 桁にすると全角 1 字が 3 桁も 4 桁も食い、
// 「ロ ギ ン グ」と字間が開いた版面になる。
//
// 隣り合う字の送り (左端の差) で測る。全角の送りが分かればその半分が 1 桁で、
// この資料はどのページにも仮名漢字があるので、まずそれを使う。
//
// 送りの下位 2 割ではいけない。字詰めの効いた対や数字の送りが混ざって
// 半角より狭い値になり、全角 1 字が 3 桁に載って字間が開く (実測 unit=3.89、
// 全角の送りは 10.3-11.7 で 2.65-3.0 桁ぶんになっていた)。
func columnUnit(lines []textLine) float64 {
	var full, all []float64
	for _, ln := range lines {
		for i := 1; i < len(ln.glyphs); i++ {
			d := ln.glyphs[i].left - ln.glyphs[i-1].left
			if d <= 0.5 || d > 40 {
				continue
			}
			all = append(all, d)
			if runeCols(ln.glyphs[i-1].r) == 2 && runeCols(ln.glyphs[i].r) == 2 {
				full = append(full, d)
			}
		}
	}
	if len(full) >= 20 {
		sort.Float64s(full)
		return full[len(full)/2] / 2
	}
	if len(all) == 0 {
		return 4.8
	}
	sort.Float64s(all)
	u := all[len(all)/2]
	if u < 1 {
		return 4.8
	}
	return u
}

// lineUnit はその行での半角 1 文字ぶんの送りを返す。
//
// ページ全体の桁幅は本文の字を代表しているので、見出しのように大きい字の行では
// 狭すぎ、語の中が空きと判定される ("ソースア ドレスセレクション")。行の中の
// 全角どうしの送りが分かればそれを使い、足りなければ字の高さから見積もる
// (高さ 1 つでは足りない。"ソ" や "ー" は全角でも字面が低い)。
func lineUnit(gs []glyph, unit float64) float64 {
	var full []float64
	em := 0.0
	for i, g := range gs {
		em = math.Max(em, g.height())
		if i == 0 || runeCols(gs[i-1].r) != 2 || runeCols(g.r) != 2 {
			continue
		}
		if d := g.left - gs[i-1].left; d > 0.5 && d < 40 {
			full = append(full, d)
		}
	}
	if len(full) >= 3 {
		sort.Float64s(full)
		return full[len(full)/2] / 2
	}
	return math.Max(unit, em*0.55)
}

// runeCols は文字が版面で占める桁数を返す。全角は 2 桁。
func runeCols(r rune) int {
	switch {
	case r >= 0x1100 && r <= 0x115f, // ハングル字母
		r >= 0x2e80 && r <= 0xa4cf, // 漢字・仮名・記号
		r >= 0xac00 && r <= 0xd7a3, // ハングル
		r >= 0xf900 && r <= 0xfaff, // 互換漢字
		r >= 0xfe30 && r <= 0xfe6f, // 互換形
		r >= 0xff00 && r <= 0xff60, // 全角英数
		r >= 0xffe0 && r <= 0xffe6:
		return 2
	}
	return 1
}

// lineSpacing は行送りの代表値 (中央値) を返す。空行の判定に使う。
func lineSpacing(lines []textLine) float64 {
	if len(lines) < 2 {
		return 0
	}
	gaps := make([]float64, 0, len(lines)-1)
	for i := 1; i < len(lines); i++ {
		if d := lines[i-1].center - lines[i].center; d > 0 {
			gaps = append(gaps, d)
		}
	}
	if len(gaps) == 0 {
		return 0
	}
	sort.Float64s(gaps)
	return gaps[len(gaps)/2]
}

// blankLines は行と行の空きから、間に入れる空行の数を返す。
//
// 表の行間のように 1 行分空いていれば 1 行、段落の切れ目のように大きく空いて
// いれば 2 行。この差が collapseTableBlanks の前提になる (単独の空行は組版の
// 都合、2 つ以上続く空行は版面にもとからあった段落の切れ目)。
// 行送りぴったりの整数倍で数えてはいけない。版面の空きは 1 行分より狭いことが
// あり、四捨五入すると空行が消える。実測ではコマンドリファレンスの入力形式で
// 本体と no 形式の間が 15.2pt (行送り 10.7pt の 1.43 倍) しかなく、空行が
// 落ちて 2 つのコマンドが 1 つの項目に繋がった。
func blankLines(gap, spacing float64) int {
	if spacing <= 0 {
		return 0
	}
	switch r := gap / spacing; {
	case r >= 2.2:
		return 2
	case r >= 1.35:
		return 1
	default:
		return 0
	}
}

// renderGridLine は 1 行を桁位置に載せる。全角は 2 桁を占める。
//
// 桁に載せるのは字ではなく語。字ごとに桁へ丸めると、送りが桁幅の整数倍から
// ずれるぶんが 1 字ずつ溜まり、語の途中に空白が入ったり (「ロギ ン グ」)、
// 逆に行の後ろへ行くほど桁が右へずれて表の列が合わなくなる。語の頭だけを
// 絶対位置で置き、語の中は詰めて書けば、どちらも起きない。
func renderGridLine(ln textLine, originX, unit, lineU float64, startCol int) string {
	var buf []rune
	cols := 0      // buf が占める桁数 (全角を 2 と数える)
	prevEnd := 0.0 // 直前の語の右端 (ポイント)
	for w, word := range splitWords(ln.glyphs, lineU) {
		switch {
		case w == 0:
			for cols < startCol {
				buf = append(buf, ' ')
				cols++
			}
		case w > 0 && word[0].left-prevEnd <= lineU*1.8:
			// ただの語間。桁に合わせると丸めの余りが空白になって、
			// 入力形式の "TRIGGER METHOD1" が "TRIGGER   METHOD1" になる。
			buf = append(buf, ' ')
			cols++
		default:
			col := int(math.Round((word[0].left - originX) / unit))
			if w > 0 && col <= cols {
				col = cols + 1 // 空きは必ず 1 桁以上に見せる
			}
			for cols < col {
				buf = append(buf, ' ')
				cols++
			}
		}
		for _, g := range word {
			buf = append(buf, g.r)
			cols += runeCols(g.r)
		}
		prevEnd = word[len(word)-1].right
	}
	return strings.TrimRight(string(buf), " ")
}

// splitWords は 1 行の文字を語に割る。PDF が持つ空白と、版面の空きの両方で割る。
//
// 空きは外接矩形の隙間ではなく送りで測る。字面の狭い字 ("1" "." "i") は矩形が
// 送りよりずっと狭く、隙間で測ると語の中で割れる ("1 0 . 0 . 0 .254")。
func splitWords(gs []glyph, unit float64) [][]glyph {
	var words [][]glyph
	var cur []glyph
	for i, g := range gs {
		split := i == 0 || g.spaceBefore
		if i > 0 && !split {
			// 送りの目安は「桁幅か、その字の字面の広いほう」。桁幅だけで
			// 測ると "G" のような広い字の後ろが毎回割れる ("G igaEthernet")。
			//
			// 閾値は半角 1 桁ぶんに近く取る。PDF が持つ空白は上の spaceBefore で
			// 拾えているので、ここで見つけたいのは表の列のような広い空きだけ。
			// 狭く取ると字面の寄り (「・」は全角の枠の中央に置かれる) を空きと
			// 読んで、項目名が「ローカル ・プリファレンス」と割れる。
			prev := gs[i-1]
			expect := math.Max(float64(runeCols(prev.r))*unit, prev.width())
			split = (g.left-prev.left)-expect > unit*0.9

			// 約物は字面が全角の枠のどこにあるか分からない。「（」は右寄り、
			// 「。」は左下に寄る。座標の差を空きと読むと「DNS サーバ （IPv4）」
			// のように語の中で割れるので、約物が絡む隙間は PDF が持つ空白
			// (spaceBefore) だけを信じる。
			if split && (isCJKPunct(prev.r) || isCJKPunct(g.r)) {
				split = false
			}
		}
		if split && len(cur) > 0 {
			words = append(words, cur)
			cur = nil
		}
		cur = append(cur, g)
	}
	if len(cur) > 0 {
		words = append(words, cur)
	}
	return words
}

// renderFlowLine は 1 行を、空きを 1 個の空白に潰して返す。
func renderFlowLine(ln textLine, unit float64) string {
	var buf []rune
	for w, word := range splitWords(ln.glyphs, unit) {
		if w > 0 {
			buf = append(buf, ' ')
		}
		for _, g := range word {
			buf = append(buf, g.r)
		}
	}
	return strings.TrimRight(string(buf), " ")
}

// isCJKPunct は和文の約物 (句読点・括弧・中点) かを返す。
func isCJKPunct(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x303f, // 、。「」・など
		r >= 0xff01 && r <= 0xff20, // 全角の ！ から ＠ まで (（ ） ： を含む)
		r >= 0xff3b && r <= 0xff40,
		r >= 0xff5b && r <= 0xff65:
		return true
	}
	return false
}
