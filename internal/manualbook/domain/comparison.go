package domain

import (
	"fmt"
	"regexp"
	"sort"
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

type DiffRow struct {
	Kind, IX, IXR, Source, RefIX, RefIXR, Note string
}

// --- 索引 ---

type IndexedCommand struct {
	Cmd, Entry, Source string
}

// lookupCommand は表に書かれたコマンド (パラメータ名や省略を含む) を索引で引く。
// 語を先頭から突き合わせ、パラメータ (英大文字・括弧) に当たるまでの語が
// 片方をもう片方が覆う索引行のうち、一致した語が最も多いものを返す。
// 索引側が短くてよいのは「pki cert erase { name NAME | all }」のように索引の
// 構文が表より早く括弧に入る場合のため。ただし 1 語だけの一致は取らない
// (ip や show で始まる行は何にでも当たる)。
func lookupCommand(cmds []IndexedCommand, text string) (IndexedCommand, bool) {
	want := keywords(text)
	if len(want) == 0 {
		return IndexedCommand{}, false
	}
	best, bestN := IndexedCommand{}, 0
	for _, c := range cmds {
		have := keywords(c.Cmd)
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

// Ch8Kind は節の題から行の種類を決める。題に種類を決める語が無ければ false
// (呼ぶ側は親の節の種類を引き継ぐ)。
func Ch8Kind(title string) (string, bool) {
	for _, c := range ch8Kinds {
		if strings.Contains(title, c.word) {
			return c.kind, true
		}
	}
	return "", false
}

// ref_* 列の書き方。「冊子:索引の source 列」で、crm は commands.tsv、fd は
// sections.tsv の source を指す。
func crmRef(source string) string { return "crm:" + source }

func fdRef(source string) string {
	if source == "" {
		return ""
	}
	return "fd:" + source
}

// Ch8Rows は表 1 つを行にする。列は見出し行の名で取る。ref は表のある節の
// source (sections.tsv の source 列) で、無ければ空。
//
//	removed: 機能 | 設定コマンド | IXシリーズ | IX-Rシリーズ
//	renamed: 機能 | IXシリーズ | IX-Rシリーズ | [備考]
//	moved:   機能 | 設定コマンド | IXシリーズ (モード) | IX-Rシリーズ (モード)
//	range / changed: コマンドの機能 | 対象コマンド | IXシリーズからの変更内容 | [補足]
func Ch8Rows(kind, ref string, table [][]string, ixCmds []IndexedCommand) []DiffRow {
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

	var rows []DiffRow
	for _, row := range table[1:] {
		ixs := expandAlternatives(cellLines(cell(row, cCmd)))
		ixrs := cellLines(cell(row, cIXR))
		var note []string
		if f := cell(row, cFunc); f != "" {
			note = append(note, f)
		}
		for i, ix := range ixs {
			r := DiffRow{Kind: kind, IX: ix, Source: "ch8", RefIXR: fdRef(ref)}
			n := append([]string{}, note...)
			switch kind {
			case "removed":
				// 「reloadコマンドに統一」の形なら置き換え先が取れる。それ以外
				// (「UFSキャッシュに統一」「廃止」) は note を読んでもらう。
				if m := unifiedRe.FindStringSubmatch(cell(row, cIXR)); m != nil {
					r.IXR = strings.TrimSpace(m[1])
				}
				n = append(n, "IX-R: "+cell(row, cIXR))
			case "renamed":
				if i < len(ixrs) {
					r.IXR = ixrs[i]
				} else if len(ixrs) > 0 {
					r.IXR = ixrs[len(ixrs)-1]
				}
				if v := cell(row, cNote); v != "" {
					n = append(n, v)
				}
			case "moved":
				r.IXR = ix
				n = append(n, "モード: "+cell(row, cIX)+" → "+cell(row, cIXR))
			default: // range / changed
				r.IXR = ix
				if v := cell(row, cChange); v != "" {
					n = append(n, v)
				}
				if v := cell(row, cNote); v != "" {
					n = append(n, v)
				}
			}
			r.Note = strings.Join(n, "。")
			if c, ok := lookupCommand(ixCmds, ix); ok {
				r.RefIX = crmRef(c.Source)
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
			out = append(out, expandAlternatives([]string{Collapse(c[:m[0]] + alt + c[m[1]:])})...)
		}
	}
	return out
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

func SetDiff(ixCmds, ixrCmds []IndexedCommand, known []DiffRow) []DiffRow {
	// IX-R 側はキーワードの前置きを全部持つ。無印の族が括弧やパラメータで
	// 短く切れている (snmp-agent [ vrf … ] → 族 "snmp-agent") とき、IX-R の
	// snmp-agent ip … があれば族が無いとは言わないため。
	ixrFam := map[string]bool{}
	for _, c := range ixrCmds {
		kw := keywords(c.Cmd)
		for n := 1; n <= len(kw); n++ {
			ixrFam[strings.Join(kw[:n], " ")] = true
		}
	}
	type fam struct {
		n     int
		first IndexedCommand
	}
	fams := map[string]*fam{}
	for _, c := range ixCmds {
		k := family(c.Cmd)
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
		delete(fams, family(r.IX))
	}
	keys := make([]string, 0, len(fams))
	for k := range fams {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var rows []DiffRow
	for _, k := range keys {
		f := fams[k]
		rows = append(rows, DiffRow{
			Kind:   "absent",
			IX:     k + " *",
			Source: "setdiff",
			RefIX:  crmRef(f.first.Source),
			Note:   fmt.Sprintf("無印の索引にある %d 件が IX-R の索引に 1 件も無い (例: %s)。目安なので commands.tsv で確かめる", f.n, f.first.Cmd),
		})
	}
	return rows
}
