package infrastructure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
	"golang.org/x/net/html"
)

const (
	figureA = "a/_images/fig.svg"
	figureB = "b/_images/fig.svg"
)

func writeCacheFileForTest(t *testing.T, cache, rel, text string) {
	t.Helper()
	full := filepath.Join(cache, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// figures/ は 1 階層なので、別のディレクトリにある同名の画像は連番で分ける。
func TestWriteWebFiguresKeepsSameNamedImagesApart(t *testing.T) {
	cache, out := t.TempDir(), t.TempDir()
	for _, rel := range []string{figureA, figureB} {
		writeCacheFileForTest(t, cache, rel, `<svg xmlns="http://www.w3.org/2000/svg"><text x="0" y="12">`+rel+`</text></svg>`)
	}
	heads := []domain.Heading{{Blocks: []domain.Block{
		{Kind: domain.BlockFigure, FigureSource: figureA},
		{Kind: domain.BlockFigure, FigureSource: figureB},
		{Kind: domain.BlockFigure, FigureSource: figureA}, // 同じ画像は同じ出力名
	}}}
	if err := WriteWebFigures(cache, out, heads); err != nil {
		t.Fatal(err)
	}
	blocks := heads[0].Blocks
	if blocks[0].Figure == blocks[1].Figure {
		t.Fatalf("different images share output name %q", blocks[0].Figure)
	}
	if blocks[0].Figure != blocks[2].Figure {
		t.Errorf("same image got two names: %q / %q", blocks[0].Figure, blocks[2].Figure)
	}
	for i, want := range []string{figureA, figureB} {
		checkWrittenFigure(t, out, blocks[i], want)
	}
	if n := webFigureCount(heads); n != 2 {
		t.Errorf("webFigureCount = %d, want 2", n)
	}
}

// checkWrittenFigure は figures/ の中身とラベルが元の画像 (want) のものかを見る。
func checkWrittenFigure(t *testing.T, out string, b domain.Block, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, "figures", b.Figure))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), want) {
		t.Errorf("figures/%s holds the wrong image", b.Figure)
	}
	if len(b.Lines) != 1 || b.Lines[0] != want {
		t.Errorf("labels of %s = %q", b.Figure, b.Lines)
	}
}

// README の図の枚数は Web の図 (SVG) を数える。ページ画像の名前 (p<番号>.png) しか
// 見ない loadFigures では常に 0 になっていた。
func TestWriteSectionsCountsWebFiguresInReadme(t *testing.T) {
	cache, out := t.TempDir(), t.TempDir()
	writeCacheFileForTest(t, cache, "_images/one.svg", `<svg xmlns="http://www.w3.org/2000/svg"/>`)
	heads := []domain.Heading{{Title: "T", Depth: 1, Chapter: 1, Section: "S", Blocks: []domain.Block{
		{Kind: domain.BlockFigure, FigureSource: "_images/one.svg"},
	}}}
	if err := WriteWebFigures(cache, out, heads); err != nil {
		t.Fatal(err)
	}
	src := Source{Kind: domain.KindWeb, BaseURL: "https://example.invalid/fd/", Profile: &domain.Profile{Name: "test"}}
	if err := WriteSections(out, "doc", src, heads, map[int]string{1: "ch"}); err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(filepath.Join(out, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "(1 枚)") {
		t.Errorf("README does not count the SVG figure:\n%s", readme)
	}
}

// 見出しの無い <section> に欄があっても落ちない (findNode の nil をそのまま辿る)。
func TestParseWebEntriesSurvivesSectionWithoutHeading(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(`<html><body><div itemprop="articleBody">
<section id="s"><h1><span class="section-number">1.1. </span>T</h1>
<section id="x"><dl><dt>[入力形式]</dt><dd><div class="line-block"><div class="line">foo bar</div></div></dd></dl></section>
</section></div></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	_, sec, _ := webArticleHeading(doc)
	pg := WebPage{path: "p.html", number: []int{1, 1}, title: "T", body: sec}
	entries, _ := ParseWebEntries([]WebPage{pg}, &domain.Profile{FieldLabels: []string{"入力形式"}})
	if len(entries) != 1 || len(entries[0].Cmds) != 1 || entries[0].Cmds[0] != "foo bar" {
		t.Fatalf("entries = %+v", entries)
	}
}

// 番号なしのページは番号付きの章の後ろに並ぶ (章番号は最大 + 1 から振る)。
func TestReadWebPagesOrdersUnnumberedAfterNumbered(t *testing.T) {
	cache := t.TempDir()
	writeCacheFileForTest(t, cache, "index.html", `<article itemprop="articleBody"><section id="cover"><h1>Cover</h1><p>intro</p></section></article>`)
	writeCacheFileForTest(t, cache, "a.html", `<article itemprop="articleBody"><section id="a"><h1><span class="section-number">1. </span>One</h1></section></article>`)
	writeCacheFileForTest(t, cache, "b.html", `<article itemprop="articleBody"><section id="b"><h1><span class="section-number">2. </span>Two</h1></section></article>`)
	pages, err := ReadWebPages(cache, true)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(pages))
	for _, pg := range pages {
		got = append(got, pg.path+"="+numberString(pg.number))
	}
	if want := "a.html=1 b.html=2 index.html=3"; strings.Join(got, " ") != want {
		t.Errorf("pages = %v, want %s", got, want)
	}
}
