package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
)

// probe サブコマンド: PDF を数十回試し読みして Profile を自動較正する。
//
// 較正はすべて「pdftotext を走らせて文字数を数える」だけで行う。PDF の内部構造
// (MediaBox, フォント, CMap) を一切パースしないので、CJK の CMap 対応で崩れる
// 心配がない。判定はどれも目視ではなく数値の不変条件で行う。

// sampleRange は較正に使う連続ページ範囲。
// 飛び飛びのページを指定すると pdftotext が毎回全ページを読むことになるため、
// 短い連続範囲を数か所とる。
type sampleRange struct{ lo, hi int }

func runProbe(args []string) {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	out := fs.String("out", "profile.json", "出力するプロファイル JSON")
	name := fs.String("name", "", "プロファイル名 (デフォルト: PDF のファイル名)")
	windows := fs.Int("windows", 3, "較正に使うサンプル窓の数")
	winSize := fs.Int("window-size", 8, "サンプル窓 1 つあたりのページ数")
	pos := parseFlags(fs, args)

	if len(pos) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: pdfbook probe <pdf> [-out profile.json]")
		os.Exit(1)
	}
	pdf := pos[0]

	if err := pdftotextAvailable(); err != nil {
		fatal(err)
	}

	// --- 1. テキスト層の有無 ---
	glyphs, err := hasTextLayer(pdf, 20)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("テキスト層: 先頭 20 ページで %d 文字\n", glyphs)
	if glyphs < 200 {
		fmt.Fprintln(os.Stderr, "\n  ⚠ テキスト層がほとんどありません（紙スキャン由来の画像 PDF の可能性）。")
		fmt.Fprintln(os.Stderr, "    md サブコマンドは使えません。画像化 → pdfbook scan で分割 → OCR の経路が必要です。")
		os.Exit(2)
	}

	all, err := extract(pdf, "-raw")
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
	p.PageWidth = findPageSize(pdf, sample, false)
	p.PageHeight = findPageSize(pdf, sample, true)
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
		fmt.Printf("段組み      : %d 段 (左 -marginr %.0f / 右 -marginl %.0f)\n", cols, gl, gr)
		fmt.Printf("段割り検証  : 取りこぼし・二重取り %d 文字 / %d 文字\n", score, total)
	} else {
		fmt.Printf("段組み      : 1 段 (最良の段割りでもずれ %d 文字 / %d 文字あり)\n", score, total)
	}

	if err := p.Save(*out); err != nil {
		fatal(err)
	}
	fmt.Printf("\nプロファイルを書き出しました: %s\n", *out)
	fmt.Printf("次: pdfbook md %s -profile %s -out out/\n", pdf, *out)
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

// sampleGlyphs はサンプル窓を読み、空白を除いた総文字数を返す。
func sampleGlyphs(pdf string, sample []sampleRange, args ...string) int {
	total := 0
	for _, r := range sample {
		pages, err := extract(pdf, append(args, "-f", fmt.Sprint(r.lo), "-l", fmt.Sprint(r.hi))...)
		if err != nil {
			return 0
		}
		for _, pg := range pages {
			total += countGlyphs(pg)
		}
	}
	return total
}

// findPageSize はページの幅または高さを求める。
//
// マージンは「ページの端から」測られるので、ページ寸法が分からないと
// 段組みの位置を絶対座標で指定できない。PDF の MediaBox を読まずに、
// pdftotext の応答だけから次の恒等式で復元する:
//
//	端Aからの余白 + 端Bからの余白 + 文字の占める幅 == ページ寸法
//
// 具体的には「全部消える -marginr の値」= 幅 - 左端 と
// 「全部消え始める -marginl の値」= 左端 を足し合わせる。
func findPageSize(pdf string, sample []sampleRange, vertical bool) float64 {
	nearFlag, farFlag := "-marginr", "-marginl" // 水平: 右端から / 左端から
	if vertical {
		nearFlag, farFlag = "-margint", "-marginb"
	}
	span := findExtent(pdf, nearFlag, sample)  // ページ寸法 - 文字の最小座標
	offset := findMargin(pdf, farFlag, sample) // 文字の最小座標
	return math.Round(span + offset)
}

// findExtent はマージンを広げ、テキストが完全に消える境界を二分探索する。
func findExtent(pdf string, marginFlag string, sample []sampleRange) float64 {
	lo, hi := 0.0, 2400.0
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if sampleGlyphs(pdf, sample, "-raw", marginFlag, ftoa(mid)) > 0 {
			lo = mid // まだテキストが残っている
		} else {
			hi = mid // 全部消えた
		}
	}
	return math.Round(hi)
}

// findMargin はテキストが 1 文字も欠けない最大のマージンを二分探索する。
// これが版面の余白 (＝最も端に寄った文字の座標) になる。
func findMargin(pdf string, marginFlag string, sample []sampleRange) float64 {
	base := sampleGlyphs(pdf, sample, "-raw")
	lo, hi := 0.0, 2400.0
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if sampleGlyphs(pdf, sample, "-raw", marginFlag, ftoa(mid)) == base {
			lo = mid // まだ 1 文字も欠けていない
		} else {
			hi = mid // 削り始めた
		}
	}
	return math.Round(lo)
}

// findRunningBand は柱 (ヘッダ) またはノンブル (フッタ) の位置を突き止める。
//
// ページの端から少しずつ内側へ削っていくと、柱が消えた直後に「削れる文字数」が
// 一度平らになる。その平坦部が版面の余白であり、そこを本文マージンに採る。
//
// 戻り値は (本文を切り出すマージン, その帯だけを残すための逆側マージン)。
func findRunningBand(pdf string, sample []sampleRange, top bool, pageHeight float64) (bodyMargin, bandMargin float64) {
	cutFlag := "-marginb"
	if top {
		cutFlag = "-margint"
	}

	base := sampleGlyphs(pdf, sample, "-raw")
	prev := base
	for m := 2.0; m <= pageHeight/4; m += 2 {
		n := sampleGlyphs(pdf, sample, "-raw", cutFlag, ftoa(m))
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

// findGutter は段間の切れ目を総当たりで決める。
//
// 判定基準は目視ではなく不変条件:
//
//	左カラムの文字数 + 右カラムの文字数 == ページ全体の文字数
//
// これが崩れるのは「どちらにも入らない文字がある」(ガターが広すぎる) か
// 「両方に入る文字がある」(狭すぎる) ときだけなので、ずれ量が最小の位置を採る。
// 4pt ずれていても目視では気づけないが、この数値は必ず反応する。
func findGutter(p *Profile, pdf string, sample []sampleRange) (gutterLeft, gutterRight float64, columns, bestScore, total int) {
	body := p.bodyArgs()
	total = sampleGlyphs(pdf, sample, append([]string{"-layout"}, body...)...)
	if total == 0 {
		return 0, 0, 1, 0, 0
	}

	bestScore = -1
	var best []float64 // 最小スコアを出した切れ目 (絶対 x) をすべて覚える
	center := p.PageWidth / 2
	for c := center - p.PageWidth*0.25; c <= center+p.PageWidth*0.25; c += 4 {
		lm := p.PageWidth - c + 3 // 左カラム: -marginr
		rm := c + 3               // 右カラム: -marginl
		if lm <= 0 || rm <= 0 {
			continue
		}
		ln := sampleGlyphs(pdf, sample, append([]string{"-layout", "-marginr", ftoa(lm)}, body...)...)
		rn := sampleGlyphs(pdf, sample, append([]string{"-layout", "-marginl", ftoa(rm)}, body...)...)
		if ln == 0 || rn == 0 {
			continue // 片側に寄りすぎ = 段組みとして成立していない
		}
		score := abs(ln + rn - total)
		if bestScore < 0 || score < bestScore {
			bestScore, best = score, []float64{c}
		} else if score == bestScore {
			best = append(best, c)
		}
	}

	// 取りこぼし・二重取りが本文の 1% を超えるなら 2 段組みではない
	if bestScore < 0 || bestScore > total/100 {
		return 0, 0, 1, bestScore, total
	}

	// 同点の切れ目が連続して並ぶのが段間の空白そのもの。その中央を採ると、
	// 版面が多少ゆれても両側に余裕が残る。端を採ると片側だけ余裕が無くなる。
	c := best[len(best)/2]
	return math.Round(p.PageWidth - c + 3), math.Round(c + 3), 2, bestScore, total
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
