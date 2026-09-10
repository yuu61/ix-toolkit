package infrastructure

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

// diff の入出力。両系列の変換結果 (commands.tsv / sections.tsv / 節の Markdown) と
// 手で導いた差分の TSV を読み、diff.tsv を書く。行の意味と突き合わせの規則は
// domain/comparison.go にある。

const diffHeader = "kind\tix\tix-r\tsource\tref_ix\tref_ixr\tnote"

// DiffInputs は diff が読む 3 つの索引。build はこれが揃ったときだけ diff を作る。
func DiffInputs(ixDir, ixrDir string) []string {
	return []string{
		CommandIndexPath(ixDir),
		CommandIndexPath(ixrDir),
		filepath.Join(FDDir(ixrDir), "sections.tsv"),
	}
}

// CommandIndexPath は系列ディレクトリの中のコマンドリファレンスの索引。
func CommandIndexPath(seriesDir string) string {
	return filepath.Join(seriesDir, "crm", "commands.tsv")
}

// FDDir は系列ディレクトリの中の機能説明書の変換結果。
func FDDir(seriesDir string) string { return filepath.Join(seriesDir, "fd") }

// WriteDiffTSV は行を diff.tsv の形で書く。
func WriteDiffTSV(out string, rows []domain.DiffRow) error {
	var b strings.Builder
	b.WriteString(diffHeader + "\n")
	for _, r := range rows {
		b.WriteString(strings.Join([]string{
			r.Kind, tsvCell(r.IX), tsvCell(r.IXR), r.Source, r.RefIX, r.RefIXR, tsvCell(r.Note),
		}, "\t") + "\n")
	}
	return os.WriteFile(out, []byte(b.String()), 0o644)
}

// FindDerived は手で導いた差分の TSV を、カレント → 実行ファイルの隣の順に探す。
// リポジトリ直下でビルドして流す想定なので、見つからなければ空を返して呼び手が警告する。
func FindDerived() string {
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
	return domain.Collapse(s)
}

// ReadCommandIndex は commands.tsv を読む。
func ReadCommandIndex(path string) ([]domain.IndexedCommand, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var cmds []domain.IndexedCommand
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
		cmds = append(cmds, domain.IndexedCommand{Cmd: c[0], Entry: c[1], Source: c[4]})
	}
	return cmds, sc.Err()
}

var (
	mdHeadingRe = regexp.MustCompile(`^(#{2,6}) (\S+) (.*)$`)
	mdTableRe   = regexp.MustCompile(`^\|.*\|$`)
	mdRuleRe    = regexp.MustCompile(`^\|(\s*:?-+:?\s*\|)+$`)
)

// ReadCh8 は IX-R 機能説明書の変換結果から「IXシリーズとの差分」の章を探し、
// 表を行に写す。
func ReadCh8(fdDir string, ixCmds []domain.IndexedCommand) ([]domain.DiffRow, error) {
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

	var rows []domain.DiffRow
	kindAt := map[int]string{} // 見出しの深さ → 種類 (親から引き継ぐ)
	kind, ref := "", ""
	for i := 0; i < len(lines); i++ {
		ln := strings.TrimRight(lines[i], "\r")
		if m := mdHeadingRe.FindStringSubmatch(ln); m != nil {
			depth := len(m[1])
			title := m[3]
			k, ok := domain.Ch8Kind(title)
			if !ok {
				k = kindAt[depth-1]
			}
			for d := depth; d <= 6; d++ {
				delete(kindAt, d)
			}
			kindAt[depth] = k
			kind = k
			if r, ok := refAt[i+1]; ok {
				ref = r
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
		rows = append(rows, domain.Ch8Rows(kind, ref, table, ixCmds)...)
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

// --- derived (手で導いた差分) ---

// ReadDerived は手で導いた差分の TSV を読む。
func ReadDerived(path string) ([]domain.DiffRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []domain.DiffRow
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
		rows = append(rows, domain.DiffRow{Kind: c[0], IX: c[1], IXR: c[2], Source: "derived", RefIX: c[3], RefIXR: c[4], Note: c[5]})
	}
	return rows, sc.Err()
}
