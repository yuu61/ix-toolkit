package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// md サブコマンド: テキスト層を持つ PDF を、構造を保った Markdown に変換する。
//
// OCR も画像化も使わない。段組みとヘッダ・フッタを pdftotext のマージンで
// 落とせれば、本文はそのまま取り出せる。

// --- 中間表現 ---

// page は 1 ページ分の本文と、ヘッダ・フッタ帯から得たメタデータ。
type page struct {
	num       int      // PDF 上のページ番号 (1 始まり)
	printed   string   // 印刷ページ番号 (例 "3-29")
	chapter   int      // 印刷ページ番号の章部分
	section   string   // 版面ヘッダ = 節名
	lines     []string // 読み順に並べた本文行
	fullLines []string // ページ全幅で読んだ行 (段をまたぐ大見出し用)
	twoColumn bool
}

// entry は entryMarker で始まる 1 コマンド項目。
type entry struct {
	title   string
	chapter int
	section string
	pageRef []string // 印刷ページ番号 (ページをまたぐ場合は複数)
	fields  []field
	cmds    []string // 入力形式から抜いたコマンド行 = 索引のキー
	anchor  string
}

type field struct {
	label string
	lines []string
}

// sectionKey は出力ファイルの単位。章 + 節で 1 ファイルにする。
type sectionKey struct {
	chapter int
	section string
}

func runMD(args []string) {
	fs := flag.NewFlagSet("md", flag.ExitOnError)
	profilePath := fs.String("profile", "", "プロファイル JSON (省略時は NEC IX CRM 用の既定値)")
	outDir := fs.String("out", "out", "出力ディレクトリ")
	title := fs.String("title", "", "資料タイトル (省略時は PDF のファイル名)")
	source := fs.String("source", "", "出典表記 (取得元 URL 等。README に記載する)")
	pos := parseFlags(fs, args)

	if len(pos) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: pdfbook md <pdf> [-profile profile.json] [-out out/]")
		os.Exit(1)
	}
	pdf := pos[0]

	if err := pdftotextAvailable(); err != nil {
		fatal(err)
	}

	p := DefaultProfile()
	if *profilePath != "" {
		var err error
		if p, err = LoadProfile(*profilePath); err != nil {
			fatal(err)
		}
	}

	docTitle := *title
	if docTitle == "" {
		docTitle = strings.TrimSuffix(baseName(pdf), filepath.Ext(pdf))
	}

	fmt.Printf("読み込み: %s (プロファイル %s)\n", pdf, p.Name)
	pages, stats, err := readPages(p, pdf)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("  ページ数: %d (2段組み %d / 全幅 %d)\n", len(pages), stats.two, stats.one)

	chapters := collectChapterTitles(pages)
	entries := parseEntries(p, pages)
	fmt.Printf("  抽出項目: %d 件 / 章: %d\n", len(entries), len(chapters))

	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "\n⚠ 項目を 1 件も抽出できませんでした。")
		fmt.Fprintf(os.Stderr, "  プロファイルの entryMarker (%q) と fieldLabels が資料に合っているか確認してください。\n", p.EntryMarker)
		os.Exit(2)
	}

	if err := writeAll(*outDir, docTitle, *source, pdf, p, chapters, entries); err != nil {
		fatal(err)
	}
	fmt.Printf("\n出力しました: %s\n", *outDir)
	fmt.Printf("  索引:         %s\n", filepath.Join(*outDir, "index.md"))
	fmt.Printf("  機械可読索引: %s\n", filepath.Join(*outDir, "commands.tsv"))
}

// --- 読み込み ---

type pageStats struct{ one, two int }

// readPages は高々 5 回の pdftotext 起動で 1 冊を読み切り、
// ページごとに段組みを判定して読み順の行列を作る。
func readPages(p *Profile, pdf string) ([]page, pageStats, error) {
	left, right, full, err := p.ExtractColumns(pdf)
	if err != nil {
		return nil, pageStats{}, err
	}
	headers, footers, err := p.ExtractBands(pdf)
	if err != nil {
		return nil, pageStats{}, err
	}

	var stats pageStats
	pages := make([]page, 0, len(full))
	for i := range full {
		pg := page{num: i + 1}
		pg.section = at(headers, i)
		pg.printed = firstToken(at(footers, i))
		pg.chapter = chapterOf(pg.printed)

		pg.fullLines = splitLines(full[i])

		// 段組み判定は目視ではなく不変条件で行う:
		//   左カラムの文字数 + 右カラムの文字数 == ページ全体の文字数
		// 一致しないページ (目次のリーダ罫、段抜きの図表) は全幅で扱う。
		//
		// 段間の空白帯を覗いて「段をまたぐ行があるか」も試したが、本文ページでも
		// 1 文字だけ帯に掛かることがあり、そのページ全体が全幅と誤判定されて
		// 左右の段が交互に混ざった。帯の判定は使わない。ページ幅いっぱいの
		// 大見出しを持つのは章扉・節扉だけなので、そちらは下の isNavigation で
		// 本文の流れから外して対処する。
		if p.Columns >= 2 && i < len(left) && i < len(right) {
			l, r, f := at(left, i), at(right, i), full[i]
			nf := countGlyphs(f)
			if abs(countGlyphs(l)+countGlyphs(r)-nf) <= columnTolerance(nf) && countGlyphs(r) > 0 {
				pg.twoColumn = true
				// 読み順は左段を読み切ってから右段 (検証済み)
				pg.lines = append(splitLines(l), splitLines(r)...)
				stats.two++
			}
		}
		if !pg.twoColumn {
			pg.lines = splitLines(full[i])
			stats.one++
		}
		pages = append(pages, pg)
	}
	return pages, stats, nil
}

// columnTolerance は段組み判定で許す文字数のずれ。
//
// 完全一致を求めると、段間にわずかに掛かった数文字のせいで本文ページが全幅と
// 判定され、左右の段が 1 行に混ざって丸ごと壊れる。実測すると本文ページの
// ずれは高々十数文字だったのに対し、目次・索引はリーダ罫が段をまたぐため
// 130 文字前後ずれる。両者のあいだには大きな隔たりがあるので、
// ページの 2% (下限 4, 上限 24 文字) を境にすれば取り違えない。
func columnTolerance(pageGlyphs int) int {
	return min(24, max(4, pageGlyphs/50))
}

func splitLines(s string) []string {
	out := strings.Split(s, "\n")
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

func at(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

func firstToken(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

var printedRe = regexp.MustCompile(`^(\d+)-\d+$`)

func chapterOf(printed string) int {
	if m := printedRe.FindStringSubmatch(printed); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// --- 章タイトル ---

var chapterTitleRe = regexp.MustCompile(`^(\d+)\.\s*(\S.*?)\s*$`)

// collectChapterTitles は章扉ページ (印刷ページ番号が "N-1") から章名を拾う。
// 目次を解析しなくても、版面そのものが構造を持っている。
func collectChapterTitles(pages []page) map[int]string {
	out := map[int]string{}
	for _, pg := range pages {
		if pg.chapter == 0 || !strings.HasSuffix(pg.printed, "-1") {
			continue
		}
		// 章扉の大見出しはページ幅いっぱいに伸びており、段組みで読むと
		// 「2. グローバル」「コンフィグ編」に割れる。全幅で読んだ行を使う。
		for _, ln := range pg.fullLines {
			m := chapterTitleRe.FindStringSubmatch(strings.TrimSpace(ln))
			if m == nil {
				continue
			}
			if n, _ := strconv.Atoi(m[1]); n == pg.chapter {
				out[n] = m[2]
				break
			}
		}
	}
	return out
}

// --- 項目の切り出し ---

func parseEntries(p *Profile, pages []page) []entry {
	labels := map[string]bool{}
	for _, l := range p.FieldLabels {
		labels[l] = true
	}

	var entries []entry
	var cur *entry
	var curField *field

	flushField := func() {
		if cur != nil && curField != nil {
			cur.fields = append(cur.fields, *curField)
		}
		curField = nil
	}
	flushEntry := func() {
		flushField()
		if cur != nil {
			entries = append(entries, *cur)
		}
		cur = nil
	}

	for _, pg := range pages {
		if isNavigation(&pg, p.EntryMarker, labels) {
			continue
		}
		for _, raw := range pg.lines {
			t := strings.TrimSpace(raw)
			if t == "" {
				if curField != nil {
					curField.lines = append(curField.lines, "")
				}
				continue
			}

			// 新しい項目の開始
			if p.EntryMarker != "" && strings.HasPrefix(t, p.EntryMarker) {
				flushEntry()
				cur = &entry{
					title:   strings.TrimSpace(strings.TrimPrefix(t, p.EntryMarker)),
					chapter: pg.chapter,
					section: pg.section,
					pageRef: []string{pg.printed},
				}
				continue
			}
			if cur == nil {
				continue // 項目の外 (目次・章扉など) は無視
			}
			// 項目がページ・段をまたいだら出典ページを足す
			if n := len(cur.pageRef); n > 0 && cur.pageRef[n-1] != pg.printed && pg.printed != "" {
				cur.pageRef = append(cur.pageRef, pg.printed)
			}

			if label, rest, ok := splitLabel(t, p.FieldLabels); ok {
				flushField()
				curField = &field{label: label}
				if rest != "" {
					// 見出し語と同じ行に載ってしまった中身を拾う
					curField.lines = append(curField.lines, rest)
				}
				continue
			}
			if curField != nil {
				curField.lines = append(curField.lines, raw)
			}
		}
	}
	flushEntry()

	for i := range entries {
		entries[i].cmds = commandsOf(&entries[i])
		entries[i].anchor = anchorFor(&entries[i], i)
	}
	return entries
}

// splitLabel は行頭の見出し語を切り出し、同じ行に残った中身を返す。
//
// 版面の都合で、見出し語とは無関係な断片が同じ行に載ることがまれにある
// (852 ページ中 7 箇所。例: "デフォルト値   IX3315")。完全一致だけを見ていると
// その行が見出しとして認識されず、丸ごと直前の見出しの中身に紛れ込む。
// 見出し語のあとに 2 文字以上の空白が続く場合だけ、見出し + 中身として扱う。
func splitLabel(t string, fieldLabels []string) (label, rest string, ok bool) {
	for _, l := range fieldLabels {
		if t == l {
			return l, "", true
		}
		if r, found := strings.CutPrefix(t, l); found && strings.HasPrefix(r, "  ") {
			return l, strings.TrimSpace(r), true
		}
	}
	return "", "", false
}

// isNavigation は目次・索引・章扉・節扉のような案内ページを見分ける。
//
// 項目の始まりを示す記号も、項目内の見出し語も 1 つも無いページには、
// 項目パーサが使える情報が無い。それでも本文の流れに混ぜると、直前の項目の
// 開きっぱなしの見出しに目次の行が流れ込んで中身が汚れる。
// この資料では、そうしたページは前付け・章扉・節扉・目次・索引だけだった
// (本文が続くだけのページには必ずいずれかの見出し語が現れる)。
func isNavigation(pg *page, marker string, labels map[string]bool) bool {
	for _, raw := range pg.lines {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if marker != "" && strings.HasPrefix(t, marker) {
			return false
		}
		if labels[t] {
			return false
		}
	}
	return true
}

// syntaxLabels は「コマンド構文そのもの」を載せる見出し語。
// ここだけはコードブロックにし、行の折り返しをいじらない。
var syntaxLabels = map[string]bool{"入力形式": true, "入力例": true}

// commandsOf は入力形式からコマンド行を取り出す (no 形は除く)。
//
// 長い構文は版面で折り返されており、その続きは 1 段深く字下げされている。
// 折り返し行をそのまま拾うと "128][aes-cbc-192]..." のような断片が索引に並ぶので、
// 字下げの深い行は直前のコマンドに繋ぎ直す。
func commandsOf(e *entry) []string {
	var joined []string
	for _, f := range e.fields {
		if f.label != "入力形式" {
			continue
		}
		for _, ln := range dedent(f.lines) {
			t := strings.TrimSpace(ln)
			if t == "" {
				continue
			}
			// コマンドは必ず英小文字で始まる。字下げが深い行と、
			// 英小文字で始まらない行 ("ADDRESS]" のような折り返しの後半) は
			// 直前のコマンドの続きとして繋ぎ直す。
			if indentOf(ln) > 0 || !startsLowerASCII(t) {
				if len(joined) == 0 {
					continue // 繋ぐ先が無い断片 = 版面のにじみ。索引には載せない
				}
				joined[len(joined)-1] += " " + t
				continue
			}
			joined = append(joined, t)
		}
	}

	var out []string
	seen := map[string]bool{}
	for _, c := range joined {
		if strings.HasPrefix(c, "no ") || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

func indentOf(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

func startsLowerASCII(s string) bool {
	r := []rune(s)
	return len(r) > 0 && r[0] >= 'a' && r[0] <= 'z'
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func anchorFor(e *entry, i int) string {
	base := ""
	if len(e.cmds) > 0 {
		base = e.cmds[0]
	}
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(base), "-"), "-")
	if s == "" {
		return fmt.Sprintf("entry-%d", i+1)
	}
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	return "cmd-" + s
}

// --- 整形 ---

// dedent は共通の字下げを取り除く。-layout はページ上の x 座標をそのまま
// 空白で表現するので、そのままだと全行が深く字下げされている。
func dedent(lines []string) []string {
	indent := -1
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		n := len(ln) - len(strings.TrimLeft(ln, " "))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	if indent <= 0 {
		return lines
	}
	out := make([]string, len(lines))
	for i, ln := range lines {
		if len(ln) >= indent {
			out[i] = ln[indent:]
		} else {
			out[i] = strings.TrimLeft(ln, " ")
		}
	}
	return out
}

var bulletPrefix = []string{"•", "・", "※", "‒", "–", "—", "-", "*"}

func startsNewUnit(s string) bool {
	for _, b := range bulletPrefix {
		if strings.HasPrefix(s, b) {
			return true
		}
	}
	// "NAME：：：..." のようなパラメータ名の定義行
	return strings.Contains(s, "：：：")
}

// joinWrapped は版面の折り返しを畳んで 1 論理行に戻す。
//
// 日本語は行末で単語が割れる (「最大レ」+「コード数」) ため、素朴に改行を
// 残すと grep が当たらなくなる。逆に英字どうしは空白で繋がないと単語が
// くっついてしまう。
func joinWrapped(lines []string) []string {
	var out []string
	var buf string

	flush := func() {
		if strings.TrimSpace(buf) != "" {
			out = append(out, strings.TrimSpace(buf))
		}
		buf = ""
	}

	for _, raw := range lines {
		t := strings.TrimSpace(raw)
		if t == "" {
			flush()
			continue
		}
		if buf == "" {
			buf = t
			continue
		}
		if startsNewUnit(t) || endsSentence(buf) {
			flush()
			buf = t
			continue
		}
		buf += joiner(buf, t) + t
	}
	flush()
	return out
}

func endsSentence(s string) bool {
	r := []rune(strings.TrimSpace(s))
	if len(r) == 0 {
		return false
	}
	switch r[len(r)-1] {
	case '。', '！', '？', '!', '?':
		return true
	}
	return false
}

// joiner は 2 行を繋ぐときに空白を挟むかどうかを決める。
// 半角英数どうしのときだけ空白が要る。
func joiner(prev, next string) string {
	pr := []rune(prev)
	nr := []rune(next)
	if len(pr) == 0 || len(nr) == 0 {
		return ""
	}
	if isASCIIWord(pr[len(pr)-1]) && isASCIIWord(nr[0]) {
		return " "
	}
	return ""
}

func isASCIIWord(r rune) bool {
	return r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

// --- 出力 ---

func writeAll(outDir, docTitle, source, pdf string, p *Profile,
	chapters map[int]string, entries []entry,
) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	// 章 → 節 → 項目 に束ねる (出現順を保つ)
	var order []sectionKey
	grouped := map[sectionKey][]*entry{}
	for i := range entries {
		k := sectionKey{entries[i].chapter, entries[i].section}
		if _, ok := grouped[k]; !ok {
			order = append(order, k)
		}
		grouped[k] = append(grouped[k], &entries[i])
	}

	// 節ごとに 1 ファイル。852 ページを 1 ファイルにすると skill から引けない。
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
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n", k.section)
		if t := chapters[k.chapter]; t != "" {
			fmt.Fprintf(&b, "> %d. %s — %s\n\n", k.chapter, t, docTitle)
		}
		for _, e := range grouped[k] {
			renderEntry(&b, e)
		}
		if err := os.WriteFile(full, []byte(b.String()), 0o644); err != nil {
			return err
		}
	}

	if err := writeIndex(outDir, docTitle, chapters, order, grouped, relPath, entries); err != nil {
		return err
	}
	return writeReadme(outDir, docTitle, source, pdf, p, len(entries), len(order))
}

func renderEntry(b *strings.Builder, e *entry) {
	fmt.Fprintf(b, "<a id=%q></a>\n\n", e.anchor)
	fmt.Fprintf(b, "## %s\n\n", e.title)
	if len(e.pageRef) > 0 && e.pageRef[0] != "" {
		fmt.Fprintf(b, "*%s p.%s*\n\n", e.section, strings.Join(e.pageRef, ", "))
	}
	for _, f := range e.fields {
		lines := dedent(f.lines)
		if syntaxLabels[f.label] {
			body := strings.Trim(strings.Join(lines, "\n"), "\n ")
			if body == "" {
				continue
			}
			fmt.Fprintf(b, "**%s**\n\n```\n%s\n```\n\n", f.label, body)
			continue
		}
		joined := joinWrapped(lines)
		if len(joined) == 0 {
			continue
		}
		fmt.Fprintf(b, "**%s**\n\n", f.label)
		for _, ln := range joined {
			fmt.Fprintf(b, "%s\n\n", ln)
		}
	}
	fmt.Fprintf(b, "---\n\n")
}

func writeIndex(outDir, docTitle string, chapters map[int]string, order []sectionKey,
	grouped map[sectionKey][]*entry, relPath map[sectionKey]string, entries []entry,
) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — 索引\n\n", docTitle)
	fmt.Fprintf(&b, "全 %d 項目。コマンド名から該当箇所を引くための索引。\n\n", len(entries))

	// --- コマンド索引 (skill がこれを引く) ---
	type row struct{ cmd, title, link, page string }
	var rows []row
	for i := range entries {
		e := &entries[i]
		link := strings.ReplaceAll(relPath[sectionKey{e.chapter, e.section}], "\\", "/")
		pg := ""
		if len(e.pageRef) > 0 {
			pg = e.pageRef[0]
		}
		for _, c := range e.cmds {
			rows = append(rows, row{c, e.title, link + "#" + e.anchor, pg})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].cmd < rows[j].cmd })

	fmt.Fprintln(&b, "## コマンド索引")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| コマンド | 項目 | ページ | 参照 |")
	fmt.Fprintln(&b, "|---|---|---|---|")
	for _, r := range rows {
		fmt.Fprintf(&b, "| `%s` | %s | %s | [→](%s) |\n", r.cmd, escapePipe(r.title), r.page, r.link)
	}

	// --- 目次 ---
	fmt.Fprintf(&b, "\n## 目次\n")
	lastCh := -1
	for _, k := range order {
		if k.chapter != lastCh {
			fmt.Fprintf(&b, "\n### %d. %s\n\n", k.chapter, chapters[k.chapter])
			lastCh = k.chapter
		}
		link := strings.ReplaceAll(relPath[k], "\\", "/")
		fmt.Fprintf(&b, "- [%s](%s) — %d 項目\n", k.section, link, len(grouped[k]))
	}

	if err := os.WriteFile(filepath.Join(outDir, "index.md"), []byte(b.String()), 0o644); err != nil {
		return err
	}

	// --- 機械可読索引 ---
	var t strings.Builder
	fmt.Fprintln(&t, "command\tentry\tfile\tanchor\tpage")
	for _, r := range rows {
		file, anchor, _ := strings.Cut(r.link, "#")
		fmt.Fprintf(&t, "%s\t%s\t%s\t%s\t%s\n", r.cmd, r.title, file, anchor, r.page)
	}
	return os.WriteFile(filepath.Join(outDir, "commands.tsv"), []byte(t.String()), 0o644)
}

func writeReadme(outDir, docTitle, source, pdf string, p *Profile, nEntries, nSections int) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s (Markdown 変換版)\n\n", docTitle)
	fmt.Fprintln(&b, "この配下は PDF から機械変換した生成物であり、著作権は原著作者に帰属する。")
	fmt.Fprintln(&b, "**再配布しないこと。** リポジトリでは `.gitignore` により除外されている。")
	fmt.Fprintf(&b, "\n## 生成条件\n\n")
	fmt.Fprintf(&b, "- 元 PDF: `%s`\n", baseName(pdf))
	if source != "" {
		fmt.Fprintf(&b, "- 取得元: %s\n", source)
	}
	fmt.Fprintf(&b, "- プロファイル: `%s` (%d 段組み, 天地マージン %.0f/%.0f pt)\n",
		p.Name, p.Columns, p.MarginTop, p.MarginBottom)
	fmt.Fprintf(&b, "- 抽出項目数: %d / 節数: %d\n", nEntries, nSections)
	fmt.Fprintln(&b, "- 変換経路: pdftotext のテキスト層 (OCR 不使用)")
	fmt.Fprintf(&b, "\n## 引き方\n\n")
	fmt.Fprintln(&b, "- `index.md` — コマンド索引と目次")
	fmt.Fprintln(&b, "- `commands.tsv` — `command / entry / file / anchor / page` のタブ区切り索引")
	fmt.Fprintln(&b, "- `chNN-<章名>/<節名>.md` — 本文")
	return os.WriteFile(filepath.Join(outDir, "README.md"), []byte(b.String()), 0o644)
}

var unsafeName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func safeName(s string) string {
	s = unsafeName.ReplaceAllString(strings.TrimSpace(s), "-")
	s = strings.Trim(s, " .")
	if s == "" {
		return "無題"
	}
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60])
	}
	return s
}

func escapePipe(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
