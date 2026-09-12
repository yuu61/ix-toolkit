package infrastructure

import (
	"fmt"
	"image"
	"os"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

// PDF を読むエンジン。PDFium を WebAssembly で持つ (cgo も外部コマンドも要らない)。
//
// 以前は pdftotext を起動していた。やめた理由は 2 つある。
//
//  1. 必要だったのは「pdftotext」ではなく「Xpdf 4.x の pdftotext」だった。
//     -table も -marginl/r/t/b も Xpdf にしか無く、パッケージマネージャが配る
//     poppler の同名コマンドでは exit 99 と usage が返るだけで、その差は
//     LookPath では見えない。
//
//  2. 経路ごとにプロセスを起こして PDF を丸ごと読み直していた。コマンド辞書の
//     経路は 5 回 (段組み 3 + ヘッダ・フッタ 2)、解説書の経路は 3 回。
//     ライブラリなら 1 回開いて全経路で使い回せる。
//
// 版面の再構成 (-table 相当) は layout.go で自前に持つ。PDFium が返すのは
// 文字とその外接矩形までで、桁を揃えるのはこちら側の仕事になる。

// glyph は 1 文字とその外接矩形 (PDF ポイント、原点は左下)。
type glyph struct {
	r           rune
	left, right float64
	top, bottom float64
	fontSize    float64 // 変換行列を反映したポイント数。見出しと本文の区別に使う。

	// spaceBefore は、この字の前に空白文字が入っていたことを表す。
	//
	// 語の切れ目を座標だけから当てるのは無理だった。字面の狭い字は外接矩形が
	// 送りよりずっと狭く、隙間で測ると語の中で空きに見える。送りで測ると
	// 今度は "default group" の空白が閾値に届かず "defaultgroup" になる。
	// PDF 自身が持っている空白をそのまま使えば、どちらも起きない。
	spaceBefore bool
}

func (g glyph) centerX() float64 { return (g.left + g.right) / 2 }

func (g glyph) centerY() float64 { return (g.top + g.bottom) / 2 }

func (g glyph) width() float64 { return g.right - g.left }

func (g glyph) height() float64 { return g.top - g.bottom }

// pdfPage は 1 ページ分の文字と、その表示上のページ寸法。
type pdfPage struct {
	width, height float64
	glyphs        []glyph
}

// pdfDoc は開いた PDF 1 冊。文字は初回アクセス時にまとめて読む。
type pdfDoc struct {
	path     string
	instance pdfium.Pdfium
	ref      references.FPDF_DOCUMENT
	pages    []pdfPage
}

var (
	engineOnce sync.Once
	engine     pdfium.Pdfium
	engineErr  error

	docsMu sync.Mutex
	docs   = map[string]*pdfDoc{}
)

// engineInstance は PDFium を 1 度だけ立ち上げ、プロセスで 1 つのインスタンスを
// 使い回す。プールにインスタンスは 1 つしか無いので、冊ごとに取りに行くと
// 2 冊目 (build で CRM の次に FD を開くとき) が空くのを待ち続けて時間切れになる。
// 1 つのインスタンスで複数の文書を開けるので、取るのは 1 回でよい。
func engineInstance() (pdfium.Pdfium, error) {
	engineOnce.Do(func() {
		pool, err := webassembly.Init(webassembly.Config{
			MinIdle: 1, MaxIdle: 1, MaxTotal: 1,
		})
		if err != nil {
			engineErr = fmt.Errorf("PDFium の初期化に失敗: %w", err)
			return
		}
		if engine, err = pool.GetInstance(30 * time.Second); err != nil {
			engineErr = fmt.Errorf("PDFium の取得に失敗: %w", err)
		}
	})
	return engine, engineErr
}

// openDoc は PDF を開く。同じパスなら開き直さず使い回す。
func openDoc(path string) (*pdfDoc, error) {
	docsMu.Lock()
	defer docsMu.Unlock()
	if d, ok := docs[path]; ok {
		return d, nil
	}

	inst, err := engineInstance()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	res, err := inst.OpenDocument(&requests.OpenDocument{File: &b})
	if err != nil {
		return nil, fmt.Errorf("PDF を開けません (%s): %w", path, err)
	}

	d := &pdfDoc{path: path, instance: inst, ref: res.Document}
	if err := d.readPages(); err != nil {
		return nil, err
	}
	docs[path] = d
	return d, nil
}

// CloseDoc は openDoc で開いた PDF を閉じ、文字の配列と PDFium 側の資源を返す。
// 1 冊で終わるサブコマンドでは要らないが、build は続けて次の冊を開く。
func CloseDoc(path string) {
	docsMu.Lock()
	defer docsMu.Unlock()
	d, ok := docs[path]
	if !ok {
		return
	}
	_, _ = d.instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: d.ref})
	delete(docs, path)
}

// page は 0 始まりのページ参照を作る。
func (d *pdfDoc) page(i int) requests.Page {
	return requests.Page{ByIndex: &requests.PageByIndex{Document: d.ref, Index: i}}
}

// readPages は全ページの文字と寸法を読む。1208 ページで 11 秒ほど。
func (d *pdfDoc) readPages() error {
	cnt, err := d.instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: d.ref})
	if err != nil {
		return err
	}
	d.pages = make([]pdfPage, cnt.PageCount)
	for i := 0; i < cnt.PageCount; i++ {
		size, err := d.instance.GetPageSize(&requests.GetPageSize{Page: d.page(i)})
		if err != nil {
			return fmt.Errorf("ページ %d の寸法を取得できません: %w", i+1, err)
		}
		txt, err := d.instance.GetPageTextStructured(&requests.GetPageTextStructured{
			Page:                   d.page(i),
			Mode:                   requests.GetPageTextStructuredModeChars,
			CollectFontInformation: true,
		})
		if err != nil {
			return fmt.Errorf("ページ %d の文字を取得できません: %w", i+1, err)
		}

		pg := pdfPage{width: size.Width, height: size.Height}
		pg.glyphs = make([]glyph, 0, len(txt.Chars))
		space := false
		for _, c := range txt.Chars {
			for _, r := range c.Text {
				// 空白と改行は外接矩形が当てにならない (潰れていることが
				// ある) ので、次の字に「前に空白があった」印だけ残す。
				if isSpace(r) {
					space = true
					continue
				}
				// PDFium は行末の折り返しハイフンを U+0002 で返す。落とすと
				// "[suppress-" が "[suppress" になり、次行と繋いだときに
				// "suppress restoration" という存在しない綴りになる。
				if r == 0x02 {
					r = '-'
				}
				if r < 0x20 {
					continue
				}
				fontSize := 0.0
				if c.FontInformation != nil {
					fontSize = c.FontInformation.RenderedSize
					if fontSize <= 0 {
						fontSize = c.FontInformation.Size
					}
				}
				pg.glyphs = append(pg.glyphs, glyph{
					r:           r,
					left:        c.PointPosition.Left,
					right:       c.PointPosition.Right,
					top:         c.PointPosition.Top,
					bottom:      c.PointPosition.Bottom,
					spaceBefore: space,
					fontSize:    fontSize,
				})
				space = false
			}
		}
		d.pages[i] = pg
	}
	return nil
}

// renderPage はページを PNG 用の画像に焼く。呼び出し側が cleanup を呼ぶ。
func (d *pdfDoc) renderPage(i, dpi int) (image.Image, func(), error) {
	res, err := d.instance.RenderPageInDPI(&requests.RenderPageInDPI{
		Page: d.page(i),
		DPI:  dpi,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("ページ %d を描画できません: %w", i+1, err)
	}
	return res.Result.RenderedImage, res.Cleanup, nil
}

func isSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\f', '\v', 0x00a0, 0x3000, 0xfeff, 0:
		return true
	}
	return false
}
