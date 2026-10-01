package infrastructure

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestPageSpecBoundsAndOrder(t *testing.T) {
	for _, tc := range []struct {
		spec string
		want []int
	}{
		{"", []int{1, 2, 3}},
		{"3,1-2,2", []int{3, 1, 2}},
		{"0-4", []int{1, 2, 3}},
		{strconv.Itoa(int(^uint(0) >> 1)), nil},
		{"2-1", nil},
		{"invalid", nil},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			got, err := parsePageSpec(tc.spec, 3)
			if (err != nil) != (tc.want == nil) {
				t.Fatalf("error = %v, want pages %v", err, tc.want)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("pages = %v, want %v", got, tc.want)
			}
		})
	}
}

// ページ画像は文字を読まずに焼く。並列に焼いても逐次と同じバイト列になり、
// 各ページはそのページの絵になる。文字が要る経路は、画像の経路で開いた文書でも読む。
func TestRenderFiguresInParallelMatchesSequentialWithoutText(t *testing.T) {
	const nPages = 7
	pdf := filepath.Join(t.TempDir(), "squares.pdf")
	writeSquaresPDF(t, pdf, nPages)
	t.Cleanup(func() { CloseDoc(pdf) })
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(0))

	sequential := renderFiguresWith(t, pdf, 1, nPages)
	parallel := renderFiguresWith(t, pdf, maxPDFWorkers, nPages)
	for page := 1; page <= nPages; page++ {
		if !bytes.Equal(readFigure(t, sequential, page), readFigure(t, parallel, page)) {
			t.Errorf("p%d.png differs between sequential and parallel rendering", page)
		}
	}
	if bytes.Equal(readFigure(t, parallel, 1), readFigure(t, parallel, 2)) {
		t.Error("p1.png and p2.png are the same picture")
	}

	if _, err := RenderFigures(pdf, t.TempDir(), 72, "2"); err != nil {
		t.Fatal(err)
	}
	d, err := openDoc(pdf)
	if err != nil {
		t.Fatal(err)
	}
	if !d.textRead || len(d.pages) != nPages {
		t.Errorf("openDoc after RenderFigures: textRead=%v pages=%d", d.textRead, len(d.pages))
	}
}

// renderFiguresWith は GOMAXPROCS を procs にして全ページを焼き、figures/ の場所を返す。
// 焼いた後に文字が読まれていないことも確かめ、文書は閉じておく。
func renderFiguresWith(t *testing.T, pdf string, procs, nPages int) string {
	t.Helper()
	runtime.GOMAXPROCS(procs)
	out := t.TempDir()
	if n, err := RenderFigures(pdf, out, 72, ""); err != nil || n != nPages {
		t.Fatalf("GOMAXPROCS=%d: RenderFigures = %d, %v", procs, n, err)
	}
	docsMu.Lock()
	d := docs[pdf]
	docsMu.Unlock()
	if d.textRead || d.pages != nil {
		t.Errorf("GOMAXPROCS=%d: RenderFigures read the text of every page", procs)
	}
	CloseDoc(pdf)
	return filepath.Join(out, "figures")
}

func readFigure(t *testing.T, dir string, page int) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("p%d.png", page)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// writeSquaresPDF は、ページごとに黒い正方形の位置を変えた PDF を合成する
// (各ページの PNG が互いに違うようにするため)。著作物の PDF は使わない。
func writeSquaresPDF(t *testing.T, path string, pages int) {
	t.Helper()
	kids := make([]string, pages)
	objects := make([]string, 2, 2+2*pages)
	objects[0] = "<< /Type /Catalog /Pages 2 0 R >>"
	for i := range pages {
		page := len(objects) + 1
		kids[i] = fmt.Sprintf("%d 0 R", page)
		content := fmt.Sprintf("0 0 0 rg %d %d 8 8 re f BT /F1 12 Tf 2 20 Td (page-%d) Tj ET", 4*i, 4*i, i+1)
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 40 40] /Contents %d 0 R /Resources << /Font << /F1 %d 0 R >> >> >>", page+1, 3+2*pages),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
	objects = append(objects, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pages)

	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, 0, len(objects))
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
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}
