package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// diff サブコマンド。無印 (ix) と IX-R (ix-r) の変換結果から、系列間のコマンド
// 対応表 diff.tsv を作る。ix-manual skill が「無印の設定を IX-R に移す」「無印で
// 覚えたコマンドが IX-R に無い」ときに引く。
//
// 行の出どころは 3 つで、source 列で区別する。確度が違うので混ぜない。
//
//   - ch8:     IX-R 機能説明書 8 章「IXシリーズとの差分」の表の書き起こし。一次情報。
//              変換結果の Markdown の表 (GFM) を節ごとに読む。節の題の語で種類を
//              決め、列は見出し行の名で取る。節番号は版で動くので使わない。
//   - derived: 両系列の制限事項を読み比べて手で導いた行。profiles/ 配下の TSV を
//              そのまま写す。版の組が合っているかは人が見る (ファイル頭に書いてある)。
//   - setdiff: 両索引 (commands.tsv) の集合差。族 (先頭の語) ごとに、無印にあって
//              IX-R に 1 つも無いものだけを出す。目安であって、個々のコマンドが
//              本当に無いかは commands.tsv で確かめる。ch8 / derived で既に
//              挙がっている族は出さない。
//
// 列: kind / ix / ix-r / source / ref_ix / ref_ixr / note
//
//	kind    renamed / moved / removed / range / changed / limit / absent
//	ix      無印のコマンド
//	ix-r    IX-R のコマンド (置き換え先)。廃止・非対応なら空
//	ref_*   出典。crm:<source> か fd:<source> (source は各索引の source 列と同じ形)
//	note    根拠・変更内容。改行は " / " に畳む

type diffRow struct {
	kind, ix, ixr, source, refIX, refIXR, note string
}

const diffHeader = "kind\tix\tix-r\tsource\tref_ix\tref_ixr\tnote"

func runDiff(args []string) {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	out := fs.String("out", "", "出力ファイル (省略で <ix-r>/diff.tsv)")
	derived := fs.String("derived", "", "手で導いた差分の TSV (省略で profiles/ix-r-derived-diff.tsv、無ければ読まない)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "使い方: manualbook diff <無印の変換結果> <IX-R の変換結果> [-out diff.tsv]")
		fmt.Fprintln(os.Stderr, "  例:   manualbook diff ~/.ix-toolkit/manuals/ix ~/.ix-toolkit/manuals/ix-r")
		fmt.Fprintln(os.Stderr, "  どちらも crm/ と fd/ を持つ系列ディレクトリ。手で導いた差分 (profiles/ix-r-derived-diff.tsv) は")
		fmt.Fprintln(os.Stderr, "  カレントか実行ファイルの隣から読む。リポジトリの外から流すなら -derived で指定する。")
		fs.PrintDefaults()
	}
	pos := parseFlags(fs, args)
	if len(pos) != 2 {
		fs.Usage()
		os.Exit(2)
	}
	ixDir, ixrDir := pos[0], pos[1]
	if *out == "" {
		*out = filepath.Join(ixrDir, "diff.tsv")
	}

	ixCmds, err := readCommandIndex(filepath.Join(ixDir, "crm", "commands.tsv"))
	if err != nil {
		fatal(err)
	}
	ixrCmds, err := readCommandIndex(filepath.Join(ixrDir, "crm", "commands.tsv"))
	if err != nil {
		fatal(err)
	}

	ch8, err := readCh8(filepath.Join(ixrDir, "fd"), ixCmds)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("ch8:     %d 行 (IX-R 機能説明書「IXシリーズとの差分」)\n", len(ch8))

	var hand []diffRow
	path := *derived
	if path == "" {
		path = findDerived()
	}
	if path != "" {
		if hand, err = readDerived(path); err != nil {
			fatal(err)
		}
		fmt.Printf("derived: %d 行 (%s)\n", len(hand), path)
	} else {
		// 黙って 0 行にすると、できた diff.tsv は欠けているのに完全に見える。
		fmt.Fprintln(os.Stderr, "⚠ derived: profiles/ix-r-derived-diff.tsv が見つからない (カレントにも実行ファイルの隣にも無い)。")
		fmt.Fprintln(os.Stderr, "  リポジトリの外から流すときは -derived <リポジトリ>/profiles/ix-r-derived-diff.tsv を付ける。")
		fmt.Println("derived: 0 行")
	}

	known := append(append([]diffRow{}, ch8...), hand...)
	sd := setDiff(ixCmds, ixrCmds, known)
	fmt.Printf("setdiff: %d 族 (無印にあって IX-R の索引に無いコマンド群)\n", len(sd))

	rows := append(known, sd...)
	var b strings.Builder
	b.WriteString(diffHeader + "\n")
	for _, r := range rows {
		b.WriteString(strings.Join([]string{
			r.kind, tsvCell(r.ix), tsvCell(r.ixr), r.source, r.refIX, r.refIXR, tsvCell(r.note),
		}, "\t") + "\n")
	}
	if err := os.WriteFile(*out, []byte(b.String()), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("出力しました: %s (%d 行)\n", *out, len(rows))
}

// findDerived は手で導いた差分の TSV を、カレント → 実行ファイルの隣の順に探す。
// リポジトリ直下でビルドして流す想定なので、見つからなければ空を返して呼び手が警告する。
func findDerived() string {
	rel := filepath.Join("profiles", "ix-r-derived-diff.tsv")
	if _, err := os.Stat(rel); err == nil {
		return rel
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), rel)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func tsvCell(s string) string {
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\n", " / ")
	return collapse(s)
}

// --- 索引 ---

type indexedCommand struct {
	cmd, entry, source string
}

func readCommandIndex(path string) ([]indexedCommand, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var cmds []indexedCommand
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	first := true
	for sc.Scan() {
		if first {
			first = false
			if !strings.HasPrefix(sc.Text(), "command\t") {
				return nil, fmt.Errorf("%s: commands.tsv の見出し行ではない: %q", path, sc.Text())
			}
			continue
		}
		c := strings.Split(sc.Text(), "\t")
		if len(c) < 5 {
			continue
		}
		cmds = append(cmds, indexedCommand{cmd: c[0], entry: c[1], source: c[4]})
	}
	return cmds, sc.Err()
}

// lookupCommand は表に書かれたコマンド (パラメータ名や省略を含む) を索引で引く。
// 語を先頭から突き合わせ、パラメータ (英大文字・括弧) に当たるまでの語が
// 片方をもう片方が覆う索引行のうち、一致した語が最も多いものを返す。
// 索引側が短くてよいのは「pki cert erase { name NAME | all }」のように索引の
// 構文が表より早く括弧に入る場合のため。ただし 1 語だけの一致は取らない
// (ip や show で始まる行は何にでも当たる)。
func lookupCommand(cmds []indexedCommand, text string) (indexedCommand, bool) {
	want := keywords(text)
	if len(want) == 0 {
		return indexedCommand{}, false
	}
	best, bestN := indexedCommand{}, 0
	for _, c := range cmds {
		have := keywords(c.cmd)
		n := 0
		for n < len(want) && n < len(have) && want[n] == have[n] {
			n++
		}
		covers := n == len(want) || (n == len(have) && n >= 2)
		if covers && n > bestN {
			best, bestN = c, n
		}
	}
	return best, bestN > 0
}

// keywords はコマンドの先頭から、キーワード (英小文字・数字・ハイフン) が続く
// 範囲の語を返す。最初のパラメータ・括弧・記号で止まる。
func keywords(cmd string) []string {
	var out []string
	for _, w := range strings.Fields(cmd) {
		if !isKeyword(w) {
			break
		}
		out = append(out, w)
	}
	return out
}

func isKeyword(w string) bool {
	if w == "" {
		return false
	}
	for _, r := range w {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.' || r == '/') {
			return false
		}
	}
	return r0Lower(w)
}

func r0Lower(w string) bool { return w[0] >= 'a' && w[0] <= 'z' }

// --- ch8 (IX-R 機能説明書 8 章) ---

// ch8Kinds は節の題に含まれる語から行の種類を決める。8 章の構成 (1.5a):
//
//	8.3.1 廃止されたコマンド                     → removed
//	8.3.2 コマンド表現の変更 (設定/表示/クリア)   → renamed
//	8.3.3 登録するモードに変更があるコマンド      → moved
//	8.3.4 デフォルト値・設定範囲の変更があるコマンド → range
//	8.3.5 その他変更があるコマンド                → changed
//
// 8.1 (パスワードの引継ぎ) と 8.2 (IX 互換モード) は表の形をしていないので読まない。
var ch8Kinds = []struct{ word, kind string }{
	{"廃止", "removed"},
	{"表現の変更", "renamed"},
	{"モード", "moved"},
	{"デフォルト値", "range"},
	{"設定範囲", "range"},
	{"その他変更", "changed"},
}

var (
	mdHeadingRe = regexp.MustCompile(`^(#{2,6}) (\S+) (.*)$`)
	mdTableRe   = regexp.MustCompile(`^\|.*\|$`)
	mdRuleRe    = regexp.MustCompile(`^\|(\s*:?-+:?\s*\|)+$`)
)

// readCh8 は IX-R 機能説明書の変換結果から「IXシリーズとの差分」の章を探し、
// 表を行に写す。
func readCh8(fdDir string, ixCmds []indexedCommand) ([]diffRow, error) {
	secs, err := readSectionIndex(filepath.Join(fdDir, "sections.tsv"))
	if err != nil {
		return nil, err
	}
	var file string
	refAt := map[int]string{} // 見出し行 → source
	for _, s := range secs {
		if strings.Contains(s.title, "IXシリーズとの差分") && !strings.Contains(s.section, ".") {
			file = s.file
		}
	}
	if file == "" {
		return nil, fmt.Errorf("%s: 章「IXシリーズとの差分」が sections.tsv に無い", fdDir)
	}
	for _, s := range secs {
		if s.file == file {
			refAt[s.line] = s.source
		}
	}
	data, err := os.ReadFile(filepath.Join(fdDir, filepath.FromSlash(file)))
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")

	var rows []diffRow
	kindAt := map[int]string{} // 見出しの深さ → 種類 (親から引き継ぐ)
	kind, ref := "", ""
	for i := 0; i < len(lines); i++ {
		ln := strings.TrimRight(lines[i], "\r")
		if m := mdHeadingRe.FindStringSubmatch(ln); m != nil {
			depth := len(m[1])
			title := m[3]
			k := ""
			for _, c := range ch8Kinds {
				if strings.Contains(title, c.word) {
					k = c.kind
					break
				}
			}
			if k == "" {
				k = kindAt[depth-1]
			}
			for d := depth; d <= 6; d++ {
				delete(kindAt, d)
			}
			kindAt[depth] = k
			kind = k
			if r, ok := refAt[i+1]; ok {
				ref = "fd:" + r
			}
			continue
		}
		if kind == "" || !mdTableRe.MatchString(ln) {
			continue
		}
		// 表の始まり。見出し行・罫線・本体の順。
		var table [][]string
		for i < len(lines) && mdTableRe.MatchString(strings.TrimRight(lines[i], "\r")) {
			t := strings.TrimRight(lines[i], "\r")
			if !mdRuleRe.MatchString(t) {
				table = append(table, splitTableRow(t))
			}
			i++
		}
		i--
		if len(table) < 2 {
			continue
		}
		rows = append(rows, ch8Rows(kind, ref, table, ixCmds)...)
	}
	return rows, nil
}

type indexedSection struct {
	section, title, file, source string
	line                         int
}

func readSectionIndex(path string) ([]indexedSection, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var secs []indexedSection
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		c := strings.Split(sc.Text(), "\t")
		if len(c) < 5 {
			continue
		}
		n, _ := strconv.Atoi(c[3])
		secs = append(secs, indexedSection{section: c[0], title: c[1], file: c[2], line: n, source: c[4]})
	}
	return secs, sc.Err()
}

// splitTableRow は GFM の表の 1 行をセルに分ける。renderTableBlock の逆で、
// `\|` はセル内の縦線、<br> はセル内の改行。
func splitTableRow(ln string) []string {
	ln = strings.TrimSuffix(strings.TrimPrefix(ln, "|"), "|")
	const esc = "\x00"
	ln = strings.ReplaceAll(ln, `\|`, esc)
	parts := strings.Split(ln, "|")
	for i, p := range parts {
		p = strings.ReplaceAll(p, esc, "|")
		p = strings.ReplaceAll(p, "<br>", "\n")
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

// ch8Rows は表 1 つを行にする。列は見出し行の名で取る。
//
//	removed: 機能 | 設定コマンド | IXシリーズ | IX-Rシリーズ
//	renamed: 機能 | IXシリーズ | IX-Rシリーズ | [備考]
//	moved:   機能 | 設定コマンド | IXシリーズ (モード) | IX-Rシリーズ (モード)
//	range / changed: コマンドの機能 | 対象コマンド | IXシリーズからの変更内容 | [補足]
func ch8Rows(kind, ref string, table [][]string, ixCmds []indexedCommand) []diffRow {
	head := table[0]
	col := func(names ...string) int {
		for _, n := range names {
			for i, h := range head {
				if h == n {
					return i
				}
			}
		}
		return -1
	}
	cell := func(row []string, i int) string {
		if i < 0 || i >= len(row) {
			return ""
		}
		return row[i]
	}
	cFunc := col("機能", "コマンドの機能")
	cCmd := col("設定コマンド", "対象コマンド")
	cIX := col("IXシリーズ")
	cIXR := col("IX-Rシリーズ")
	cChange := col("IXシリーズからの変更内容")
	cNote := col("備考", "補足")
	if cCmd < 0 && kind == "renamed" {
		cCmd = cIX
		cIX = -1
	}
	if cCmd < 0 {
		return nil
	}

	var rows []diffRow
	for _, row := range table[1:] {
		ixs := expandAlternatives(cellLines(cell(row, cCmd)))
		ixrs := cellLines(cell(row, cIXR))
		var note []string
		if f := cell(row, cFunc); f != "" {
			note = append(note, f)
		}
		for i, ix := range ixs {
			r := diffRow{kind: kind, ix: ix, source: "ch8", refIXR: ref}
			n := append([]string{}, note...)
			switch kind {
			case "removed":
				// 「reloadコマンドに統一」の形なら置き換え先が取れる。それ以外
				// (「UFSキャッシュに統一」「廃止」) は note を読んでもらう。
				if m := unifiedRe.FindStringSubmatch(cell(row, cIXR)); m != nil {
					r.ixr = strings.TrimSpace(m[1])
				}
				n = append(n, "IX-R: "+cell(row, cIXR))
			case "renamed":
				if i < len(ixrs) {
					r.ixr = ixrs[i]
				} else if len(ixrs) > 0 {
					r.ixr = ixrs[len(ixrs)-1]
				}
				if v := cell(row, cNote); v != "" {
					n = append(n, v)
				}
			case "moved":
				r.ixr = ix
				n = append(n, "モード: "+cell(row, cIX)+" → "+cell(row, cIXR))
			default: // range / changed
				r.ixr = ix
				if v := cell(row, cChange); v != "" {
					n = append(n, v)
				}
				if v := cell(row, cNote); v != "" {
					n = append(n, v)
				}
			}
			r.note = strings.Join(n, "。")
			if c, ok := lookupCommand(ixCmds, ix); ok {
				r.refIX = "crm:" + c.source
			}
			rows = append(rows, r)
		}
	}
	return rows
}

func cellLines(s string) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			out = append(out, t)
		}
	}
	return out
}

var unifiedRe = regexp.MustCompile(`([a-z][a-z0-9-]*(?: [a-z][a-z0-9-]*)*) ?コマンドに統一`)

var altRe = regexp.MustCompile(`[\[{] ?([a-z0-9-]+(?: \| [a-z0-9-]+)+) ?[\]}]`)

// expandAlternatives は「[ ip | ipv6 ] ufs-cache enable」のような選択肢を
// 1 つずつのコマンドに開く (ip ufs-cache enable / ipv6 ufs-cache enable)。
// 索引は 1 行 1 コマンドなので、開いておかないと引けない。
// 開くのはキーワードだけの選択肢で、パラメータを含むものはそのまま。
func expandAlternatives(cmds []string) []string {
	var out []string
	for _, c := range cmds {
		m := altRe.FindStringSubmatchIndex(c)
		if m == nil {
			out = append(out, c)
			continue
		}
		for _, alt := range strings.Split(c[m[2]:m[3]], " | ") {
			out = append(out, expandAlternatives([]string{collapse(c[:m[0]] + alt + c[m[1]:])})...)
		}
	}
	return out
}

// --- derived (手で導いた差分) ---

func readDerived(path string) ([]diffRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []diffRow
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	seenHeader := false
	for sc.Scan() {
		ln := strings.TrimRight(sc.Text(), "\r")
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		if !seenHeader {
			if ln != "kind\tix\tix-r\tref_ix\tref_ixr\tnote" {
				return nil, fmt.Errorf("%s: 見出し行が違う: %q", path, ln)
			}
			seenHeader = true
			continue
		}
		c := strings.Split(ln, "\t")
		if len(c) != 6 {
			return nil, fmt.Errorf("%s: 列が 6 つでない: %q", path, ln)
		}
		rows = append(rows, diffRow{kind: c[0], ix: c[1], ixr: c[2], source: "derived", refIX: c[3], refIXR: c[4], note: c[5]})
	}
	return rows, sc.Err()
}

// --- setdiff (索引の集合差) ---

// family はコマンドの族。先頭のキーワード 2 語 (show / clear で始まれば 3 語)。
// 「ip pim …」が全部無ければ族ごと無い、と言うための単位。
func family(cmd string) string {
	kw := keywords(cmd)
	n := 2
	if len(kw) > 0 && (kw[0] == "show" || kw[0] == "clear") {
		n = 3
	}
	if len(kw) < n {
		return strings.Join(kw, " ")
	}
	return strings.Join(kw[:n], " ")
}

func setDiff(ixCmds, ixrCmds []indexedCommand, known []diffRow) []diffRow {
	// IX-R 側はキーワードの前置きを全部持つ。無印の族が括弧やパラメータで
	// 短く切れている (snmp-agent [ vrf … ] → 族 "snmp-agent") とき、IX-R の
	// snmp-agent ip … があれば族が無いとは言わないため。
	ixrFam := map[string]bool{}
	for _, c := range ixrCmds {
		kw := keywords(c.cmd)
		for n := 1; n <= len(kw); n++ {
			ixrFam[strings.Join(kw[:n], " ")] = true
		}
	}
	type fam struct {
		n     int
		first indexedCommand
	}
	fams := map[string]*fam{}
	for _, c := range ixCmds {
		k := family(c.cmd)
		if k == "" || ixrFam[k] {
			continue
		}
		if fams[k] == nil {
			fams[k] = &fam{first: c}
		}
		fams[k].n++
	}
	// ch8 / derived で挙がっている族は出さない (そちらの方が確かで、根拠もある)
	for _, r := range known {
		delete(fams, family(r.ix))
	}
	keys := make([]string, 0, len(fams))
	for k := range fams {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var rows []diffRow
	for _, k := range keys {
		f := fams[k]
		rows = append(rows, diffRow{
			kind:   "absent",
			ix:     k + " *",
			source: "setdiff",
			refIX:  "crm:" + f.first.source,
			note:   fmt.Sprintf("無印の索引にある %d 件が IX-R の索引に 1 件も無い (例: %s)。目安なので commands.tsv で確かめる", f.n, f.first.cmd),
		})
	}
	return rows
}
