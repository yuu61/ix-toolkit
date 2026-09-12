package application

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
	"github.com/yuu61/ix-toolkit/internal/manualbook/infrastructure"
)

func TestPDFReleasedAfterBuildFigureFailure(t *testing.T) {
	for _, failure := range []string{"render", "mark"} {
		t.Run(failure, func(t *testing.T) {
			cache, out := t.TempDir(), t.TempDir()
			pdf := filepath.Join(cache, "sample.pdf")
			writeSyntheticPDF(t, pdf)
			t.Cleanup(func() { infrastructure.CloseDoc(pdf) })
			profile := filepath.Join(t.TempDir(), "profile.json")
			if err := os.WriteFile(profile, []byte(`{"name":"test-fd","columns":1,"pageWidth":40,"pageHeight":40,"fieldLabels":[]}`), 0600); err != nil {
				t.Fatal(err)
			}
			block := "p1.png"
			if failure == "mark" {
				block = ".manualbook.json"
			}
			if err := os.MkdirAll(filepath.Join(out, "figures", block), 0700); err != nil {
				t.Fatal(err)
			}
			err := convertDoc(domain.Doc{Name: "sample", Kind: "pdf"}, cache, out, profile)
			if err == nil || !strings.Contains(err.Error(), block) {
				t.Fatalf("error = %v, want failure writing %s", err, block)
			}
			assertPDFReleased(t, pdf)
		})
	}
}

func TestPDFApplicationEntriesReleaseDocument(t *testing.T) {
	for _, entry := range []string{"convert", "figures", "probe"} {
		t.Run(entry, func(t *testing.T) {
			pdf := filepath.Join(t.TempDir(), "sample.pdf")
			writeSyntheticPDF(t, pdf)
			t.Cleanup(func() { infrastructure.CloseDoc(pdf) })
			out := t.TempDir()
			switch entry {
			case "convert":
				if err := Convert(MDOptions{Input: pdf, OutDir: out}); err == nil {
					t.Fatal("blank PDF unexpectedly converted")
				}
			case "figures":
				if err := Figures(pdf, out, 72, ""); err != nil {
					t.Fatal(err)
				}
			case "probe":
				if err := Probe(pdf, filepath.Join(out, "profile.json"), "test", 1, 1); err == nil {
					t.Fatal("blank PDF unexpectedly calibrated")
				}
			}
			assertPDFReleased(t, pdf)
		})
	}
}

func assertPDFReleased(t *testing.T, pdf string) {
	t.Helper()
	if err := os.Remove(pdf); err != nil {
		t.Fatal(err)
	}
	// A retained PDF cache would still render successfully after deleting the source.
	if _, err := infrastructure.RenderFigures(pdf, t.TempDir(), 72, ""); !os.IsNotExist(err) {
		t.Fatalf("PDF remained cached after application returned: %v", err)
	}
}

// Generate a blank page from scratch; no copyrighted manual fixtures are needed.
func writeSyntheticPDF(t *testing.T, path string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 40 40] /Resources << >> >>",
	}
	var offsets []int
	for i, object := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	if err := os.WriteFile(path, []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
}
