package application

import (
	"fmt"
	"path/filepath"

	"github.com/yuu61/ix-toolkit/internal/manualbook/infrastructure"
)

// Figures はページを PNG に焼いて <out>/figures/ に置く。
//
// 図は変換できない。ラベルはテキストとして取れるが、矢印の向き・包含関係・
// 順序は失われるので、構成や流れを答えるにはページそのものが要る。PDF への
// リンクを辿れるのは PDF ビューアを開ける人だけで、マニュアルを引く
// エージェントは #page=1057 を開けない。PNG なら読める。
//
// 焼くのは以前は外部のレンダラ (poppler の pdftoppm か Xpdf の pdftopng) の
// 仕事だった。テキストを読むのに PDFium を持つようになった今、同じ道具で
// 描画もできるので、外部レンダラは要らない。
//
// pages は焼くページの指定 (例 "1050-1060,1100")。空なら全ページ。
func Figures(pdf, out string, dpi int, pages string) error {
	defer infrastructure.CloseDoc(pdf)
	n, err := infrastructure.RenderFigures(pdf, out, dpi, pages)
	if err != nil {
		return err
	}
	fmt.Printf("%d ページを焼きました: %s\n", n, filepath.Join(out, "figures"))
	fmt.Println("囲みの直後に [ページ画像] を付けるには manualbook md を流し直してください。")
	return nil
}
