package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// figures サブコマンド: ページを PNG に焼いて figures/ に置く。
//
// 図は変換できない。ラベルはテキストとして取れるが、矢印の向き・包含関係・
// 順序は失われるので、構成や流れを答えるにはページそのものが要る。PDF への
// リンクを辿れるのは PDF ビューアを開ける人だけで、マニュアルを引く
// エージェントは #page=1057 を開けない。PNG なら読める。
//
// 焼くのは以前は外部のレンダラ (poppler の pdftoppm か Xpdf の pdftopng) の
// 仕事だった。テキストを読むのに PDFium を持つようになった今、同じ道具で
// 描画もできるので、外部レンダラは要らない。

func runFigures(args []string) {
	fs := flag.NewFlagSet("figures", flag.ExitOnError)
	out := fs.String("out", "out", "出力先ディレクトリ (この下の figures/ に置く)")
	dpi := fs.Int("dpi", 150, "解像度")
	pages := fs.String("pages", "", "焼くページ (例 1050-1060,1100)。省略で全ページ")
	pos := parseFlags(fs, args)

	if len(pos) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: manualbook figures <pdf> [-out out/] [-dpi 150] [-pages 1050-1060]")
		os.Exit(1)
	}

	n, err := renderFigures(pos[0], *out, *dpi, *pages)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("%d ページを焼きました: %s\n", n, filepath.Join(*out, "figures"))
	fmt.Println("囲みの直後に [ページ画像] を付けるには manualbook md を流し直してください。")
}

// renderFigures はページを焼いて <outDir>/figures/p<ページ番号>.png に置く。
// 焼いた枚数を返す。
func renderFigures(pdf, outDir string, dpi int, pageSpec string) (int, error) {
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
