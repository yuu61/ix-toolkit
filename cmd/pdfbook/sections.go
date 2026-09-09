package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// 節見出しで割る読み方。機能説明書のような解説書がこちらに来る。
//
// md.go の読み方 (項目の記号と見出し語でコマンドを切り出す) が使えない資料、
// つまり EntryMarker と FieldLabels を持たないプロファイルがここへ回る。
// コマンド辞書との違いは 3 つある。
//
//  1. 項目の切れ目が記号ではなく階層番号の見出し (■2.11 / 2.11.6 / 2.11.6.1)。
//     索引は「コマンド名 → 行」ではなく「節番号 → 行」になる。
//
//  2. 表が多く、行ごとの空きをそのまま空白にすると列の対応が黙って崩れる。
//     ページ全体で同じ桁幅の格子に載せて読む (理由は layout.go のコメント)。
//
//  3. 図がある。ただし図は不透明なラスタではない。画像 XObject は作図と網掛けの
//     レイヤで、ラベルはその上に載った PDF のテキストとして取れる。実測すると
//     「マルチキャストサーバ（放送局）」「upstream」「ストリームがループ」といった
//     語はすべて抽出できており、失われているのは矢印の向き・包含・順序、
//     つまり語と語の関係だけだった。
//
//     そのため図の読み取りは実装しない。断片を地の文に流し込まないように版面
//     どおり囲って出し、元 PDF の該当ページへのリンクを添えるだけにする。
//     変換時にモデルで説明を書かせる案は採らなかった。ビルドに非決定性が入り、
//     利用者ごとに違うマニュアルが出来上がり、何より生成文が本文と区別できなく
//     なる。参照マニュアルとして、それは崩れた表と同じ種類の事故になる。

// --- 中間表現 ---

// heading は階層番号を持つ 1 見出しと、その配下の本文。
type heading struct {
	number  string // "2.11.6"
	title   string
	depth   int // 番号の階層の深さ (2.11 なら 2)
	chapter int
	section string
	pdfPage int // 見出しが現れた PDF の物理ページ (1 始まり)
	blocks  []block
	line    int // 出力ファイル中の見出し行番号 (1 始まり)。索引はここを指す
}

// block は本文の一区切り。地の文か、版面どおりに囲う塊か。
type block struct {
	layout bool // true なら版面固定 (表・図・コンソール出力)
	page   int  // この塊が現れた PDF の物理ページ
	lines  []string
}

// 見出し行。"■2.11 PPP の設定" と "2.11.6 オンデマンド帯域幅制御（BOD）の設定" の両方。
// 目次のリーダ罫 (".........") を含む行は本文の見出しではないので呼ぶ側で落とす。
var headingRe = regexp.MustCompile(`^(■)?\s*(\d+(?:\.\d+)+)\s+(\S.*?)\s*$`)

var tocLeaderRe = regexp.MustCompile(`\.{6,}`)

// --- 読み込み ---

// readSectionPages は本文帯とヘッダ・フッタ帯を読み、ページの列に組み立てる。
func readSectionPages(p *Profile, pdf string) ([]page, error) {
	body, err := p.ExtractBody(pdf)
	if err != nil {
		return nil, err
	}
	headers, footers, err := p.ExtractBands(pdf)
	if err != nil {
		return nil, err
	}

	pages := make([]page, 0, len(body))
	for i := range body {
		pg := page{num: i + 1}
		pg.section = at(headers, i)
		pg.printed = firstToken(at(footers, i))
		pg.chapter = chapterOf(pg.printed)
		pg.lines = collapseTableBlanks(splitLines(body[i]))
		pg.fullLines = pg.lines
		pages = append(pages, pg)
	}

	// 版面ヘッダは 55 ページで空になる。大きな表が版面いっぱいに広がるページでは
	// 柱もノンブルも省かれており、ヘッダ (節名) とフッタ (章番号) が同時に落ちる。
	// 節も章も複数ページに跨がるので、空のときは直前のページのものを引き継ぐ。
	// 引き継がないと、そのページの見出しが行き場を失って "ch00-その他/無題" に落ちる。
	lastSection, lastChapter := "", 0
	for i := range pages {
		if pages[i].section == "" {
			pages[i].section = lastSection
		} else {
			lastSection = pages[i].section
		}
		if pages[i].chapter == 0 {
			pages[i].chapter = lastChapter
		} else {
			lastChapter = pages[i].chapter
		}
	}
	return pages, nil
}

// collapseTableBlanks は行送りのゆらぎで入った空行を畳む。
//
// 表の行間のように 1 行分空いただけの箇所にも空行が入る (layout.go の
// blankLines)。これをそのまま残すと、地の文の折り返しが段落の切れ目と区別
// できなくなり、joinWrapped が働かない。「表示が 1 画面に収まら」と
// 「ない場合は」が別の段落として残り、通しの文で grep できなくなる。
//
// 単独の空行は組版の都合、2 つ以上続く空行は版面にもとからあった段落の
// 切れ目、とみなす。
func collapseTableBlanks(lines []string) []string {
	out := make([]string, 0, len(lines))
	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) != "" {
			out = append(out, lines[i])
			i++
			continue
		}
		n := 0
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			n++
			i++
		}
		if n >= 2 {
			out = append(out, "")
		}
	}
	return out
}

// splitRunningHeader は版面ヘッダを章名と節名に割る。
//
// ヘッダは "ルータの設定・PPP の設定" の形。節名自体が "運用・保守" や
// "統計情報・メッセージ" のように区切りを含むので、最初の 1 つだけで割る。
func splitRunningHeader(h, sep string) (chapter, section string) {
	h = strings.TrimSpace(h)
	if sep == "" {
		return "", h
	}
	if c, s, ok := strings.Cut(h, sep); ok {
		return strings.TrimSpace(c), strings.TrimSpace(s)
	}
	return "", h
}

// --- 行の分類 ---

type lineKind int

const (
	lineBlank lineKind = iota
	lineProse
	lineLayout
)

// interiorGap は行頭の字下げを除いた、行の内側の最大空白幅を返す。
//
// 字下げを含めて測ってはいけない。版面どおりに組み直した本文は段落も 7 桁ほど
// 字下げされており、それを数えると地の文が丸ごと版面扱いになる。
func interiorGap(s string) int {
	body := strings.TrimLeft(strings.TrimRight(s, " "), " ")
	maxGap, run := 0, 0
	for _, r := range body {
		if r == ' ' {
			run++
			if run > maxGap {
				maxGap = run
			}
			continue
		}
		run = 0
	}
	return maxGap
}

// classifyLine は 1 行を地の文か版面かに分ける。
//
// 表の行と図のラベルはどちらも版面に落ちる。両者を区別はしない。テキストだけ
// からでは諸元表・機能一覧表・コンソール出力例・構成図を見分けられなかった
// (ラスタ図のあるページを正解にして測ると適合率 0.11。ただし外れの中身を見ると
// そのほとんどは画像を持たない本物の表で、囲う判断自体は正しかった)。
//
// 区別する必要も無い。壊れるのは「地の文に流し込むこと」だけで、版面どおりに
// 囲えばどれも保たれる。当てにならない "図" / "表" のラベルは付けない。
func classifyLine(s string) lineKind {
	t := strings.TrimSpace(s)
	if t == "" {
		return lineBlank
	}
	// 見出しと箇条書きは、短くても文書の構造であって版面ではない。
	// これを版面に落とすと、節見出しがコードブロックに飲まれて索引が空になる。
	if isBullet(t) || headingRe.MatchString(t) {
		return lineProse
	}
	// 文末で終わる行は、短くても地の文の締め。
	if endsSentence(t) {
		return lineProse
	}
	if interiorGap(s) >= 3 {
		return lineLayout
	}
	// 日本語を 1 文字も含まない行は、この資料では地の文ではない。
	// 設定例 (ip route default BRI1/0.0)、コンソール出力、図中の英字ラベルが
	// ここに落ちる。字下げも空白の開きも無いので、この判定が無いと設定例が
	// 1 行ずつ地の文に紛れ込む。
	if !hasJapanese(t) {
		return lineLayout
	}
	// 語が 1 つ 2 つしか無い行は図のラベルの断片。
	// 短い地の文を巻き込むほうが害が大きいので、境目は低めに取る。
	if len([]rune(t)) < 22 {
		return lineLayout
	}
	return lineProse
}

// hasJapanese は仮名・漢字を含むかを返す。
//
// 半角カナも数える。この資料では半角カナだけで全角の仮名漢字を含まない行は
// 1 行も無かったので判定は変わらないが、機器の出力を転記した箇所で半角カナが
// 28,000 字ほど使われており、他の資料で単独で現れないとは限らない。
func hasJapanese(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0x3040 && r <= 0x30ff, // ひらがな・カタカナ
			r >= 0x4e00 && r <= 0x9fff, // 漢字
			r >= 0x3400 && r <= 0x4dbf, // 漢字拡張 A
			r >= 0xff66 && r <= 0xff9d: // 半角カナ
			return true
		}
	}
	return false
}

// --- 節の切り出し ---

func parseHeadings(p *Profile, pages []page) ([]heading, map[int]string) {
	chapters := map[int]string{}
	var heads []heading
	var cur *heading

	// 版面が続く塊をためる。空行 1 つでは切らない。表の行間にも空行が入るので、
	// 空行で切ると表が 1 行ずつばらばらになる。
	var pend []string
	var pendPage int
	hole := 0

	addProse := func(s string, pg int) {
		if cur == nil {
			return
		}
		n := len(cur.blocks)
		if n > 0 && !cur.blocks[n-1].layout {
			cur.blocks[n-1].lines = append(cur.blocks[n-1].lines, s)
			return
		}
		cur.blocks = append(cur.blocks, block{page: pg, lines: []string{s}})
	}
	flushLayout := func() {
		if cur == nil || len(pend) == 0 {
			pend, hole = nil, 0
			return
		}
		// 1 行だけの塊は、囲むほどの版面ではない。図のラベルの取りこぼしか、
		// 番号の付かない小見出し (「トラフィックシェーピングの動作原理」) である。
		// 地の文として出すが、直後の段落と地続きにしてはいけない。繋げると
		// 「動作原理トラフィックシェーピングでは、…」と見出しが本文に癒着して、
		// 見出しでも本文でも grep で当たらなくなる。空行を入れて段落を切る。
		if len(pend) < 2 {
			addProse(pend[0], pendPage)
			addProse("", pendPage)
			pend, hole = nil, 0
			return
		}
		cur.blocks = append(cur.blocks, block{layout: true, page: pendPage, lines: pend})
		pend, hole = nil, 0
	}

	for _, pg := range pages {
		// 版面ブロックはページを跨がせない。跨がせると、後半の行に対しても
		// 前のページ番号のリンクが付き、引く側を別のページへ送ってしまう。
		// 図や表が見開きで続く場合は、ページごとの塊が 2 つ並ぶ形になる。
		flushLayout()

		chName, secName := splitRunningHeader(pg.section, p.ChapterSep)
		if pg.chapter > 0 && chName != "" {
			chapters[pg.chapter] = chName
		}
		for _, raw := range pg.lines {
			t := strings.TrimSpace(raw)
			// 目次のリーダ罫を含む行は本文ではない。
			if tocLeaderRe.MatchString(raw) {
				continue
			}
			if m := headingRe.FindStringSubmatch(t); m != nil {
				flushLayout()
				heads = append(heads, heading{
					number:  m[2],
					title:   m[3],
					depth:   strings.Count(m[2], ".") + 1,
					chapter: pg.chapter,
					section: secName,
					// 版面に刷られた番号 (2-118) ではなく PDF の物理ページを持つ。
					// 刷られた番号は章ごとに振り直されていて PDF ビューアにも
					// ページ指定にも渡せない。
					pdfPage: pg.num,
				})
				cur = &heads[len(heads)-1]
				continue
			}
			if cur == nil {
				continue // 最初の見出しより前 (表紙・目次) は捨てる
			}

			switch classifyLine(raw) {
			case lineBlank:
				if len(pend) > 0 {
					// collapseTableBlanks を通したあとなので、空行はもう
					// 組版の都合ではなく版面にあった切れ目を指す。
					// 表の中の段の区切りは 1 つまで許し、2 つ続いたら塊を閉じる。
					hole++
					if hole > 1 {
						flushLayout()
					}
					continue
				}
				addProse("", pg.num)
			case lineLayout:
				if len(pend) == 0 {
					pendPage = pg.num
				}
				// 空行を挟んで続いた分は、塊の一部として戻す。
				for ; hole > 0; hole-- {
					pend = append(pend, "")
				}
				pend = append(pend, strings.TrimRight(raw, " "))
			case lineProse:
				flushLayout()
				addProse(raw, pg.num)
			}
		}
	}
	flushLayout()
	return heads, chapters
}

// --- 出力 ---

// figureSet は出力先の figures/ に置かれたページ画像。キーは元 PDF の物理ページ。
type figureSet map[int]string

// レンダラごとに名前が違う。pdfium-cli は出力名のテンプレートに従うので "p1057.png"、
// poppler の pdftoppm は総ページ数の桁でゼロ詰めして "p-1057.png" (1208 ページの資料) や
// "p-007.png" (852 ページの資料)、Xpdf の pdftopng は 6 桁固定で "p-001057.png" になる。
// 手で置くなら "p1057.png"。どれも受ける。
var figureNameRe = regexp.MustCompile(`^p-?0*(\d+)\.(?:png|jpg|jpeg)$`)

// ページを焼くときの実測値。すべて FD-ver10.11-1.1.pdf (全 1208 ページ) で測った。
// 版が変わったら下の手順で取り直す。
//
//   - 解像度は 150dpi でよい。この資料の最小文字は約 7pt (pdftotext -bbox の語ボックス
//     高さで実測。全 134892 語のうち約 7pt が 23 語、約 8pt が 3315 語、最小は p612 の
//     6.96pt) で、150dpi ならその 7pt の吹き出しも読める。100dpi でも一応読めるが線が痩せる。
//
//   - 全ページ焼くと 63 秒・404 MB (150dpi の PNG が 1 ページ平均 340 KB)。
//
//   - 図があるのは 1208 ページのうち 361 ページ (pdfimages -list が画像オブジェクトを
//     報告するページ数)。ただし囲みブロックの載るページは 1098 あるので、囲みを手がかりに
//     焼くページを絞ってもほとんど減らない。README で全ページ焼くことを勧めているのはこのため。
//
//   - pdfimages のような抽出は使えない。要るのはページ描画で、p1057 では図 1 枚が
//     271x39 px などの網掛け小片と smask に割れる (FD 全体で 11240 個)。
//
// 焼くのは pdfbook figures なので、測り直しに要るのは poppler (pdfimages / pdftotext
// -bbox) だけ。MSYS2 なら pacman -S mingw-w64-x86_64-poppler。pdftopng は poppler に無く
// Xpdf だけが持つので、下の figureNameRe が受ける名前を確かめるときだけ Xpdf tools
// (https://www.xpdfreader.com/download.html) の zip を落として bin64/pdftopng.exe を使う。
// figureNameRe の名前は xpdf-tools-win-4.06.zip
// (sha256 2b6ca45da794e7854a6468fd6c8063fde62701f001ce03fa4f603eab7e15a0b6) で確かめた。
//
//	pdfimages -list FD-ver10.11-1.1.pdf | tail -n +3 | awk '{print $1}' | sort -nu | wc -l
//	pdftotext -bbox FD-ver10.11-1.1.pdf - |
//	    sed -n 's/.*yMin="\([0-9.]*\)".*yMax="\([0-9.]*\)".*/\1 \2/p' |
//	    awk '{printf "%.0f\n", $2-$1}' | sort -n | uniq -c

// loadFigures は出力先に置かれたページ画像を拾う。無ければ空を返す。
//
// ここは「置いてあれば張る、無ければ張らない」だけを引き受ける。焼くのは
// pdfbook figures (md なら -figures) だが、外で焼いて置いた画像も同じように拾う。
// 画像の有無が変換の成否に影響しないので、テキストだけ先に作って後から
// 焼き足してもよい。
//
// どのページを焼くかも利用者側に残る。図のあるページだけを焼けば図のブロック
// にだけ画像が付き、全ページ焼けば表にも付く。変換側で図と表を判別しない
// (テキストからは判別できない) 以上、これが最も確かな絞り方になる。
func loadFigures(outDir string) figureSet {
	ents, err := os.ReadDir(filepath.Join(outDir, "figures"))
	if err != nil {
		return nil
	}
	figs := figureSet{}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		m := figureNameRe.FindStringSubmatch(strings.ToLower(e.Name()))
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			continue
		}
		figs[n] = e.Name()
	}
	return figs
}

// links は 1 つの節ファイルから外へ張るリンク。相対パスは節ファイルの
// 置き場所から辿れる形で持つ。
type links struct {
	pdf  string    // 元 PDF への相対パス
	figs figureSet // ページ画像 (無ければ nil)
	base string    // figures/ への相対パス
}

// image はページ n の画像への相対パスを返す。置かれていなければ "" を返す。
func (l links) image(n int) string {
	name, ok := l.figs[n]
	if !ok {
		return ""
	}
	return l.base + "/" + name
}

func writeSections(outDir, docTitle, source, pdf string, p *Profile,
	heads []heading, chapters map[int]string,
) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	figs := loadFigures(outDir)
	if len(figs) > 0 {
		fmt.Printf("  ページ画像: %d 枚 (%s)\n", len(figs), filepath.Join(outDir, "figures"))
	}

	var order []sectionKey
	grouped := map[sectionKey][]*heading{}
	for i := range heads {
		k := sectionKey{heads[i].chapter, heads[i].section}
		if _, ok := grouped[k]; !ok {
			order = append(order, k)
		}
		grouped[k] = append(grouped[k], &heads[i])
	}

	relPath := map[sectionKey]string{}
	for _, k := range order {
		chDir := "ch00-その他"
		if k.chapter > 0 {
			chDir = fmt.Sprintf("ch%02d-%s", k.chapter, safeName(chapters[k.chapter]))
		}
		rel := filepath.Join(chDir, safeName(k.section)+".md")
		relPath[k] = rel
		full := filepath.Join(outDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}

		// 図表から外へ張るリンク。節ファイルの位置から実際に辿れる形で書く。
		lk := links{
			pdf:  pdfLinkBase(filepath.Dir(full), pdf),
			figs: figs,
			base: relLink(filepath.Dir(full), filepath.Join(outDir, "figures")),
		}

		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n", k.section)
		line := 1 + strings.Count(b.String(), "\n")
		for _, h := range grouped[k] {
			var hb strings.Builder
			renderHeading(&hb, h, lk)
			h.line = line
			line += strings.Count(hb.String(), "\n")
			b.WriteString(hb.String())
		}
		if err := os.WriteFile(full, []byte(b.String()), 0o644); err != nil {
			return err
		}
	}

	if err := writeSectionIndex(outDir, docTitle, chapters, order, grouped, relPath, heads); err != nil {
		return err
	}
	return writeSectionReadme(outDir, docTitle, source, pdf, p, len(heads), len(order), figs)
}

// pdfLinkBase は節ファイルの置き場所から元 PDF へ辿る相対パスを返す。
// 辿れない (ドライブが違う等) ときは、素のファイル名に落とす。
func pdfLinkBase(fromDir, pdf string) string {
	if rel := relLink(fromDir, pdf); rel != "" {
		return rel
	}
	return baseName(pdf)
}

// relLink は fromDir から target へ辿る相対パスを URL 向きの区切りで返す。
// 辿れないときは "" を返す。
func relLink(fromDir, target string) string {
	abs, err := filepath.Abs(target)
	if err != nil {
		return ""
	}
	from, err := filepath.Abs(fromDir)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(from, abs)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

// renderHeading は 1 見出しとその本文を書き出す。
func renderHeading(b *strings.Builder, h *heading, lk links) {
	// 番号の深さをそのまま見出しの深さにする。節ファイルの見出しが "# " なので
	// 1 つ下げて始める。
	level := min(6, h.depth+1)
	fmt.Fprintf(b, "%s %s %s\n\n", strings.Repeat("#", level), h.number, h.title)

	for _, blk := range h.blocks {
		if blk.layout {
			renderLayoutBlock(b, blk, lk)
			continue
		}
		joined := joinWrapped(blk.lines)
		if len(joined) == 0 {
			continue
		}
		renderBody(b, joined)
	}
}

// renderLayoutBlock は表・図・コンソール出力を版面どおりに囲って出し、
// 元 PDF の該当ページへのリンクを添える。
//
// リンクを添えるのは、囲った中身だけでは足りないことがあるからである。
// 図のラベルは取れていても矢印の向きや包含関係は失われており、表も脚注の
// 対応 (※1 がどの欄に掛かるか) までは残らない。引く側が「ここは版面を見ないと
// 分からない」と判断できるように、必ず出所を書く。
//
// ページ画像が置いてあれば、そこへのリンクも並べる。PDF へのリンクは
// ビューアを開ける人しか辿れないが、画像なら引く側がそのまま開いて版面を
// 見られる。囲みの中身は消さない。節を探し当てているのは囲みの中の語であり、
// 画像はそれを置き換えるものではなく、足りない分を補うものである。
func renderLayoutBlock(b *strings.Builder, blk block, lk links) {
	lines := dedent(trimBlankEdges(blk.lines))
	if len(lines) == 0 {
		return
	}
	b.WriteString("```text\n")
	for _, ln := range lines {
		// 囲みの中に囲みの終わりが現れると Markdown が壊れる。
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			ln = " " + ln
		}
		b.WriteString(ln + "\n")
	}
	b.WriteString("```\n")
	fmt.Fprintf(b, "<sup>[元 PDF p%d](%s)", blk.page,
		mdLinkDest(fmt.Sprintf("%s#page=%d", lk.pdf, blk.page)))
	if img := lk.image(blk.page); img != "" {
		fmt.Fprintf(b, " / [ページ画像](%s)", mdLinkDest(img))
	}
	b.WriteString("</sup>\n\n")
}

func trimBlankEdges(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func writeSectionIndex(outDir, docTitle string, chapters map[int]string, order []sectionKey,
	grouped map[sectionKey][]*heading, relPath map[sectionKey]string, heads []heading,
) error {
	// --- 機械可読索引 ---
	//
	// コマンド辞書ではないので、キーは節番号と見出し語になる。skill は
	// 節番号で引くか、見出し語を部分一致で探すか、本文を全文検索する。
	var t strings.Builder
	fmt.Fprintln(&t, "section\ttitle\tfile\tline\tpdfpage")
	for i := range heads {
		h := &heads[i]
		file := filepath.ToSlash(relPath[sectionKey{h.chapter, h.section}])
		fmt.Fprintf(&t, "%s\t%s\t%s\t%d\t%d\n", h.number, h.title, file, h.line, h.pdfPage)
	}
	if err := os.WriteFile(filepath.Join(outDir, "sections.tsv"), []byte(t.String()), 0o644); err != nil {
		return err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s — 目次\n\n", docTitle)
	fmt.Fprintf(&b, "全 %d 見出し / %d 節。節番号や見出し語から引くには `sections.tsv` "+
		"(`section` / `title` / `file` / `line` / `pdfpage` のタブ区切り) を検索する。\n",
		len(heads), len(order))
	lastCh := -1
	for _, k := range order {
		if k.chapter != lastCh {
			fmt.Fprintf(&b, "\n## %d. %s\n\n", k.chapter, chapters[k.chapter])
			lastCh = k.chapter
		}
		fmt.Fprintf(&b, "- [%s](%s) — %d 見出し\n", k.section, mdLinkDest(relPath[k]), len(grouped[k]))
	}
	return os.WriteFile(filepath.Join(outDir, "index.md"), []byte(b.String()), 0o644)
}

func writeSectionReadme(outDir, docTitle, source, pdf string, p *Profile,
	nHeads, nSections int, figs figureSet,
) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (Markdown 変換版)\n\n", docTitle)
	fmt.Fprintln(&b, "この配下は PDF から機械変換した生成物であり、著作権は原著作者に帰属する。")
	fmt.Fprintln(&b, "**再配布しないこと。** リポジトリでは `.gitignore` により除外されている。")
	fmt.Fprintf(&b, "\n## 生成条件\n\n")
	fmt.Fprintf(&b, "- 元 PDF: `%s`\n", baseName(pdf))
	if source != "" {
		fmt.Fprintf(&b, "- 取得元: %s\n", source)
	}
	fmt.Fprintf(&b, "- プロファイル: `%s` (節見出しで割る, 天地マージン %.0f/%.0f pt)\n",
		p.Name, p.MarginTop, p.MarginBottom)
	fmt.Fprintf(&b, "- 見出し数: %d / 節数: %d\n", nHeads, nSections)
	fmt.Fprintln(&b, "- 変換経路: PDF のテキスト層を版面どおりに組み直したもの (OCR・画像解析・モデル不使用)")

	fmt.Fprintf(&b, "\n## 引き方\n\n")
	fmt.Fprintln(&b, "- `sections.tsv` — `section / title / file / line / pdfpage` のタブ区切り索引。")
	fmt.Fprintln(&b, "  `line` は本文ファイル中の見出し行 (1 始まり)。")
	fmt.Fprintln(&b, "- `index.md` — 章・節の目次")
	fmt.Fprintln(&b, "- `chNN-<章名>/<節名>.md` — 本文")

	fmt.Fprintf(&b, "\n## ```text で囲まれた塊について\n\n")
	fmt.Fprintln(&b, "表・図・コンソール出力は、版面どおりの固定幅ブロックとして囲ってある。")
	fmt.Fprintln(&b, "地の文に流し込むと表の列の対応が崩れ、図のラベルが本文に混ざるためで、")
	fmt.Fprintln(&b, "**囲みの中は行と桁の位置に意味がある。**")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "囲みが表なのか図なのかは区別していない。テキストからは判別できず、")
	fmt.Fprintln(&b, "誤ったラベルを付けるより出所を示すほうが確かなので、各ブロックの直後に")
	fmt.Fprintln(&b, "元 PDF の該当ページへのリンクを置いてある。")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "**図の中身はこのテキストだけでは完結しない。** 図のラベル (機器名・")
	fmt.Fprintln(&b, "インタフェース名・プロトコル名) は抽出できているが、矢印の向き・包含関係・")
	fmt.Fprintln(&b, "順序は失われている。構成や流れを答えるにはページそのものを見る必要がある。")
	fmt.Fprintln(&b, "テキストの断片から構成を推測しない。")

	fmt.Fprintf(&b, "\n## ページ画像\n\n")
	if len(figs) > 0 {
		fmt.Fprintf(&b, "`figures/` に %d ページ分の画像がある。囲みの直後の "+
			"`[ページ画像]` から辿れる。\n", len(figs))
		fmt.Fprintln(&b, "**図について答えるときは、囲みのテキストではなくこの画像を見ること。**")
	} else {
		fmt.Fprintln(&b, "`figures/` が無いので、囲みには元 PDF へのリンクしか付いていない。")
		fmt.Fprintln(&b, "ページを焼いて `figures/` に置き、変換し直すと、囲みの直後に")
		fmt.Fprintln(&b, "`[ページ画像]` が並ぶ。焼くのは `pdfbook figures` で、外部の道具は要らない。")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "このディレクトリで:")
		fmt.Fprintln(&b)
		fmt.Fprintf(&b, "    pdfbook figures <%s のあるパス> -out .\n", baseName(pdf))
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "そのあと `pdfbook md` をもう一度流す (変換は figures/ を消さない)。")
		fmt.Fprintln(&b, "はじめから `pdfbook md -figures` で流せば 1 回で済む。")
	}
	return os.WriteFile(filepath.Join(outDir, "README.md"), []byte(b.String()), 0o644)
}

// runSectionMD は md サブコマンドの、節見出しで割る経路。
func runSectionMD(outDir, docTitle, source, pdf string, p *Profile) {
	fmt.Printf("読み込み: %s (プロファイル %s / 節見出しで割る)\n", pdf, p.Name)
	pages, err := readSectionPages(p, pdf)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("  ページ数: %d\n", len(pages))

	heads, chapters := parseHeadings(p, pages)
	nLayout := 0
	for i := range heads {
		for _, b := range heads[i].blocks {
			if b.layout {
				nLayout++
			}
		}
	}
	fmt.Printf("  見出し: %d 件 / 章: %d / 版面ブロック: %d\n", len(heads), len(chapters), nLayout)

	if len(heads) == 0 {
		fmt.Fprintln(os.Stderr, "\n⚠ 見出しを 1 件も抽出できませんでした。")
		fmt.Fprintln(os.Stderr, "  この資料の見出しが階層番号 (2.11.6 の形) で始まっているか確認してください。")
		os.Exit(2)
	}

	if err := writeSections(outDir, docTitle, source, pdf, p, heads, chapters); err != nil {
		fatal(err)
	}
	fmt.Printf("\n出力しました: %s\n", outDir)
	fmt.Printf("  機械可読索引: %s\n", filepath.Join(outDir, "sections.tsv"))
	fmt.Printf("  目次:         %s\n", filepath.Join(outDir, "index.md"))
}
