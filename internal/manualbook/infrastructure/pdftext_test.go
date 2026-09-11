package infrastructure

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

func testGlyph(r rune, x, y float64) glyph {
	return glyph{r: r, left: x - 1, right: x + 1, bottom: y - 4, top: y + 4}
}

func glyphCounts(s string) map[rune]int {
	out := map[rune]int{}
	for _, r := range s {
		if !unicode.IsSpace(r) {
			out[r]++
		}
	}
	return out
}

func TestRenderColumnsPreservesGutterTextAndReadingOrder(t *testing.T) {
	p := &domain.Profile{Columns: 2, GutterLeft: 53, GutterRight: 53, MarginTop: 10, MarginBottom: 10}
	pg := pdfPage{width: 100, height: 100, glyphs: []glyph{
		testGlyph('H', 10, 95), testGlyph('F', 10, 5),
		// 左段の行末は中央より右、右段の行頭は中央より左に張り出す。
		testGlyph('a', 45, 80), testGlyph('b', 49, 80), testGlyph('c', 51, 80), testGlyph('R', 70, 80),
		testGlyph('L', 30, 60), testGlyph('•', 49, 60), testGlyph('d', 55, 60),
		// 同じ高さに手掛かりが無い場合も文字を捨てない。
		testGlyph('x', 48, 40), testGlyph('y', 52, 20),
	}}
	l, r := renderColumns(pg, p)
	if got := strings.Join(strings.Fields(l), ""); got != "abcLx" {
		t.Errorf("left = %q", l)
	}
	if got := strings.Join(strings.Fields(r), ""); got != "R•dy" {
		t.Errorf("right = %q", r)
	}
	if !reflect.DeepEqual(glyphCounts(l+r), glyphCounts("abcLR•dxy")) {
		t.Fatal("body glyphs were lost or duplicated")
	}
}

func TestRenderColumnsAssignsBoundaryAndOverlapOnce(t *testing.T) {
	for _, edges := range [][2]float64{{50, 50}, {45, 45}} {
		p := &domain.Profile{Columns: 2, GutterLeft: edges[0], GutterRight: edges[1]}
		pg := pdfPage{width: 100, height: 100, glyphs: []glyph{
			testGlyph('L', 40, 80), testGlyph('x', 50, 80), testGlyph('R', 60, 80),
		}}
		l, r := renderColumns(pg, p)
		if !reflect.DeepEqual(glyphCounts(l+r), glyphCounts("LxR")) {
			t.Errorf("edges %v: left %q, right %q", edges, l, r)
		}
	}
	l, r := renderColumns(pdfPage{width: 100, height: 100}, domain.DefaultProfile())
	if l != "" || r != "" {
		t.Fatal("empty page produced text")
	}
}

// 原本はリポジトリに置かない。手元の PDF で全文字の保存を検証する場合だけ指定する。
// IX_MANUALBOOK_PDF_ROOT=<repository> go test ./internal/manualbook/infrastructure -run TestLocalPDF
func TestLocalPDF(t *testing.T) {
	root := os.Getenv("IX_MANUALBOOK_PDF_ROOT")
	if root == "" {
		t.Skip("set IX_MANUALBOOK_PDF_ROOT to check local PDFs")
	}
	for _, book := range []string{"crm", "fd"} {
		t.Run(book, func(t *testing.T) {
			pdf := filepath.Join(root, "pdf", strings.ToUpper(book)+"-ver10.11-1.1.pdf")
			p, err := LoadProfile(filepath.Join(root, "profiles", "nec-ix-"+book+".json"))
			if err != nil {
				t.Fatal(err)
			}
			d, err := openDoc(pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer CloseDoc(pdf)
			var pages []Page
			if p.Columns >= 2 {
				var stats PageStats
				pages, stats, err = ReadPages(p, pdf)
				if err != nil {
					t.Fatal(err)
				}
				if stats.Two != 629 || stats.One != 223 {
					t.Fatalf("column classification changed: %+v", stats)
				}
				if n := len(ParseEntries(p, pages)); n != 2039 {
					t.Fatalf("entries = %d, want 2039", n)
				}
			} else if len(d.pages) != 1208 {
				t.Fatalf("FD pages = %d, want 1208", len(d.pages))
			}
			for i, pg := range d.pages {
				want := map[rune]int{}
				for _, g := range pg.glyphs {
					if bodyCrop(p).keep(g, pg.width, pg.height) && !unicode.IsSpace(g.r) {
						want[g.r]++
					}
				}
				var text string
				if len(pages) > 0 {
					text = strings.Join(pages[i].lines, "\n")
				} else {
					text = renderPage(pg, bodyCrop(p), true)
				}
				if !reflect.DeepEqual(glyphCounts(text), want) {
					t.Errorf("p%d: body glyphs were lost or duplicated", i+1)
				}
			}
			t.Logf("verified glyph counts on %d pages", len(d.pages))
		})
	}
}
