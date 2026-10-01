package infrastructure

import (
	"os"
	"path/filepath"
	"testing"
)

//nolint:cyclop // One fixture checks unchanged identity, missing/empty output, and same-name replacement.
func TestFiguresMarkRequiresUnchangedPDFAndEveryImage(t *testing.T) {
	pdf := filepath.Join(t.TempDir(), "sample.pdf")
	writeSquaresPDF(t, pdf, 2)
	t.Cleanup(func() { CloseDoc(pdf) })
	out := t.TempDir()
	if _, err := RenderFigures(pdf, out, 72, ""); err != nil {
		t.Fatal(err)
	}
	if err := WriteFiguresMark(out, pdf, 72, 2); err != nil {
		t.Fatal(err)
	}
	if !FiguresDone(out, pdf, 72) || FiguresDone(out, pdf, 144) {
		t.Fatal("complete images were not cached at the correct DPI")
	}
	image := filepath.Join(out, "figures", "p2.png")
	original, err := os.ReadFile(image)
	if err != nil {
		t.Fatal(err)
	}
	if removeErr := os.Remove(image); removeErr != nil {
		t.Fatal(removeErr)
	}
	if FiguresDone(out, pdf, 72) {
		t.Fatal("missing image was accepted")
	}
	if writeErr := os.WriteFile(image, nil, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if FiguresDone(out, pdf, 72) {
		t.Fatal("empty image was accepted")
	}
	if writeErr := os.WriteFile(image, original, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	st, err := os.Stat(pdf)
	if err != nil {
		t.Fatal(err)
	}
	// Same filename, size and modification time: only a content fingerprint
	// distinguishes this replacement from the original PDF.
	body, err := os.ReadFile(pdf)
	if err != nil {
		t.Fatal(err)
	}
	body[len(body)-1] = ' '
	if err := os.WriteFile(pdf, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(pdf, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if FiguresDone(out, pdf, 72) {
		t.Fatal("replacement PDF was accepted")
	}
}

func TestManualOrFailedFigureRenderInvalidatesCompleteMark(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "partial", true: "failed"}[failure], func(t *testing.T) {
			pdf := filepath.Join(t.TempDir(), "sample.pdf")
			writeSquaresPDF(t, pdf, 2)
			t.Cleanup(func() { CloseDoc(pdf) })
			out := t.TempDir()
			if _, err := RenderFigures(pdf, out, 72, ""); err != nil {
				t.Fatal(err)
			}
			if err := WriteFiguresMark(out, pdf, 72, 2); err != nil {
				t.Fatal(err)
			}
			if failure {
				path := filepath.Join(out, "figures", "p1.png")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			_, err := RenderFigures(pdf, out, 144, "1")
			if (err != nil) != failure {
				t.Fatalf("render error = %v, want failure = %v", err, failure)
			}
			if FiguresDone(out, pdf, 72) {
				t.Fatal("old complete mark survived a partial or failed rewrite")
			}
		})
	}
}

func TestOldFiguresMarkIsRegenerated(t *testing.T) {
	out := t.TempDir()
	dir := filepath.Join(out, "figures")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, figuresMarkName), []byte(`{"source":"sample.pdf","dpi":72,"pages":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if FiguresDone(out, "sample.pdf", 72) {
		t.Fatal("old mark without a content fingerprint was accepted")
	}
}
