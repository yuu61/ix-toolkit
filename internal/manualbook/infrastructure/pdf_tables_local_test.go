package infrastructure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 原本は手元だけに置く。画像の罫線とテキスト層を組み合わせる経路の回帰検証。
func TestLocalPDFTables(t *testing.T) {
	root := os.Getenv("IX_MANUALBOOK_PDF_ROOT")
	if root == "" {
		t.Skip("set IX_MANUALBOOK_PDF_ROOT to check local PDFs")
	}
	pdf := filepath.Join(root, "pdf/FD-ver10.11-1.1.pdf")
	d, err := openDoc(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer CloseDoc(pdf)
	p, err := LoadProfile(filepath.Join(root, "profiles/nec-ix-fd.json"))
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
			rows := tables[0].rows
			if len(rows) != 15 || len(rows[0]) != 10 {
				t.Fatalf("p27 dimensions = %dx%d", len(rows), len(rows[0]))
			}
			if !strings.Contains(rows[0][2], "IX2107") || rows[1][7] != "1000 ※1" || rows[2][7] != "8" || rows[1][0] != rows[2][0] {
				t.Fatalf("p27 model columns, footnotes or merged cells changed: %#v", rows[:3])
			}
		}
		if len(tables) == 0 {
			continue
		}
		lines, blocks := sectionTableLines(pg, tables, bodyCrop(p), tc.page)
		combined := strings.Join(lines, "\n")
		for _, b := range blocks {
			for _, row := range b.Rows {
				combined += strings.Join(row, "")
			}
		}
		counts := glyphCounts(combined)
		for r, n := range glyphCounts(renderPage(pg, bodyCrop(p), true)) {
			// 結合セルの展開で文字が増えることはあるが、表外の本文を含めて減らしてはいけない。
			if counts[r] < n {
				t.Errorf("p%d: glyph %q count = %d, want at least %d", tc.page, r, counts[r], n)
			}
		}
	}
}

func TestLocalPDFTableContinuation(t *testing.T) {
	root := os.Getenv("IX_MANUALBOOK_PDF_ROOT")
	if root == "" {
		t.Skip("local PDF")
	}
	pdf := filepath.Join(root, "pdf/FD-ver10.11-1.1.pdf")
	d, err := openDoc(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer CloseDoc(pdf)
	p, err := LoadProfile(filepath.Join(root, "profiles/nec-ix-fd.json"))
	if err != nil {
		t.Fatal(err)
	}
	var previous pdfTable
	for _, n := range []int{1141, 1142} {
		rules, err := d.readRules(n - 1)
		if err != nil {
			t.Fatal(err)
		}
		tables := findPDFTables(d.pages[n-1], rules)
		if len(tables) == 0 {
			t.Fatalf("p%d no tables", n)
		}
		if n == 1141 {
			previous = tables[len(tables)-1]
			previous.headerPage = n
			continue
		}
		current := tables[0]
		continuePDFTable(previous, &current, d.pages[n-2], d.pages[n-1], bodyCrop(p), n)
		if current.headerPage != 1141 {
			t.Errorf("header not inherited: prev columns %v, bounds %v/%v; current columns %v, bounds %v/%v", previous.columns, previous.top, previous.bottom, current.columns, current.top, current.bottom)
		}
		if current.rows[0][0] != "オブジェクト名" {
			t.Fatalf("unexpected inherited header: %q", current.rows[0][0])
		}
	}
}
