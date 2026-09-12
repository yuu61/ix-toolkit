package application_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuu61/ix-toolkit/internal/manualbook/application"
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
				if err := os.MkdirAll(filepath.Join(o.OutDir, "figures", "diagram.svg"), 0700); err != nil {
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
	pages, err := infrastructure.ReadWebPages(o.Input)
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
	for _, b := range heads[0].Blocks {
		if b.FigureSource != "diagram.svg" || b.Figure != "" || len(b.Lines) != 0 {
			t.Fatalf("parser must return only the figure reference: %+v", b)
		}
	}
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
	body, err := os.ReadFile(filepath.Join(o.OutDir, filepath.FromSlash(columns[2])))
	if err != nil {
		t.Fatal(err)
	}
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

func writeWebFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
