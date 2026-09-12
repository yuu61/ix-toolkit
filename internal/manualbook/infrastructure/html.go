package infrastructure

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
	"golang.org/x/net/html"
)

// Sphinx が出した HTML を読む前段。fetch が置いた取得キャッシュ (1 ページ 1 ファイル)
// から、md.go の entry か sections.go の heading を組み立てる。
//
// PDF 前段と違って版面の解析は要らない。構造は DOM にそのまま入っている:
//
//   - どのページも <h1> が「21.1. AAA」のように番号を持つ。番号の先頭が章、
//     題が節で、これがそのまま出力ファイルの単位 (sectionKey) になる。
//   - 章名はパンくず (breadcrumb) の「21. リモートアクセス編」から取る。
//   - コマンドリファレンスは 1 コマンド = <section id="aaa-enable"> + <h2> +
//     <dl>。<dt> が [入力形式] [パラメータ] … の見出し語で、PDF の fieldLabels と
//     同じ構成。<dl> の中身が entry.fields になる。
//   - 機能説明書は <section> の入れ子で、<h2>〜<h6> の番号と深さが DOM から
//     直接取れる。<table> は表、<figure><img> は図、<pre> と line-block は
//     コンソール出力で、テキストから推定していた PDF 前段より確かに分かる。
//
// 表は Markdown の表に、図は SVG のまま figures/ に置く (design memo の決定)。

// WebMeta は fetch が取得キャッシュに置く .manualbook.json。
// md はこれを読んで、題・版・系列・プロファイルの既定値にする。
type WebMeta struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Version string `json:"version"`
	Series  string `json:"series"`
	Profile string `json:"profile"`
	Fetched string `json:"fetched"`
}

const WebMetaName = ".manualbook.json"

func ReadWebMeta(dir string) (WebMeta, error) {
	var m WebMeta
	b, err := os.ReadFile(filepath.Join(dir, WebMetaName))
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(b, &m)
	return m, err
}

// WebPage は取得キャッシュの 1 ページ。
type WebPage struct {
	path         string     // 冊子の起点からの相対パス (cli/remoteaccess/cli_aaa.html)
	number       []int      // <h1> の番号 (21.1 → [21 1])
	title        string     // <h1> の題 (番号を除く)
	chapterTitle string     // パンくずの章名
	body         *html.Node // 本文の <section> (h1 を含む)
}

// --- 読み込み ---

// ReadWebPages は取得キャッシュの HTML を全部読み、番号順に並べる。
// 番号付きの <h1> を持たないページ (表紙・検索・索引) は本文ではないので落とす。
func ReadWebPages(dir string) ([]WebPage, error) {
	var pages []WebPage
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".html") {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		pg, ok, err := readWebPage(p, filepath.ToSlash(rel))
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if ok {
			pages = append(pages, pg)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(pages, func(i, j int) bool { return lessNumber(pages[i].number, pages[j].number) })
	return pages, nil
}

func lessNumber(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func readWebPage(file, rel string) (WebPage, bool, error) {
	f, err := os.Open(file)
	if err != nil {
		return WebPage{}, false, err
	}
	defer f.Close()
	doc, err := html.Parse(f)
	if err != nil {
		return WebPage{}, false, err
	}
	body := findNode(doc, func(n *html.Node) bool { return attr(n, "itemprop") == "articleBody" })
	if body == nil {
		return WebPage{}, false, nil
	}
	sec := findNode(body, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "section" })
	if sec == nil {
		return WebPage{}, false, nil
	}
	h1 := findNode(sec, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "h1" })
	if h1 == nil {
		return WebPage{}, false, nil
	}
	num, title := headingParts(h1)
	if len(num) == 0 {
		return WebPage{}, false, nil
	}
	pg := WebPage{path: rel, number: num, title: title, body: sec}

	// 章名はパンくずから。「21. リモートアクセス編」のように番号が 1 段のものが章。
	// 章そのもののページ (機能説明書の「8. IXシリーズとの差分」) はパンくずに
	// 章の項が無いので、h1 の題がそのまま章名になる。
	pg.chapterTitle = title
	walk(doc, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data == "li" && strings.Contains(attr(n, "class"), "breadcrumb-item") {
			if bn, bt := headingParts(n); len(bn) == 1 && bn[0] == num[0] {
				pg.chapterTitle = bt
			}
		}
		return true
	})
	return pg, true, nil
}

// headingParts は見出しの番号と題を返す。番号は <span class="section-number">
// に「8.2.1. 」の形で入っている。番号が無ければ nil。
func headingParts(n *html.Node) ([]int, string) {
	var numText string
	var title strings.Builder
	walk(n, func(c *html.Node) bool {
		if c.Type == html.ElementNode {
			switch {
			case c.Data == "span" && strings.Contains(attr(c, "class"), "section-number"):
				numText = nodeText(c)
				return false
			case c.Data == "a" && strings.Contains(attr(c, "class"), "headerlink"):
				return false
			}
		}
		if c.Type == html.TextNode {
			title.WriteString(c.Data)
		}
		return true
	})
	t := domain.Collapse(title.String())
	numText = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(numText), "."))
	if numText == "" {
		return nil, t
	}
	var num []int
	for _, part := range strings.Split(numText, ".") {
		v, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, t
		}
		num = append(num, v)
	}
	return num, t
}

var autoIDRe = regexp.MustCompile(`^id\d*$`)

// anchorOf は節を指すアンカーを選ぶ。
//
// Sphinx は節に id を 2 つ持たせることがある。<section id="…"> と、その直下の
// <span id="…"></span>。片方は題から作った自動の id (ipv4 / id6)、片方は原稿に
// 書かれたラベル (ip-access-list-icmp / aaa-enable) で、どちらがどちらに載るかは
// 決まっていない。冊子の索引 (cliindex.html) は原稿のラベルで指すので、
// 自動の id (idN) でない方を優先し、<span> のラベルを <section> の id より先に取る。
//
// これで cliindex.html の 1311 件のうち 4 件を除いて一致する。残りは原稿の誤りで、
// 直さない (1.5a 時点): cli_ssh.html の show-ssh-server-host-key は
// クライアント側の節に付いていて、サーバ側の節は自動の id9 しか持たない。
// cli_traceroute.html#id1 は索引が自動の id を指している (節は ipv4)。
// cli_ikev2.html の ikev2-authentication / ikev2-dpd はどの節にも無い。
// cli_napt.html と cli_mobile.html の 3 件は索引に載っていない項目。
func anchorOf(sec *html.Node) string {
	secID := attr(sec, "id")
	for c := sec.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if isHeadingTag(c.Data) {
			break
		}
		if c.Data == "span" {
			if id := attr(c, "id"); id != "" && !autoIDRe.MatchString(id) {
				return id
			}
		}
	}
	return secID
}

func numberString(num []int) string {
	parts := make([]string, len(num))
	for i, v := range num {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ".")
}

// --- コマンド辞書 (entry) ---

// ParseWebEntries はコマンドリファレンスのページ群から項目を切り出す。
//
// 項目は「<dl> を直接の子に持ち、その最初の <dt> が見出し語の <section>」。
// PDF の entryMarker にあたる記号は無く、この構造そのものが項目の頭になる。
func ParseWebEntries(pages []WebPage, p *domain.Profile) ([]domain.Entry, map[int]string) {
	labels := map[string]bool{}
	for _, l := range p.FieldLabels {
		labels[domain.NormalizeLabel(l)] = true
	}
	chapters := map[int]string{}
	var entries []domain.Entry
	for _, pg := range pages {
		chapters[pg.number[0]] = pg.chapterTitle
		walk(pg.body, func(n *html.Node) bool {
			if n.Type != html.ElementNode || n.Data != "section" {
				return true
			}
			fields := entryFields(n, labels)
			if len(fields) == 0 {
				return true
			}
			h := findNode(n, func(c *html.Node) bool { return c.Type == html.ElementNode && isHeadingTag(c.Data) })
			_, title := headingParts(h)
			e := domain.Entry{
				Title:   title,
				Chapter: pg.number[0],
				Section: pg.title,
				Ref:     domain.Ref{Path: pg.path, Anchor: anchorOf(n)},
				Fields:  fields,
			}
			e.Cmds = domain.CommandsOf(&e)
			entries = append(entries, e)
			return false // 項目の中に項目は無い
		})
	}
	return entries, chapters
}

// entryFields は項目の <section> から見出し語ごとの欄を取る。無ければ nil (項目ではない)。
//
// 原稿の書き方が 2 通りある。
//
//	<dl><dt>[入力形式]</dt><dd>…</dd><dt>[パラメータ]</dt><dd>…</dd></dl>
//	<p><strong>[入力形式]</strong></p><blockquote>…</blockquote><p><strong>[パラメータ]</strong></p>…
//
// 後者は見出し語の <p> から次の見出し語の <p> の手前までが欄の中身。
// どちらも節の直下だけを見る (入れ子の節は別の項目)。
func entryFields(sec *html.Node, labels map[string]bool) []domain.Field {
	var fields []domain.Field
	var cur *domain.Field // <p> 形式で開いている欄
	flush := func() {
		if cur != nil {
			fields = append(fields, *cur)
			cur = nil
		}
	}
	add := func(label string, nodes ...*html.Node) {
		f := domain.Field{Label: label}
		for _, n := range nodes {
			// 構文の欄は 1 行 1 コマンドの生の行。それ以外は論理行で、
			// joinWrapped が行を繋がないよう空行で区切る (proseLines)。
			if domain.SyntaxLabels[label] {
				f.Lines = append(f.Lines, rawLines(n)...)
			} else {
				f.Lines = append(f.Lines, proseLines(n)...)
			}
		}
		fields = append(fields, f)
	}
	for c := sec.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		switch {
		case c.Data == "section":
			flush()
			return fields
		case c.Data == "dl":
			flush()
			for d := c.FirstChild; d != nil; d = d.NextSibling {
				if d.Type != html.ElementNode || d.Data != "dt" {
					continue
				}
				label := dtLabel(d)
				if !labels[domain.NormalizeLabel(label)] {
					continue
				}
				dd := d.NextSibling
				for dd != nil && !(dd.Type == html.ElementNode && dd.Data == "dd") {
					dd = dd.NextSibling
				}
				if dd != nil {
					add(domain.NormalizeLabel(label), dd)
				} else {
					add(domain.NormalizeLabel(label))
				}
			}
		case c.Data == "p" && labels[domain.NormalizeLabel(dtLabel(c))]:
			flush()
			cur = &domain.Field{Label: domain.NormalizeLabel(dtLabel(c))}
		default:
			if cur == nil {
				continue
			}
			if domain.SyntaxLabels[cur.Label] {
				cur.Lines = append(cur.Lines, rawLines(c)...)
			} else {
				cur.Lines = append(cur.Lines, proseLines(c)...)
			}
		}
	}
	flush()
	return fields
}

var dtBracketRe = regexp.MustCompile(`^\s*[\[［]\s*(.*?)\s*[\]］]\s*$`)

// dtLabel は <dt><strong>[入力形式]</strong></dt> から見出し語を取る。
func dtLabel(dt *html.Node) string {
	t := domain.Collapse(nodeText(dt))
	if m := dtBracketRe.FindStringSubmatch(t); m != nil {
		return m[1]
	}
	return t
}

// --- 解説書 (heading) ---

// WriteWebFigures は解析済み本文の画像参照を保存し、出力名と図中ラベルを補う。
// 保存に失敗した場合は呼び出し元に返し、成功した画像だけを重複排除する。
func WriteWebFigures(cacheDir, outDir string, heads []domain.Heading) error {
	type savedFigure struct {
		name   string
		labels []string
	}
	saved := map[string]savedFigure{}
	dir := filepath.Join(outDir, "figures")
	for i := range heads {
		for j := range heads[i].Blocks {
			b := &heads[i].Blocks[j]
			if b.Kind != domain.BlockFigure {
				continue
			}
			rel := b.FigureSource
			fig, ok := saved[rel]
			if !ok {
				from := filepath.Join(cacheDir, filepath.FromSlash(rel))
				fig.name = path.Base(rel)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("図の出力先を作れません: %w", err)
				}
				if err := copyFile(from, filepath.Join(dir, fig.name)); err != nil {
					return fmt.Errorf("図を写せません: %s: %w", rel, err)
				}
				if strings.EqualFold(path.Ext(fig.name), ".svg") {
					fig.labels = svgLabels(from)
				}
				saved[rel] = fig
			}
			b.Figure, b.Lines = fig.name, fig.labels
		}
	}
	return nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ParseWebHeadings は機能説明書のページ群から見出しと本文を切り出す。
func ParseWebHeadings(pages []WebPage) ([]domain.Heading, map[int]string) {
	chapters := map[int]string{}
	var heads []domain.Heading
	for _, pg := range pages {
		chapters[pg.number[0]] = pg.chapterTitle
		var visit func(sec *html.Node)
		visit = func(sec *html.Node) {
			h := findNode(sec, func(c *html.Node) bool { return c.Type == html.ElementNode && isHeadingTag(c.Data) })
			num, title := headingParts(h)
			if len(num) == 0 {
				return
			}
			r := domain.Ref{Path: pg.path, Anchor: anchorOf(sec)}
			hd := domain.Heading{
				Number:  numberString(num),
				Title:   title,
				Depth:   len(num),
				Chapter: pg.number[0],
				Section: pg.title,
				Ref:     r,
			}
			var content []*html.Node
			var subs []*html.Node
			for c := sec.FirstChild; c != nil; c = c.NextSibling {
				if c.Type != html.ElementNode {
					continue
				}
				switch {
				case c.Data == "section":
					subs = append(subs, c)
				case isHeadingTag(c.Data) && c == h:
				default:
					content = append(content, c)
				}
			}
			hd.Blocks = contentBlocks(content, pg.path, r)
			heads = append(heads, hd)
			for _, s := range subs {
				visit(s)
			}
		}
		visit(pg.body)
	}
	return heads, chapters
}

// contentBlocks は節の直下の要素を本文の塊に組み立てる。
func contentBlocks(nodes []*html.Node, pagePath string, r domain.Ref) []domain.Block {
	var blocks []domain.Block
	addProse := func(lines []string) {
		if len(lines) == 0 {
			return
		}
		n := len(blocks)
		if n > 0 && blocks[n-1].Kind == domain.BlockProse {
			blocks[n-1].Lines = append(blocks[n-1].Lines, lines...)
			return
		}
		blocks = append(blocks, domain.Block{Kind: domain.BlockProse, Ref: r, Lines: lines})
	}
	addFigure := func(n *html.Node) {
		img := n
		if n.Data == "figure" {
			img = findNode(n, func(c *html.Node) bool { return c.Type == html.ElementNode && c.Data == "img" })
		}
		if img == nil {
			return
		}
		rel := path.Join(path.Dir(pagePath), attr(img, "src"))
		blocks = append(blocks, domain.Block{Kind: domain.BlockFigure, Ref: r, FigureSource: rel})
		// figcaption があれば地の文として続ける
		if cap := findNode(n, func(c *html.Node) bool { return c.Type == html.ElementNode && c.Data == "figcaption" }); cap != nil {
			addProse([]string{domain.Collapse(nodeText(cap)), ""})
		}
	}
	// nestedFigures は箇条書きなどの中に埋まった図を拾う。地の文の塊には図を
	// 置けないので、その塊の直後に並べる (節の中での位置は少しずれる)。
	nestedFigures := func(n *html.Node) {
		walk(n, func(c *html.Node) bool {
			if c.Type == html.ElementNode && (c.Data == "figure" || c.Data == "img") {
				addFigure(c)
				return false
			}
			return true
		})
	}
	var add func(n *html.Node)
	add = func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		class := attr(n, "class")
		switch {
		case n.Data == "table":
			blocks = append(blocks, domain.Block{Kind: domain.BlockTable, Ref: r, Rows: tableRows(n)})
		case n.Data == "figure" || n.Data == "img":
			addFigure(n)
		case n.Data == "pre":
			blocks = append(blocks, domain.Block{Kind: domain.BlockLayout, Ref: r, Lines: rawLines(n)})
		case n.Data == "div" && strings.Contains(class, "line-block"):
			// line-block は「| 行」の書式で、この資料では地の文の段落にも
			// コンソール出力や設定例にも使われている。行の中身で見分ける
			// (PDF 前段の classifyLine と同じ判定)。全行が地の文なら段落、
			// 1 行でも版面なら塊ごと囲う。
			lines := rawLines(n)
			prose := len(lines) > 0
			for _, ln := range lines {
				if classifyLine(ln) == lineLayout {
					prose = false
					break
				}
			}
			if prose {
				var pl []string
				for _, ln := range lines {
					pl = append(pl, ln, "")
				}
				addProse(pl)
				return
			}
			blocks = append(blocks, domain.Block{Kind: domain.BlockLayout, Ref: r, Lines: lines})
		case n.Data == "div" && strings.Contains(class, "toctree-wrapper"):
			// 目次は本文ではない
		case n.Data == "div" || n.Data == "blockquote" || n.Data == "aside" && strings.Contains(class, "footnote-list"):
			// 入れ物は中身を順に見る (admonition は下で 1 塊として扱う)
			if strings.Contains(class, "admonition") {
				addProse(proseLines(n))
				nestedFigures(n)
				return
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				add(c)
			}
		default:
			addProse(proseLines(n))
			nestedFigures(n)
		}
	}
	for _, n := range nodes {
		add(n)
	}
	return blocks
}

// --- 表 ---

// tableRows は <table> を行列にする。先頭行が見出し。
//
// colspan / rowspan は覆う範囲のセル全部に同じ値を繰り返す。空けておくと、
// 列名で引いたときに隣の列の値を返してしまう (諸元表で機種の列が結合されている)。
func tableRows(t *html.Node) [][]string {
	var rows [][]string
	pending := map[[2]int]string{} // rowspan で下の行に持ち越す値
	ri := 0
	walk(t, func(n *html.Node) bool {
		if n.Type != html.ElementNode || n.Data != "tr" {
			return true
		}
		var row []string
		col := 0
		take := func() {
			for {
				v, ok := pending[[2]int{ri, col}]
				if !ok {
					return
				}
				delete(pending, [2]int{ri, col})
				row = append(row, v)
				col++
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode || (c.Data != "td" && c.Data != "th") {
				continue
			}
			take()
			text := cellText(c)
			cs, _ := strconv.Atoi(attr(c, "colspan"))
			rs, _ := strconv.Atoi(attr(c, "rowspan"))
			cs, rs = max(cs, 1), max(rs, 1)
			for i := 0; i < cs; i++ {
				row = append(row, text)
				for k := 1; k < rs; k++ {
					pending[[2]int{ri + k, col}] = text
				}
				col++
			}
		}
		take()
		rows = append(rows, row)
		ri++
		return false
	})
	return rows
}

// cellText はセルの中身。段落 (<p>) や行ブロックの行は改行で区切る。
// 「pki cert export bundle / pki cert export pem」のように 1 つのセルに
// コマンドを 2 つ並べた表があり、空白で繋ぐと 1 つのコマンドに見える。
// 改行は renderTableBlock が <br> に、diff は行に分けて読む。
func cellText(c *html.Node) string {
	var parts []string
	for n := c.FirstChild; n != nil; n = n.NextSibling {
		if n.Type == html.ElementNode && n.Data == "div" && strings.Contains(attr(n, "class"), "line-block") {
			parts = append(parts, rawLines(n)...)
			continue
		}
		if t := domain.Collapse(nodeText(n)); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n")
}

// --- 本文行 ---

// rawLines は構文・コンソール出力のように 1 行 1 行に意味がある要素の行。
// line-block なら <div class="line"> ごと、<pre> なら改行ごと。
//
// 長い構文は原稿で行を分けてあり、続きの行 (src SRC … / [ device … ]) は
// 入れ子の line-block になっている (reST の行ブロックで字下げした行)。
// PDF では続きが 1 段深く字下げされていて domain.CommandsOf がそれを見て繋ぐので、
// 入れ子の深さをそのまま 2 桁の字下げに写す。1 行目と先頭語が違う行を続きと
// みなす推測はしない。IKEv2 の項目のように、プロファイルモードの
// anti-replay とインタフェースモードの ikev2 anti-replay を同じ高さに
// 並べて 1 つの項目で説明するものがあり、後者も索引に載せるべき別の
// コマンドだからである。
//
// 1 行目が入れ子の深さにある欄が 1 つある (IKEv2 の local-ts: 原稿の入れ子が
// 崩れている)。そのまま写し、索引の側 (domain.CommandsOf) が 1 行目の字下げを基準にする。
func rawLines(n *html.Node) []string {
	var lines []string
	if n.Type == html.ElementNode && n.Data == "pre" {
		for _, ln := range strings.Split(strings.Trim(nodeText(n), "\n"), "\n") {
			lines = append(lines, strings.TrimRight(ln, " \t"))
		}
		return lines
	}
	found := false
	var visit func(c *html.Node, depth int)
	visit = func(c *html.Node, depth int) {
		if c.Type == html.ElementNode && c.Data == "div" {
			class := attr(c, "class")
			switch {
			case strings.Contains(class, "line-block"):
				for k := c.FirstChild; k != nil; k = k.NextSibling {
					visit(k, depth+1)
				}
				return
			case strings.Contains(class, "line"):
				// 一番外の line-block が深さ 1。その中の入れ子から字下げする。
				// ただし no で始まる行は入れ子にあっても no 形の頭とみなす
				// (tunnel keepalive の項目で no 形が続きの深さに書かれている
				// 原稿の誤り。続きの行が語 no で始まることはない)。
				text := domain.Collapse(nodeText(c))
				indent := ""
				if depth > 1 && !strings.HasPrefix(text, "no ") {
					indent = strings.Repeat("  ", depth-1)
				}
				lines = append(lines, indent+text)
				found = true
				return
			}
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			visit(k, depth)
		}
	}
	visit(n, 0)
	if !found {
		for _, ln := range proseLines(n) {
			if ln != "" {
				lines = append(lines, ln)
			}
		}
	}
	return lines
}

// proseLines は地の文を論理行にする。
//
// 各行の後ろに空行を置く。md.go の joinWrapped は空行で区切られた行を別の
// 段落として扱い、続いた行は版面の折り返しとみなして繋ぐ。Web の行は既に
// 論理行なので、空行を挟まないと段落どうしが 1 行に繋がれる。
// 見た目を整えるための空行ではないので、消してはいけない。
func proseLines(n *html.Node) []string {
	var lines []string
	emit := func(s string) {
		if s = strings.TrimRight(s, " "); strings.TrimSpace(s) != "" {
			lines = append(lines, s, "")
		}
	}
	var visit func(n *html.Node, indent string)
	visit = func(n *html.Node, indent string) {
		if n.Type == html.TextNode {
			emit(indent + domain.Collapse(n.Data))
			return
		}
		if n.Type != html.ElementNode {
			return
		}
		class := attr(n, "class")
		switch n.Data {
		case "p", "dt", "figcaption":
			emit(indent + domain.Collapse(nodeText(n)))
		case "aside":
			// 脚注 <aside class="footnote"> は "[1] 本文" の 1 行。footnote-list は入れ物。
			if strings.Contains(class, "footnote") && !strings.Contains(class, "footnote-list") {
				emit(indent + domain.Collapse(nodeText(n)))
				return
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				visit(c, indent)
			}
		case "li":
			// 箇条書きの本文 (最初の段落) と、入れ子のリスト
			var own []string
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && (c.Data == "ul" || c.Data == "ol") {
					continue
				}
				if t := domain.Collapse(nodeText(c)); t != "" {
					own = append(own, t)
				}
			}
			emit(indent + "- " + strings.Join(own, " "))
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && (c.Data == "ul" || c.Data == "ol") {
					visit(c, indent+"  ")
				}
			}
		case "ul", "ol", "dd", "dl", "blockquote", "div", "section", "span":
			if strings.Contains(class, "admonition-title") {
				emit(indent + domain.Collapse(nodeText(n)) + ":")
				return
			}
			if strings.Contains(class, "toctree-wrapper") {
				return
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				visit(c, indent)
			}
		case "table":
			// 地の文の中の表は 1 行 1 段の縦線区切り。セル内の改行は " / " に畳む。
			for _, row := range tableRows(n) {
				cells := make([]string, len(row))
				for i, c := range row {
					cells[i] = strings.ReplaceAll(c, "\n", " / ")
				}
				emit(indent + "| " + strings.Join(cells, " | ") + " |")
			}
		case "pre":
			for _, ln := range rawLines(n) {
				emit(indent + ln)
			}
		case "img", "figure":
			// 地の文の中の図は落とす (節の直下にある図は contentBlocks が拾う)
		default:
			emit(indent + domain.Collapse(nodeText(n)))
		}
	}
	visit(n, "")
	return lines
}

// --- SVG のラベル ---

// svgText は SVG の 1 つの <text>。位置は transform="matrix(1 0 0 1 X Y)" から取る
// (Office の書き出しはこの形。x / y 属性で置く書き出しも受ける)。
type svgText struct {
	x, y, size float64
	s          string
}

var svgMatrixRe = regexp.MustCompile(`matrix\(\s*([-\d.eE+]+)\s+([-\d.eE+]+)\s+([-\d.eE+]+)\s+([-\d.eE+]+)\s+([-\d.eE+]+)\s+([-\d.eE+]+)\s*\)`)

// svgLabels は SVG の <text> を読み順に並べ、行ごとにまとめる。
//
// Office から書き出した SVG は字送りの都合で 1 語が「b」「9」「:」のように
// 字ごとの <text> に割れている。文書順で繋ぐと隣のラベルまで癒着するので、
// 位置で判断する: 同じ高さで、前の字の右端からの隙間が字の大きさより小さければ
// 同じ語、隙間があれば同じ行の別の語、高さが違えば別の行。字の幅は測れない
// (フォントが無い) ので、半角は字の大きさの 0.55 倍、全角は 1 倍とみなす。
func svgLabels(file string) []string {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	dec := xml.NewDecoder(f)
	dec.Strict = false
	var items []svgText
	var stack []svgText // 入れ子の <text> / <tspan> に備える
	var cur *svgText
	var buf strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "text" {
				continue
			}
			st := svgText{size: 12}
			for _, a := range t.Attr {
				switch a.Name.Local {
				case "x":
					st.x, _ = strconv.ParseFloat(strings.Fields(a.Value + " 0")[0], 64)
				case "y":
					st.y, _ = strconv.ParseFloat(strings.Fields(a.Value + " 0")[0], 64)
				case "font-size":
					if v, err := strconv.ParseFloat(strings.TrimSuffix(a.Value, "px"), 64); err == nil {
						st.size = v
					}
				case "transform":
					if m := svgMatrixRe.FindStringSubmatch(a.Value); m != nil {
						st.x, _ = strconv.ParseFloat(m[5], 64)
						st.y, _ = strconv.ParseFloat(m[6], 64)
					}
				}
			}
			stack = append(stack, st)
			cur = &stack[len(stack)-1]
			buf.Reset()
		case xml.CharData:
			if cur != nil {
				buf.Write(t)
			}
		case xml.EndElement:
			if t.Name.Local != "text" || cur == nil {
				continue
			}
			if s := domain.Collapse(buf.String()); s != "" {
				it := *cur
				it.s = s
				items = append(items, it)
			}
			stack = stack[:len(stack)-1]
			if len(stack) > 0 {
				cur = &stack[len(stack)-1]
			} else {
				cur = nil
			}
			buf.Reset()
		}
	}
	if len(items) == 0 {
		return nil
	}
	// 読み順: 上から下、左から右。同じ行かどうかは字の大きさの半分で見る。
	sort.SliceStable(items, func(i, j int) bool {
		if d := items[i].y - items[j].y; d < -items[i].size/2 || d > items[i].size/2 {
			return items[i].y < items[j].y
		}
		return items[i].x < items[j].x
	})
	width := func(it svgText) float64 {
		w := 0.0
		for _, r := range it.s {
			if r < 0x80 {
				w += it.size * 0.55
			} else {
				w += it.size
			}
		}
		return w
	}
	var lines []string
	var ln strings.Builder
	prev := items[0]
	ln.WriteString(prev.s)
	for _, it := range items[1:] {
		sameLine := it.y-prev.y > -prev.size/2 && it.y-prev.y < prev.size/2
		gap := it.x - (prev.x + width(prev))
		switch {
		case sameLine && gap < prev.size*0.5:
			ln.WriteString(it.s) // 字送りで割れた同じ語
		case sameLine:
			ln.WriteString("  " + it.s)
		default:
			lines = append(lines, ln.String())
			ln.Reset()
			ln.WriteString(it.s)
		}
		prev = it
	}
	lines = append(lines, ln.String())
	return lines
}

// --- DOM の補助 ---

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func isHeadingTag(tag string) bool {
	return len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6'
}

// walk は深さ優先で辿る。fn が false を返した節点の子は見ない。
func walk(n *html.Node, fn func(*html.Node) bool) {
	if !fn(n) {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

func findNode(n *html.Node, pred func(*html.Node) bool) *html.Node {
	var found *html.Node
	walk(n, func(c *html.Node) bool {
		if found != nil {
			return false
		}
		if pred(c) {
			found = c
			return false
		}
		return true
	})
	return found
}

func childElement(n *html.Node, tag string) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			return c
		}
	}
	return nil
}

// nodeText は節点以下のテキストを繋ぐ。
//
// <code> は Sphinx が語ごとに <span class="pre"> に割るので、空白で繋いで
// バッククォートで括る。見出しのアンカー (headerlink) は落とす。
func nodeText(n *html.Node) string {
	var b strings.Builder
	var visit func(n *html.Node)
	visit = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
			return
		case html.ElementNode:
			class := attr(n, "class")
			switch {
			case n.Data == "a" && strings.Contains(class, "headerlink"):
				return
			case n.Data == "br":
				b.WriteString("\n")
				return
			case n.Data == "code":
				var parts []string
				walk(n, func(c *html.Node) bool {
					if c.Type == html.TextNode {
						if t := strings.TrimSpace(c.Data); t != "" {
							parts = append(parts, t)
						}
					}
					return true
				})
				b.WriteString("`" + strings.Join(parts, " ") + "`")
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(n)
	return b.String()
}
