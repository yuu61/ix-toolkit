package application_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yuu61/ix-toolkit/internal/manualbook/application"
	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
	"github.com/yuu61/ix-toolkit/internal/manualbook/infrastructure"
)

func TestWebFigureFailuresReachApplication(t *testing.T) {
	for _, failure := range []string{"missing image", "figure directory", "image destination"} {
		t.Run(failure, func(t *testing.T) {
			o := webFigureFixture(t)
			switch failure {
			case "missing image":
				if err := os.Remove(filepath.Join(o.Input, "diagram.svg")); err != nil {
					t.Fatal(err)
				}
			case "figure directory":
				writeWebFile(t, filepath.Join(o.OutDir, "figures"), "blocked")
			case "image destination":
				if err := os.MkdirAll(filepath.Join(o.OutDir, "figures", "diagram.svg"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := application.Convert(o); err == nil || !strings.Contains(err.Error(), "図") {
				t.Fatalf("Convert error = %v, want figure failure", err)
			}
			if _, err := os.Stat(filepath.Join(o.OutDir, "sections.tsv")); !os.IsNotExist(err) {
				t.Fatalf("index published after figure failure: %v", err)
			}
		})
	}
}

func TestWebHeadingParsingDoesNotReadOrWriteFigures(t *testing.T) {
	o := webFigureFixture(t)
	pages, err := infrastructure.ReadWebPages(o.Input, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(o.Input, "diagram.svg")); err != nil {
		t.Fatal(err)
	}
	heads, _ := infrastructure.ParseWebHeadings(pages)
	if len(heads) != 1 || len(heads[0].Blocks) != 2 {
		t.Fatalf("headings = %+v", heads)
	}
	checkFigureReferences(t, heads)
	if entries, err := os.ReadDir(o.OutDir); err != nil || len(entries) != 0 {
		t.Fatalf("parser wrote output: %v, %v", entries, err)
	}
}

func TestWebFiguresKeepLinksAndLabels(t *testing.T) {
	o := webFigureFixture(t)
	if err := application.Convert(o); err != nil {
		t.Fatal(err)
	}
	figures, err := os.ReadDir(filepath.Join(o.OutDir, "figures"))
	if err != nil || len(figures) != 1 {
		t.Fatalf("figures = %v, %v", figures, err)
	}
	index, err := os.ReadFile(filepath.Join(o.OutDir, "sections.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	columns := strings.Split(strings.Split(strings.TrimSpace(string(index)), "\n")[1], "\t")
	body := readIndexBody(t, o.OutDir, columns[2])
	if strings.Count(string(body), "diagram.svg") != 2 || strings.Count(string(body), "Router") != 2 {
		t.Fatalf("figure links or labels lost: %s", body)
	}
}

func webFigureFixture(t *testing.T) application.MDOptions {
	t.Helper()
	cache, out := t.TempDir(), t.TempDir()
	profile := filepath.Join(t.TempDir(), "profile.json")
	writeWebFile(t, profile, `{"name":"test-fd","fieldLabels":[]}`)
	writeWebFile(t, filepath.Join(cache, "page.html"), `<article itemprop="articleBody"><section id="chapter"><h1><span class="section-number">1. </span>Test</h1><img src="diagram.svg"><img src="diagram.svg"></section></article>`)
	writeWebFile(t, filepath.Join(cache, "diagram.svg"), `<svg xmlns="http://www.w3.org/2000/svg"><text x="0" y="12">Router</text></svg>`)
	return application.MDOptions{Input: cache, OutDir: out, ProfilePath: profile}
}

// 原本は配布しない。番号なしの表紙・ログ項目の構造だけを合成する。
func TestWebUnnumberedHeadings(t *testing.T) {
	cache, out := t.TempDir(), t.TempDir()
	profile := filepath.Join(t.TempDir(), "profile.json")
	writeWebFile(t, profile, `{"name":"test-slog","fieldLabels":[],"webUnnumberedHeadings":true}`)
	writeWebFile(t, filepath.Join(cache, ".manualbook.json"), `{"url":"https://example.invalid/slog/","series":"ix-r","version":"1.2.3"}`)
	writeWebFile(t, filepath.Join(cache, "index.html"), `<article itemprop="articleBody">
<section id="cover"><h1>Example series</h1></section>
<section id="reference"><h1>Log reference</h1><section id="format"><h2>Format</h2><p>APP-NAME MSGID MSG</p></section></section>
</article>`)
	writeWebFile(t, filepath.Join(cache, "log_sample.html"), `<article itemprop="articleBody">
<section id="sample"><h1>sample</h1><section id="events"><h2>Events</h2>
<nav class="contents local"><ul class="simple">
<li><p><a href="#sample-001-event">sample - 001 - Event &lt;PEER&gt;</a></p></li>
<li><p><a href="#sample-002-event">sample - 002 - Next event</a></p></li>
</ul></nav>
<section id="sample-001-event"><span id="sample-001"></span><h3><a class="toc-backref" href="#toc">sample - 001 - Event &lt;PEER&gt;</a><a class="headerlink" href="#sample-001-event">link</a></h3>
<dl class="field-list"><dt>Level:</dt><dd><p>notice</p></dd><dt>Meaning:</dt><dd><p>Example event.</p></dd><dt>Parameters:</dt><dd><p>&lt;PEER&gt;: peer name</p></dd></dl></section>
</section></section></article>`)
	// 本文のない検索ページは番号なし対応でも含めない。
	writeWebFile(t, filepath.Join(cache, "search.html"), `<article itemprop="articleBody"><h1>Search</h1></article>`)
	pages, err := infrastructure.ReadWebPages(cache, false)
	if err != nil || len(pages) != 0 {
		t.Fatalf("numbered mode included unnumbered pages: %d, %v", len(pages), err)
	}
	if err = application.Convert(application.MDOptions{Input: cache, OutDir: out, ProfilePath: profile}); err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(out, "sections.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(string(index)), "\n")
	if len(rows) != 7 { // ヘッダ + 表紙 3 見出し + ログページ 3 見出し
		t.Fatalf("index headings lost or duplicated: %s", index)
	}
	for _, row := range rows[1:] {
		checkUnnumberedIndexRow(t, out, row)
	}
	if !strings.Contains(string(index), "sample-001\t") {
		t.Fatal("log entry anchor missing")
	}
}

func writeWebFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func checkFigureReferences(t *testing.T, heads []domain.Heading) {
	for i := range heads[0].Blocks {
		b := &heads[0].Blocks[i]
		if b.FigureSource != "diagram.svg" || b.Figure != "" || len(b.Lines) != 0 {
			t.Fatalf("parser must return only the figure reference: %+v", b)
		}
	}
}

func checkUnnumberedIndexRow(t *testing.T, out, row string) {
	cols := strings.Split(row, "\t")
	if len(cols) != 5 {
		t.Fatalf("bad index row: %s", row)
	}
	body := readIndexBody(t, out, cols[2])
	line, err := strconv.Atoi(cols[3])
	lines := strings.Split(string(body), "\n")
	if err != nil || line < 1 || line > len(lines) || !strings.HasSuffix(strings.ReplaceAll(lines[line-1], "`", ""), " "+cols[1]) {
		t.Fatalf("index does not point to heading: %s", row)
	}
	if cols[0] == "format" && !strings.Contains(string(body), "APP-NAME MSGID MSG") {
		t.Fatal("cover explanation lost")
	}
	if cols[0] == "sample-001" {
		checkLogIndexRow(t, row, cols, lines, body, line)
	}
}

func checkLogIndexRow(t *testing.T, row string, cols, lines []string, body []byte, line int) {
	list := "- sample - 001 - Event `<PEER>`\n- sample - 002 - Next event\n"
	if !strings.Contains(string(body), list) {
		t.Fatalf("local contents must preserve one list item per line: %s", body)
	}
	if !strings.Contains(lines[line-1], "`<PEER>`") {
		t.Fatal("heading parameter would be interpreted as an HTML tag")
	}
	if cols[1] != "sample - 001 - Event <PEER>" || cols[4] != "log_sample.html#sample-001" {
		t.Fatalf("message or source lost: %s", row)
	}
	for _, want := range []string{"Level", "notice", "Meaning", "Example event.", "Parameters", "<PEER>", "peer name"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("log body missing %q: %s", want, body)
		}
	}
}

func readIndexBody(t *testing.T, dir, rel string) []byte {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	body, err := root.ReadFile(filepath.FromSlash(rel))
	if err != nil {
		t.Fatal(err)
	}
	return body
}
