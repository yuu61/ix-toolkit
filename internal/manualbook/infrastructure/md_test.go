package infrastructure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

const (
	slashSection = "A/B"
	chapterOne   = "one"
)

// safeName で同じファイル名になる節を同じファイルに書くと、後の節が先の節を
// 黙って上書きし、索引の行番号は消えた本文を指す。別ファイルに分ける。
func TestSectionFilesKeepCollidingNamesApart(t *testing.T) {
	long := strings.Repeat("あ", 60)
	order := []domain.SectionKey{
		{Chapter: 1, Section: slashSection},
		{Chapter: 1, Section: "A:B"},
		{Chapter: 1, Section: "a-b"}, // Windows / macOS では A-B と同じファイル
		{Chapter: 1, Section: long + "1"},
		{Chapter: 1, Section: long + "2"},
		{Chapter: 2, Section: slashSection}, // 章が違えば衝突しない
	}
	rel := sectionFiles(order, map[int]string{1: chapterOne, 2: "ch2"})
	seen := map[string]domain.SectionKey{}
	for _, k := range order {
		p := strings.ToLower(filepath.ToSlash(rel[k]))
		if prev, dup := seen[p]; dup {
			t.Errorf("%+v and %+v share %s", prev, k, rel[k])
		}
		seen[p] = k
	}
	if got, want := filepath.ToSlash(rel[order[0]]), "ch01-"+chapterOne+"/A-B.md"; got != want {
		t.Errorf("first section = %s, want %s", got, want)
	}
	if got := filepath.ToSlash(rel[order[5]]); got != "ch02-ch2/A-B.md" {
		t.Errorf("other chapter = %s, want ch02-ch2/A-B.md", got)
	}
}

func TestWriteAllKeepsEveryCollidingSection(t *testing.T) {
	out := t.TempDir()
	entries := []domain.Entry{
		{Title: chapterOne, Chapter: 1, Section: slashSection, Fields: []domain.Field{{Label: "説明", Lines: []string{"first"}}}},
		{Title: "entry2", Chapter: 1, Section: "A:B", Fields: []domain.Field{{Label: "説明", Lines: []string{"second"}}}},
	}
	src := Source{Kind: domain.KindPDF, PDF: "x.pdf", Profile: domain.DefaultProfile()}
	if err := WriteAll(out, "doc", src, map[int]string{1: "ch"}, entries); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"first", "second"} {
		found := false
		for _, e := range entries {
			// 索引の行番号は必ず見出し行を指す。
			if !strings.Contains(readSectionAt(t, out, e), want) {
				continue
			}
			found = true
		}
		if !found {
			t.Errorf("section body %q lost", want)
		}
	}
}

// readSectionAt は索引と同じ規則 (章・節 → ファイル、Line → 見出し行) で本文を読む。
func readSectionAt(t *testing.T, out string, e domain.Entry) string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(out, "ch01-ch"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range ents {
		b, err := os.ReadFile(filepath.Join(out, "ch01-ch", f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(b), "\n")
		if e.Line-1 < len(lines) && lines[e.Line-1] == "## "+e.Title {
			return string(b)
		}
	}
	return ""
}

// 字下げは半角空白の数なので、削るのも半角空白に限る。長さだけで切ると
// 全角空白だけの空行を途中で割って壊れた UTF-8 になる。
func TestDedentKeepsIdeographicSpaceLinesValid(t *testing.T) {
	got := dedent([]string{"    abc", "　　", "    def"})
	for i, ln := range got {
		if !utf8.ValidString(ln) {
			t.Errorf("line %d: invalid UTF-8 %q", i, ln)
		}
	}
	if got[0] != "abc" || got[2] != "def" {
		t.Errorf("dedent = %q", got)
	}
}
