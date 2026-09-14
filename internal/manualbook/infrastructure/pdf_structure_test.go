package infrastructure

import (
	"reflect"
	"strings"
	"testing"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

func structureLine(text string, x, y, size float64) []glyph {
	var gs []glyph
	space := false
	for _, r := range text {
		if r == ' ' {
			x += size / 2
			space = true
			continue
		}
		g := testGlyph(r, x, y)
		g.fontSize, g.spaceBefore = size, space
		gs = append(gs, g)
		x += float64(runeCols(r)) * size / 2
		space = false
	}
	return gs
}

func TestPDFHeadingsRequireChapterAndLargerNumber(t *testing.T) {
	texts := []string{
		"■2.43 設定", "2.43.8.17 SSH/Telnet の設定", // IPv4 と同じ桁数も有効
		"192.168.0.1 Router-A", "2.1 手順の説明です。", // 同じ章番号の本文も除く
		"3.1 別の章の番号", "2.43.8.18 目次.........30",
	}
	pg := pdfPage{width: 600, height: 800}
	for i, s := range texts {
		size := 11.04
		if i == 2 || i == 3 {
			size = 10.56
		}
		pg.glyphs = append(pg.glyphs, structureLine(s, 45, float64(700-i*25), size)...)
	}
	page := Page{
		num: 10, chapter: 2, section: "章・設定", lines: texts,
		headings: pdfHeadingLines(pg, crop{}, 10.56, 0),
	}
	heads, _ := ParseHeadings(&domain.Profile{ChapterSep: "・"}, []Page{page})
	got := make([]string, 0, len(heads))
	for i := range heads {
		h := &heads[i]
		got = append(got, h.Number)
	}
	if !reflect.DeepEqual(got, []string{"2.43", "2.43.8.17"}) {
		t.Fatalf("headings = %v", got)
	}
	body := headingBody(heads)
	for _, text := range texts[2:5] {
		if !strings.Contains(body, text) {
			t.Errorf("rejected heading lost from body: %s", text)
		}
	}
	if classifyLine(texts[2]) != lineLayout {
		t.Fatal("diagram address classified as prose")
	}
}

func TestPDFBodyFontSizeUsesBookAndIgnoresMargins(t *testing.T) {
	pg := pdfPage{width: 600, height: 800}
	pg.glyphs = append(pg.glyphs, structureLine(strings.Repeat("本文", 20), 45, 700, 10.56)...)
	pg.glyphs = append(pg.glyphs, structureLine(strings.Repeat("柱", 100), 45, 790, 14)...)
	pg.glyphs = append(pg.glyphs, structureLine("2.1 見出し", 45, 650, 12)...)
	tablePage := pdfPage{width: 600, height: 800, glyphs: structureLine("小さい表", 45, 700, 8)}
	if got := pdfBodyFontSize([]pdfPage{pg, tablePage}, crop{top: 40}); got != 10.56 {
		t.Fatalf("body font size = %v", got)
	}
}

func TestPDFExampleHeadingsAndAlternatingFooters(t *testing.T) {
	for _, footer := range []string{"IPv4 設定 1-3", "1-4 IPv4 設定"} {
		section, printed := footerSection(footer)
		if section != "IPv4 設定" || chapterOf(printed) != 1 {
			t.Fatalf("footer = %q, %q", section, printed)
		}
		checkExampleHeadings(t, section)
	}
}

func TestPDFBulletParagraphAndWrappedContinuation(t *testing.T) {
	for _, tc := range []struct {
		name string
		want []string
		gap  float64
	}{
		{name: "paragraph", gap: 15, want: []string{"• first", "• second", "後続の本文です。"}},
		{name: "wrap", gap: 10, want: []string{"• first", "• second後続の本文です。"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pg := pdfPage{width: 300, height: 400}
			// 項目名の間隔が大きく、ページ全体の行送りは 17pt になる。
			for i := range 8 {
				pg.glyphs = append(pg.glyphs, structureLine("項目名", 45, float64(370-i*17), 8)...)
			}
			pg.glyphs = append(pg.glyphs, structureLine("• first", 30, 210, 8)...)
			pg.glyphs = append(pg.glyphs, structureLine("• second", 30, 200, 8)...)
			pg.glyphs = append(pg.glyphs, structureLine("後続の本文です。", 55, 200-tc.gap, 8)...)
			text := renderPage(pg, crop{}, true)
			start := strings.Index(text, "• first")
			if start < 0 {
				t.Fatal("missing first bullet")
			}
			lines := splitLines(text[start:])
			for _, input := range [][]string{lines, collapseTableBlanks(lines)} {
				if got := joinWrapped(input); !reflect.DeepEqual(got, tc.want) {
					t.Errorf("joined = %#v, want %#v", got, tc.want)
				}
			}
		})
	}
}

func TestPDFSingleBulletReturnsToParagraphIndent(t *testing.T) {
	for _, x := range []float64{45, 70} {
		pg := pdfPage{width: 300, height: 400}
		pg.glyphs = append(pg.glyphs, structureLine("• 長い項目", 60, 200, 8)...)
		pg.glyphs = append(pg.glyphs, structureLine("続きです。", x, 190, 8)...)
		got := joinWrapped(splitLines(renderPage(pg, crop{}, true)))
		want := []string{"• 長い項目", "続きです。"}
		if x == 70 {
			want = []string{"• 長い項目続きです。"}
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("x=%v: joined = %#v, want %#v", x, got, want)
		}
	}
}

func headingBody(heads []domain.Heading) string {
	var body strings.Builder
	for i := range heads {
		h := &heads[i]
		for j := range h.Blocks {
			b := &h.Blocks[j]
			body.WriteString(strings.Join(b.Lines, "\n"))
		}
	}
	return body.String()
}

func checkExampleHeadings(t *testing.T, section string) {
	texts := make([]string, 0, 9)
	texts = append(texts, "１．１　２つのLANを接続する", "１．2 経路を設定する", "１．３ 目次 · · · · · · · · · 1-5", "２．１ 別章", "192.168.0.1 Router", "Router(config)# ip route default 192.0.2.1", "1.1 項と同じ設定です。", "１．４ 目次の長い題 1-6")
	pg := pdfPage{width: 600, height: 800}
	for i, s := range texts {
		size := 12.0
		if i >= 4 {
			size = 10
		}
		pg.glyphs = append(pg.glyphs, structureLine(s, 45, float64(700-i*25), size)...)
	}
	// 通常の12pt見出しに加え、長い題を縮めた10.49ptの見出しも残す。
	texts = append(texts, "１．５ 長い題を縮めた事例")
	pg.glyphs = append(pg.glyphs, structureLine(texts[len(texts)-1], 45, 480, 10.49)...)
	page := Page{num: 16, chapter: 1, section: section, lines: texts, headings: pdfHeadingLines(pg, crop{}, 9, 10.3)}
	heads, chapters := ParseHeadings(&domain.Profile{FooterSection: true}, []Page{page})
	if len(heads) != 3 {
		t.Fatalf("headings = %+v", heads)
	}
	if heads[0].Number != "1.1" || heads[1].Number != "1.2" || heads[0].Title != "２つのLANを接続する" || heads[0].Ref.Page != 16 || chapters[1] != section {
		t.Fatalf("headings or chapter lost: %+v, %v", heads, chapters)
	}
	checkExampleBody(t, heads, texts)
}

func checkExampleBody(t *testing.T, heads []domain.Heading, texts []string) {
	var body strings.Builder
	for j := range heads[1].Blocks {
		b := &heads[1].Blocks[j]
		body.WriteString(strings.Join(b.Lines, "\n"))
	}
	if !strings.Contains(body.String(), texts[5]) {
		t.Error("configuration text changed")
	}
	if !strings.Contains(body.String(), texts[6]) {
		t.Error("cross-reference lost from body")
	}
}
