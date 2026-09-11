package infrastructure

import (
	"image"
	"image/draw"
)

// pdfRule はページ座標の水平線または垂直線。pos は固定座標、lo/hi は線の範囲。
type pdfRule struct {
	vertical    bool
	pos, lo, hi float64
}

// readRules は表示された罫線を読む。FD の表は罫線が画像で、文字だけがテキスト層に
// あるページを含むため、描画パスだけでは読めない。文字には OCR を使わない。
func (d *pdfDoc) readRules(page int) ([]pdfRule, error) {
	const dpi = 144
	img, cleanup, err := d.renderPage(page, dpi)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return imageRules(img, d.pages[page].width, d.pages[page].height), nil
}

func imageRules(img image.Image, width, height float64) []pdfRule {
	b := img.Bounds()
	gray := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(gray, gray.Bounds(), img, b.Min, draw.Src)
	sx, sy := width/float64(b.Dx()), height/float64(b.Dy())
	var rules []pdfRule
	// 文字の短い画は除く。罫線候補は後段で閉じたセル群を作れるものだけ採用する。
	for y := 0; y < b.Dy(); y++ {
		start := -1
		for x := 0; x <= b.Dx(); x++ {
			dark := x < b.Dx() && gray.Pix[y*gray.Stride+x] < 180
			if dark {
				if start < 0 {
					start = x
				}
				continue
			}
			if start >= 0 && float64(x-start)*sx >= 12 {
				rules = append(rules, pdfRule{false, height - (float64(y)+0.5)*sy, float64(start) * sx, float64(x) * sx})
			}
			start = -1
		}
	}
	for x := 0; x < b.Dx(); x++ {
		start := -1
		for y := 0; y <= b.Dy(); y++ {
			dark := y < b.Dy() && gray.Pix[y*gray.Stride+x] < 180
			if dark {
				if start < 0 {
					start = y
				}
				continue
			}
			if start >= 0 && float64(y-start)*sy >= 8 {
				rules = append(rules, pdfRule{true, (float64(x) + 0.5) * sx, height - float64(y)*sy, height - float64(start)*sy})
			}
			start = -1
		}
	}
	return rules
}
