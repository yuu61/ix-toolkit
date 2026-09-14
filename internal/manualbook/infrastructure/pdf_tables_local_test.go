package infrastructure

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

const warnLevel = "warn"

const (
	debugLevel = "debug"
)

// 原本は手元だけに置く。画像の罫線とテキスト層を組み合わせる経路の回帰検証。
func TestLocalPDFTables(t *testing.T) {
	root := localPDFRoot(t)
	pdf := filepath.Join(root, "pdf", "FD-ver10.11-1.1.pdf")
	d := localPDFDoc(t, pdf)
	p, err := LoadProfile(filepath.Join(root, "profiles", "nec-ix-fd.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ page, tables int }{{24, 1}, {26, 1}, {27, 1}, {28, 1}, {61, 0}, {766, 2}, {1057, 0}, {1113, 2}, {1142, 2}} {
		rules, err := d.readRules(tc.page - 1)
		if err != nil {
			t.Fatal(err)
		}
		pg := d.pages[tc.page-1]
		tables := findPDFTables(pg, rules)
		if len(tables) != tc.tables {
			t.Fatalf("p%d tables = %d, want %d", tc.page, len(tables), tc.tables)
		}
		if tc.page == 27 {
			checkLocalModelTable(t, tables[0].rows)
		}
		if len(tables) == 0 {
			continue
		}
		checkTableGlyphs(t, pg, tables, p, tc.page)
	}
}

func TestLocalPDFTableContinuation(t *testing.T) {
	root := localPDFRoot(t)
	pdf := filepath.Join(root, "pdf", "FD-ver10.11-1.1.pdf")
	d := localPDFDoc(t, pdf)
	p, err := LoadProfile(filepath.Join(root, "profiles", "nec-ix-fd.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		previous, current, headerRows int
	}{{670, 671, 0}, {480, 481, 2}, {1141, 1142, 1}} {
		var previous pdfTable
		for _, n := range []int{tc.previous, tc.current} {
			rules, err := d.readRules(n - 1)
			if err != nil {
				t.Fatal(err)
			}
			tables := findPDFTables(d.pages[n-1], rules)
			if len(tables) == 0 {
				t.Fatalf("p%d no tables", n)
			}
			if n == tc.previous {
				previous = tables[len(tables)-1]
				previous.headerPage = n
				continue
			}
			checkLocalTableContinuation(t, d, p, previous, tables[0], n, tc.previous, tc.headerRows)
		}
	}
}

func checkLocalModelTable(t *testing.T, rows [][]string) {
	if len(rows) != 15 || len(rows[0]) != 10 {
		t.Fatalf("p27 dimensions = %dx%d", len(rows), len(rows[0]))
	}
	if !strings.Contains(rows[0][2], "IX2107") || rows[1][7] != "1000 ※1" || rows[2][7] != "8" || rows[1][0] != rows[2][0] {
		t.Fatalf("p27 model columns, footnotes or merged cells changed: %#v", rows[:3])
	}
}

func checkTableGlyphs(t *testing.T, pg pdfPage, tables []pdfTable, p *domain.Profile, page int) {
	lines, blocks := sectionTableLines(pg, tables, bodyCrop(p), page)
	var combined strings.Builder
	combined.WriteString(strings.Join(lines, "\n"))
	for k := range blocks {
		for _, row := range blocks[k].Rows {
			combined.WriteString(strings.Join(row, ""))
		}
	}
	counts := glyphCounts(combined.String())
	for r, n := range glyphCounts(renderPage(pg, bodyCrop(p), true)) {
		// 結合セルの展開で文字が増えることはあるが、表外の本文を含めて減らしてはいけない。
		if counts[r] < n {
			t.Errorf("p%d: glyph %q count = %d, want at least %d", page, r, counts[r], n)
		}
	}
}

func checkLocalTableContinuation(t *testing.T, d *pdfDoc, p *domain.Profile, previous, current pdfTable, n, previousPage, headerRows int) {
	original := current.rows
	continuePDFTable(previous, &current, d.pages[n-2], d.pages[n-1], bodyCrop(p), n)
	if headerRows == 0 {
		if current.headerPage != n || !reflect.DeepEqual(current.rows, original) || !strings.Contains(current.rows[0][0], "暗号／認証") {
			t.Fatalf("p%d own heading changed: %#v", n, current)
		}
		return
	}
	if current.headerPage != previousPage {
		t.Errorf("header not inherited: prev columns %v, bounds %v/%v; current columns %v, bounds %v/%v", previous.columns, previous.top, previous.bottom, current.columns, current.top, current.bottom)
	}
	if current.headerRows != headerRows || !reflect.DeepEqual(current.rows[:headerRows], previous.rows[:headerRows]) || !reflect.DeepEqual(current.rows[headerRows:], original) {
		t.Fatalf("p%d header levels or data changed: %#v", n, current)
	}
	checkLocalTableHeaders(t, current, n)
}

func checkLocalTableHeaders(t *testing.T, current pdfTable, n int) {
	if n == 481 && !reflect.DeepEqual(current.rows[1][2:], []string{"error", warnLevel, "notice", "info", debugLevel}) {
		t.Fatalf("p481 lost log levels: %#v", current.rows[:2])
	}
	if n == 1142 && current.rows[0][0] != "オブジェクト名" {
		t.Fatalf("p1142 unexpected header: %q", current.rows[0][0])
	}
}
