package infrastructure

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// RenderFigures はページを焼いて <outDir>/figures/p<ページ番号>.png に置く。
// 焼いた枚数を返す。文字は使わないので読まない (openPDF)。
//
// 1 枚ごとに描画と PNG の書き出しがあり、書き出しの方が重い。両方を並列処理用の
// インスタンスに載せ、ページを並べて焼く。描画した画像は焼いたインスタンスの
// メモリの上にあるので、書き出しと後始末も同じインスタンスの番のうちに済ませる。
func RenderFigures(pdf, outDir string, dpi int, pageSpec string) (int, error) {
	d, err := openPDF(pdf)
	if err != nil {
		return 0, err
	}
	want, err := parsePageSpec(pageSpec, d.count)
	if err != nil {
		return 0, err
	}

	dir := filepath.Join(outDir, "figures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}

	if err := d.forEach(len(want), func(worker *pdfDoc, k int) error {
		return worker.writeFigure(dir, want[k], dpi)
	}); err != nil {
		return 0, err
	}
	return len(want), nil
}

// writeFigure はページ n (1 始まり) を焼いて <dir>/p<n>.png に書く。
func (d *pdfDoc) writeFigure(dir string, n, dpi int) error {
	img, cleanup, err := d.renderPage(n-1, dpi)
	if err != nil {
		return err
	}
	defer cleanup() // WebAssembly では画像の裏のメモリをここで返す
	path := filepath.Join(dir, fmt.Sprintf("p%d.png", n))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = png.Encode(f, img)
	_ = f.Close()
	if err != nil {
		return fmt.Errorf("%s の書き出しに失敗: %w", path, err)
	}
	return nil
}

// parsePageSpec は "1050-1060,1100" の形を展開する。空なら全ページ。
func parsePageSpec(spec string, nPages int) ([]int, error) {
	if strings.TrimSpace(spec) == "" {
		return allPageNumbers(nPages), nil
	}

	seen := map[int]bool{}
	var out []int
	add := func(n int) {
		if n >= 1 && n <= nPages && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		a, b, err := pageRange(part)
		if err != nil {
			return nil, err
		}

		for n := max(a, 1); n <= min(b, nPages); n++ {
			add(n)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("焼くページがありません: %q", spec)
	}
	return out, nil
}

func pageRange(part string) (int, int, error) {
	lo, hi, ok := strings.Cut(part, "-")
	if !ok {
		n, err := strconv.Atoi(part)
		if err != nil {
			return 0, 0, fmt.Errorf("ページ指定を読めません: %q", part)
		}
		return n, n, nil
	}
	a, err1 := strconv.Atoi(strings.TrimSpace(lo))
	b, err2 := strconv.Atoi(strings.TrimSpace(hi))
	if err1 != nil || err2 != nil || a > b {
		return 0, 0, fmt.Errorf("ページ範囲を読めません: %q", part)
	}
	return a, b, nil
}

func allPageNumbers(nPages int) []int {
	all := make([]int, nPages)
	for i := range all {
		all[i] = i + 1
	}
	return all
}
