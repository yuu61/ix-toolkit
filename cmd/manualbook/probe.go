package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
)

// probe サブコマンド: PDF を試し読みして Profile を自動較正する。
//
// 較正は「領域を削って、残る文字数を数える」だけで行う。判定はどれも目視では
// なく数値の不変条件で行う。
//
// ページ寸法だけは数えずにエンジンから直接もらう。pdftotext を叩いていた頃は
// マージンを二分探索して「全部消える幅」と「1 文字も欠けない幅」の和から寸法を
// 復元していたが、ライブラリならページの寸法そのものが返ってくる。

// sampleRange は較正に使う連続ページ範囲。
type sampleRange struct{ lo, hi int }

func runProbe(args []string) {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	out := fs.String("out", "profile.json", "出力するプロファイル JSON")
	name := fs.String("name", "", "プロファイル名 (デフォルト: PDF のファイル名)")
	windows := fs.Int("windows", 3, "較正に使うサンプル窓の数")
	winSize := fs.Int("window-size", 8, "サンプル窓 1 つあたりのページ数")
	pos := parseFlags(fs, args)

	if len(pos) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: manualbook probe <pdf> [-out profile.json]")
		os.Exit(1)
	}
	pdf := pos[0]

	// --- 1. テキスト層の有無 ---
	glyphs, err := hasTextLayer(pdf, 20)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("テキスト層: 先頭 20 ページで %d 文字\n", glyphs)
	if glyphs < 200 {
		fmt.Fprintln(os.Stderr, "\n  ⚠ テキスト層がほとんどありません（紙スキャン由来の画像 PDF の可能性）。")
		fmt.Fprintln(os.Stderr, "    md サブコマンドは使えません。画像化 → manualbook scan で分割 → OCR の経路が必要です。")
		os.Exit(2)
	}

	all, err := extractPages(pdf, crop{}, false)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("ページ数: %d\n", len(all))

	p := DefaultProfile()
	p.Name = *name
	if p.Name == "" {
		p.Name = strings.TrimSuffix(baseName(pdf), ".pdf")
	}

	sample := pickSampleRanges(all, *windows, *winSize)
	if len(sample) == 0 {
		fatal(fmt.Errorf("本文のあるページが見つかりません"))
	}
	fmt.Printf("サンプル: %s\n\n", describe(sample))

	// --- 2. ページ寸法 ---
	w, h, err := pageSize(pdf)
	if err != nil {
		fatal(err)
	}
	p.PageWidth, p.PageHeight = math.Round(w*100)/100, math.Round(h*100)/100
	fmt.Printf("ページ寸法  : %.0f x %.0f pt\n", p.PageWidth, p.PageHeight)

	// --- 3. ヘッダ・フッタ帯 ---
	p.MarginTop, p.HeaderBand = findRunningBand(pdf, sample, true, p.PageHeight)
	p.MarginBottom, p.FooterBand = findRunningBand(pdf, sample, false, p.PageHeight)
	report := func(what string, body, band float64) {
		if band == 0 {
			fmt.Printf("%s: 検出されず\n", what)
			return
		}
		fmt.Printf("%s: 本文マージン %.0f pt / 帯の切り出し %.0f pt\n", what, body, band)
	}
	report("ヘッダ      ", p.MarginTop, p.HeaderBand)
	report("フッタ      ", p.MarginBottom, p.FooterBand)

	// --- 4. 段組み ---
	gl, gr, cols, score, total := findGutter(p, pdf, sample)
	p.Columns, p.GutterLeft, p.GutterRight = cols, gl, gr
	if cols >= 2 {
		fmt.Printf("段組み      : %d 段 (左カラムは右端から %.0f / 右カラムは左端から %.0f を削る)\n", cols, gl, gr)
		fmt.Printf("段割り検証  : 取りこぼし・二重取り %d 文字 / %d 文字\n", score, total)
	} else {
		fmt.Printf("段組み      : 1 段 (最良の段割りでもずれ %d 文字 / %d 文字あり)\n", score, total)
	}

	if err := p.Save(*out); err != nil {
		fatal(err)
	}
	fmt.Printf("\nプロファイルを書き出しました: %s\n", *out)
	fmt.Printf("次: manualbook md %s -profile %s -out out/\n", pdf, *out)
}

// pickSampleRanges は本文の詰まったページから、連続した窓を数か所選ぶ。
func pickSampleRanges(pages []string, windows, size int) []sampleRange {
	var dense []int
	for i, s := range pages {
		if countGlyphs(s) > 300 {
			dense = append(dense, i+1)
		}
	}
	if len(dense) == 0 {
		return nil
	}
	if windows < 1 {
		windows = 1
	}
	if size < 1 {
		size = 1
	}

	// 前付け (目次) と後付け (索引) は避ける。
	// 目次はリーダ罫が段をまたいで走るため、段組みの較正には使えない。
	lo, hi := len(dense)*15/100, len(dense)*85/100
	if hi-lo < windows {
		lo, hi = 0, len(dense)
	}
	body := dense[lo:hi]

	var out []sampleRange
	for w := range windows {
		// 本文を windows 等分し、各区画の先頭から size ページ分をとる
		start := body[len(body)*w/windows]
		end := min(start+size-1, body[len(body)-1])
		if n := len(out); n > 0 && start <= out[n-1].hi {
			continue // 窓が重なるほど本文が少ない
		}
		out = append(out, sampleRange{start, end})
	}
	return out
}

func describe(s []sampleRange) string {
	parts := make([]string, len(s))
	for i, r := range s {
		parts[i] = fmt.Sprintf("p%d-%d", r.lo, r.hi)
	}
	return strings.Join(parts, ", ")
}

// sampleGlyphs はサンプル窓に残る文字数を数える。
func sampleGlyphs(pdf string, sample []sampleRange, c crop) int {
	d, err := openDoc(pdf)
	if err != nil {
		return 0
	}
	total := 0
	for _, r := range sample {
		for i := r.lo; i <= r.hi && i <= len(d.pages); i++ {
			pg := d.pages[i-1]
			for _, g := range pg.glyphs {
				if c.keep(g, pg.width, pg.height) {
					total++
				}
			}
		}
	}
	return total
}

// findRunningBand は柱 (ヘッダ) またはノンブル (フッタ) の位置を突き止める。
//
// ページの端から少しずつ内側へ削っていくと、柱が消えた直後に「削れる文字数」が
// 一度平らになる。その平坦部が版面の余白であり、そこを本文マージンに採る。
//
// 戻り値は (本文を切り出すマージン, その帯だけを残すための逆側マージン)。
func findRunningBand(pdf string, sample []sampleRange, top bool, pageHeight float64) (bodyMargin, bandMargin float64) {
	cut := func(m float64) crop {
		if top {
			return crop{top: m}
		}
		return crop{bottom: m}
	}

	base := sampleGlyphs(pdf, sample, crop{})
	prev := base
	for m := 2.0; m <= pageHeight/4; m += 2 {
		n := sampleGlyphs(pdf, sample, cut(m))
		// 柱は既に落ちて (n < base)、本文にはまだ届いていない (n == prev)
		if n < base && n == prev {
			// 版面のゆらぎを見込んで本文側は少し内側に寄せる。
			// 帯を残す側のマージンは、天地どちらでも pageHeight - m - 2 でよい。
			return m + 6, math.Round(pageHeight - m - 2)
		}
		prev = n
	}
	return 0, 0 // 柱・ノンブルなし
}

// findGutter は段間の切れ目を決める。
//
// 段間は「どの字も掛かっていない縦の帯」なので、字の占める x 区間を塗って、
// ページ中央寄りで最も広い空白帯を採る。その帯の左端より左が左カラム、
// 右端より右が右カラムになる。
//
// 不変条件の一致だけで決めてはいけない:
//
//	左カラムの文字数 + 右カラムの文字数 == ページ全体の文字数
//
// これは「取りこぼしも二重取りも無い」ことしか言わない。切れ目が右カラムの
// 内側にあっても、はみ出した字は左カラム側に入るので数は合う。実際、この
// 条件だけで選ぶと切れ目が右カラムの項目記号 (■) より右に寄り、記号が
// 左カラムの末尾に紛れて項目が 2039 件から 1422 件に落ちた。数が合うことは
// 必要だが、切れ目が空白帯にあることの証明にはならない。
//
// そこで空白帯で位置を決め、不変条件は検算に使う。
func findGutter(p *Profile, pdf string, sample []sampleRange) (gutterLeft, gutterRight float64, columns, bestScore, total int) {
	body := p.bodyCrop()
	total = sampleGlyphs(pdf, sample, body)
	if total == 0 {
		return 0, 0, 1, 0, 0
	}

	lo, hi, ok := findEmptyBand(pdf, sample, p, body)
	if !ok {
		return 0, 0, 1, total, total
	}

	// 切れ目は帯の中央に置き、前後 3pt だけを死角として残す。
	//
	// 帯の幅そのものを死角にしてはいけない。その中に落ちた字はどちらの段にも
	// 入らず、ページごとの段組み判定 (左+右 == 全体) が崩れて、本文ページまで
	// 全幅として読まれる (帯を 18pt 取ったとき 2 段と判定できたページが
	// 703 から 603 に減った)。逆に死角を無くすと、段をまたぐ図表がきれいに
	// 左右へ割れてしまい、全幅のページを見つけられなくなる。
	c := (lo + hi) / 2
	lc, rc := body, body
	lc.right = p.PageWidth - (c - 3) // 左カラム: 切れ目から右を捨てる
	rc.left = c + 3                  // 右カラム: 切れ目から左を捨てる
	ln := sampleGlyphs(pdf, sample, lc)
	rn := sampleGlyphs(pdf, sample, rc)
	bestScore = abs(ln + rn - total)

	// 取りこぼし・二重取りが本文の 1% を超えるなら 2 段組みではない
	if ln == 0 || rn == 0 || bestScore > total/100 {
		return 0, 0, 1, bestScore, total
	}
	return math.Round(lc.right), math.Round(rc.left), 2, bestScore, total
}

// findEmptyBand はページ中央寄りで最も広い、字の掛からない縦の帯を返す。
func findEmptyBand(pdf string, sample []sampleRange, p *Profile, body crop) (lo, hi float64, ok bool) {
	d, err := openDoc(pdf)
	if err != nil {
		return 0, 0, false
	}

	// 1pt 刻みで、その位置に字が掛かるページ数を数える。
	//
	// 「1 ページでも掛かったら段間ではない」としてはいけない。段抜きの表や図が
	// あるページでは段間も埋まるので、サンプルを重ねると帯が消える (実測では
	// 24 ページ中の数ページで塞がり、段組みなしと誤判定した)。
	w := int(math.Ceil(p.PageWidth))
	count := make([]int, w+1)
	pages := 0
	for _, r := range sample {
		for i := r.lo; i <= r.hi && i <= len(d.pages); i++ {
			pg := d.pages[i-1]
			pages++
			seen := make([]bool, w+1)
			for _, g := range pg.glyphs {
				if !body.keep(g, pg.width, pg.height) {
					continue
				}
				for x := int(math.Floor(g.left)); x <= int(math.Ceil(g.right)) && x <= w; x++ {
					if x >= 0 && !seen[x] {
						seen[x] = true
						count[x]++
					}
				}
			}
		}
	}
	if pages == 0 {
		return 0, 0, false
	}
	limit := pages / 10

	// 中央 50% の範囲だけを見る。左右の余白は段間ではない。
	from, to := w/4, w*3/4
	bestLo, bestHi := -1, -1
	for x := from; x <= to; x++ {
		if count[x] > limit {
			continue
		}
		y := x
		for y+1 <= to && count[y+1] <= limit {
			y++
		}
		if bestLo < 0 || y-x > bestHi-bestLo {
			bestLo, bestHi = x, y
		}
		x = y
	}
	if bestLo < 0 || bestHi-bestLo < 2 {
		return 0, 0, false
	}
	return float64(bestLo), float64(bestHi), true
}

// --- 補助 ---

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func baseName(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "エラー: %s\n", err)
	os.Exit(1)
}
