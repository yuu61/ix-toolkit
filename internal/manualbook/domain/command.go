package domain

import (
	"strings"
)

// SyntaxLabels は「コマンド構文そのもの」を載せる見出し語。
// ここだけはコードブロックにし、行の折り返しをいじらない。
//
// 揃えたあとの綴り (NormalizeLabel を通した形) で引く。LabelSpelling の写し先が
// ここと食い違うと、構文の欄が地の文として整形され、索引が空になる。
// その不変条件は manual_test.go で確かめる。
var SyntaxLabels = map[string]bool{"入力形式": true, "入力例": true}

// CommandsOf は入力形式からコマンド行を取り出す (no 形は除く)。これが索引のキーになる。
//
// 長い構文は版面で折り返されており、その続きは 1 段深く字下げされている。
// 折り返し行をそのまま拾うと "128][aes-cbc-192]..." のような断片が索引に並ぶので、
// 字下げの深い行は直前のコマンドに繋ぎ直す。
//
// 「深い」の基準は欄の 1 行目の字下げ。共通の字下げを取り除く (dedent) だけだと、
// no 形が 1 行目より浅く置かれた項目 (無印 CRM の snmp-agent ip trap や
// ike proposal、IX-R CRM の local-ts: 版面と原稿の癖) で 1 行目が続き扱いになり、
// 項目ごと索引から消える。欄の 1 行目が続きであることは無い。
func CommandsOf(e *Entry) []string {
	var joined []string
	for _, f := range e.Fields {
		if f.Label != "入力形式" {
			continue
		}
		base := -1
		for _, ln := range f.Lines {
			t := strings.TrimSpace(ln)
			if t == "" {
				continue
			}
			if base < 0 {
				base = indentOf(ln)
			}
			// コマンドは必ず英小文字で始まる。1 行目より字下げが深い行と、
			// 英小文字で始まらない行 ("ADDRESS]" のような折り返しの後半) は
			// 直前のコマンドの続きとして繋ぎ直す。
			if indentOf(ln) > base || !startsLowerASCII(t) {
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
