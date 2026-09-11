package infrastructure

import (
	"image"
	"image/color"
	"image/draw"
	"reflect"
	"strings"
	"testing"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

func sampleTable() (pdfPage, []pdfRule) {
	rules := []pdfRule{
		{false, 120, 10, 130}, {false, 100, 10, 130}, {false, 80, 10, 130}, {false, 60, 50, 130}, {false, 40, 10, 130},
		{true, 10, 40, 120}, {true, 50, 40, 120}, {true, 90, 40, 100}, {true, 130, 40, 120},
	}
	pg := pdfPage{width: 160, height: 150, glyphs: []glyph{
		testGlyph('A', 25, 110), testGlyph('B', 65, 115), testGlyph('C', 65, 105),
		testGlyph('D', 25, 90), testGlyph('E', 65, 90), testGlyph('F', 105, 90),
		testGlyph('G', 25, 70), testGlyph('H', 65, 70), testGlyph('I', 105, 70),
		testGlyph('J', 105, 50),
	}}
	return pg, rules
}

func TestPDFTableMergedCells(t *testing.T) {
	pg, rules := sampleTable()
	tables := findPDFTables(pg, rules)
	if len(tables) != 1 {
		t.Fatalf("tables = %d", len(tables))
	}
	want := [][]string{{"A", "B\nC", "B\nC"}, {"D", "E", "F"}, {"G", "H", "I"}, {"G", "", "J"}}
	if !reflect.DeepEqual(tables[0].rows, want) {
		t.Fatalf("rows = %#v", tables[0].rows)
	}
}

func TestPDFTableRasterRules(t *testing.T) {
	pg, rules := sampleTable()
	// 画像の原点が (0,0) 以外でも、ページの左下を原点とする座標へ戻す。
	img := image.NewRGBA(image.Rect(7, 9, 327, 309))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	for _, r := range rules {
		var box image.Rectangle
		if r.vertical {
			box = image.Rect(int(r.pos*2), int((pg.height-r.hi)*2), int(r.pos*2)+2, int((pg.height-r.lo)*2)+2)
		} else {
			box = image.Rect(int(r.lo*2), int((pg.height-r.pos)*2), int(r.hi*2)+2, int((pg.height-r.pos)*2)+2)
		}
		draw.Draw(img, box.Add(img.Bounds().Min), image.NewUniform(color.Black), image.Point{}, draw.Src)
	}
	tables := findPDFTables(pg, imageRules(img, pg.width, pg.height))
	if len(tables) != 1 || tables[0].rows[3][0] != "G" || tables[0].rows[0][2] != "B\nC" {
		t.Fatalf("raster table = %#v", tables)
	}
}

func TestPDFTableRejectsUncertainCells(t *testing.T) {
	for _, tc := range []string{"open border", "partial boundary", "nonrectangular merge", "crossing text", "single box"} {
		t.Run(tc, func(t *testing.T) {
			pg, rules := sampleTable()
			switch tc {
			case "open border":
				rules = rules[:len(rules)-1]
			case "partial boundary":
				rules = append(rules, pdfRule{true, 90, 101, 110})
			case "nonrectangular merge":
				rules[1].lo = 50
				rules[6].hi = 100
			case "crossing text":
				pg.glyphs = append(pg.glyphs, glyph{r: 'X', left: 47, right: 53, top: 94, bottom: 86})
			case "single box":
				rules = []pdfRule{{false, 120, 10, 130}, {false, 40, 10, 130}, {true, 10, 40, 120}, {true, 130, 40, 120}}
			}
			if tables := findPDFTables(pg, rules); len(tables) != 0 {
				t.Fatalf("uncertain table accepted: %#v", tables)
			}
		})
	}
}

func TestPDFTableStaysBetweenProseAndKeepsSource(t *testing.T) {
	pg, rules := sampleTable()
	pg.glyphs = append(pg.glyphs, testGlyph('P', 20, 135), testGlyph('Q', 20, 25))
	tables := findPDFTables(pg, rules)
	lines, blocks := sectionTableLines(pg, tables, crop{}, 27)
	if strings.Join(lines, "\n") != "P\n\nQ" {
		t.Fatalf("surrounding text = %#v", lines)
	}
	if len(blocks) != 1 || blocks[1].Ref.Page != 27 || blocks[1].Kind != domain.BlockTable {
		t.Fatalf("table insertion = %#v", blocks)
	}
	// 文末が句点のセルや節番号で始まるセルも、地の文や見出しへ分解しない。
	b := blocks[1]
	b.Rows[1][0] = "説明です。"
	b.Rows[1][1] = "2.3 別の節ではない"
	blocks[1] = b
	lines = append([]string{"1.2 本文"}, lines...)
	page := Page{num: 27, chapter: 1, section: "章・節", lines: lines, tables: map[int]domain.Block{2: b}}
	heads, _ := parseHeadings(&domain.Profile{ChapterSep: "・"}, []Page{page})
	if len(heads) != 1 {
		t.Fatalf("headings = %d", len(heads))
	}
	var output strings.Builder
	renderHeading(&output, &heads[0], links{pdf: "manual.pdf", figs: figureSet{27: "p27.png"}, base: "../figures"})
	for _, want := range []string{"| A | B<br>C | B<br>C |", "説明です。", "2.3 別の節ではない", "manual.pdf#page=27", "../figures/p27.png"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("missing %q in %s", want, output.String())
		}
	}
}

func TestPDFTableContinuation(t *testing.T) {
	pg, rules := sampleTable()
	table := findPDFTables(pg, rules)[0]
	table.headerPage = 27
	current := table
	current.rows = table.rows[1:]
	continuePDFTable(table, &current, pg, pg, crop{}, 28)
	if current.headerPage != 27 || !reflect.DeepEqual(current.rows, table.rows) {
		t.Fatalf("continuation = %#v", current)
	}
	shifted := table
	shifted.rows = table.rows[1:]
	shifted.left += 28
	shifted.right += 28
	shifted.columns = append([]float64(nil), table.columns...)
	for i := range shifted.columns {
		shifted.columns[i] += 28
	}
	continuePDFTable(table, &shifted, pg, pg, crop{}, 28)
	if shifted.headerPage != 27 {
		t.Fatal("alternating page margins lost the header")
	}
	_, blocks := sectionTableLines(pg, []pdfTable{current}, crop{}, 28)
	var output strings.Builder
	renderTableBlock(&output, blocks[0], links{pdf: "manual.pdf"})
	if !strings.Contains(output.String(), "[列見出し: 元 PDF p27](manual.pdf#page=27)") || !strings.Contains(output.String(), "[元 PDF p28]") {
		t.Fatalf("source = %s", output.String())
	}
	for _, tc := range []string{"prose before", "prose after", "different columns", "repeated header"} {
		t.Run(tc, func(t *testing.T) {
			prevPage, page := pg, pg
			current := table
			current.rows = table.rows[1:]
			current.columns = append([]float64(nil), table.columns...)
			switch tc {
			case "prose before":
				page.glyphs = append(append([]glyph(nil), pg.glyphs...), testGlyph('P', 20, 135))
			case "prose after":
				prevPage.glyphs = append(append([]glyph(nil), pg.glyphs...), testGlyph('Q', 20, 25))
			case "different columns":
				current.columns[1] += 5
			case "repeated header":
				current.rows = table.rows
			}
			want := len(current.rows)
			continuePDFTable(table, &current, prevPage, page, crop{}, 28)
			if len(current.rows) != want || current.headerPage != 28 {
				t.Fatal("new table or repeated header was extended")
			}
		})
	}
}

func TestTablePreservesLiteralMarkup(t *testing.T) {
	var output strings.Builder
	renderTableBlock(&output, domain.Block{Rows: [][]string{{"項目", "内容"}, {"a|b", "<ARG> & value\n次の行"}}}, links{pdf: "manual.pdf"})
	if !strings.Contains(output.String(), `| a\|b | &lt;ARG&gt; &amp; value<br>次の行 |`) {
		t.Fatalf("markup = %s", output.String())
	}
}
