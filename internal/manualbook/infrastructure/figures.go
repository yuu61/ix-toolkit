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
// 焼いた枚数を返す。
func RenderFigures(pdf, outDir string, dpi int, pageSpec string) (int, error) {
	d, err := openDoc(pdf)
	if err != nil {
		return 0, err
	}
	want, err := parsePageSpec(pageSpec, len(d.pages))
	if err != nil {
		return 0, err
	}

	dir := filepath.Join(outDir, "figures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}

	for _, n := range want {
		img, cleanup, err := d.renderPage(n-1, dpi)
		if err != nil {
			return 0, err
		}
		path := filepath.Join(dir, fmt.Sprintf("p%d.png", n))
		f, err := os.Create(path)
		if err != nil {
			cleanup()
			return 0, err
		}
		err = png.Encode(f, img)
		f.Close()
		cleanup() // WebAssembly では画像の裏のメモリをここで返す
		if err != nil {
			return 0, fmt.Errorf("%s の書き出しに失敗: %w", path, err)
		}
	}
	return len(want), nil
}

// parsePageSpec は "1050-1060,1100" の形を展開する。空なら全ページ。
func parsePageSpec(spec string, nPages int) ([]int, error) {
	if strings.TrimSpace(spec) == "" {
		all := make([]int, nPages)
		for i := range all {
			all[i] = i + 1
		}
		return all, nil
	}

	seen := map[int]bool{}
	var out []int
	add := func(n int) {
		if n >= 1 && n <= nPages && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, ok := strings.Cut(part, "-")
		if !ok {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("ページ指定を読めません: %q", part)
			}
			add(n)
			continue
		}
		a, err1 := strconv.Atoi(strings.TrimSpace(lo))
		b, err2 := strconv.Atoi(strings.TrimSpace(hi))
		if err1 != nil || err2 != nil || a > b {
			return nil, fmt.Errorf("ページ範囲を読めません: %q", part)
		}
		for n := a; n <= b; n++ {
			add(n)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("焼くページがありません: %q", spec)
	}
	return out, nil
}
