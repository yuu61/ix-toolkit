package infrastructure

import (
	"fmt"
	"image"
	"os"
	"runtime"
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
	data     []byte // 並列処理のインスタンスも同じ原本を開く。
	instance pdfium.Pdfium
	ref      references.FPDF_DOCUMENT
	pages    []pdfPage
}

var (
	engineOnce  sync.Once
	engine      pdfium.Pdfium
	enginePools [maxPDFWorkers]pdfium.Pool
	engineErr   error

	docsMu sync.Mutex
	docs   = map[string]*pdfDoc{}
)

// 主インスタンスは冊をまたいで使い回す。並列処理用のプールは必要時に作る。
// wazero v1.12.0 の Emscripten 呼び出しは型情報を遅延初期化するため、
// 同一 Runtime 内の複数インスタンスでも競合する。Runtime ごと分離する。
const maxPDFWorkers = 4

func engineInstance() (pdfium.Pdfium, error) {
	engineOnce.Do(func() {
		pool, err := webassembly.Init(webassembly.Config{
			MinIdle: 1, MaxIdle: 1, MaxTotal: 1,
		})
		if err != nil {
			engineErr = fmt.Errorf("PDFium の初期化に失敗: %w", err)
			return
		}
		enginePools[0] = pool
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

	d := &pdfDoc{path: path, data: b, instance: inst, ref: res.Document}
	if err := d.readPages(); err != nil {
		_, _ = inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: d.ref})
		return nil, err
	}
	docs[path] = d
	return d, nil
}

// CloseDoc は openDoc で開いた PDF を閉じ、文字の配列と PDFium 側の資源を返す。
// application が PDF を使う一連の処理の終了時に呼ぶ。途中の失敗でも解放する。
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

// forPages は各インスタンスを 1 つの goroutine だけで使う。
// f は自分のページの結果だけを書き、他ページの結果は全処理の終了後に読む。
// 呼び出し中は、呼び出し元も d.instance に触れない。
func (d *pdfDoc) forPages(f func(*pdfDoc, int) error) error {
	n := min(maxPDFWorkers, runtime.GOMAXPROCS(0), len(d.pages))
	if n <= 1 {
		for i := range d.pages {
			if err := f(d, i); err != nil {
				return err
			}
		}
		return nil
	}
	workers := []*pdfDoc{d}
	for len(workers) < n {
		k := len(workers)
		if enginePools[k] == nil {
			pool, err := webassembly.Init(webassembly.Config{MinIdle: 1, MaxIdle: 1, MaxTotal: 1})
			if err != nil {
				return fmt.Errorf("並列処理用 PDFium の初期化に失敗: %w", err)
			}
			enginePools[k] = pool
		}
		inst, err := enginePools[k].GetInstance(30 * time.Second)
		if err != nil {
			return fmt.Errorf("並列処理用 PDFium の取得に失敗: %w", err)
		}
		defer inst.Close()
		res, err := inst.OpenDocument(&requests.OpenDocument{File: &d.data})
		if err != nil {
			return fmt.Errorf("並列処理用 PDF を開けません (%s): %w", d.path, err)
		}
		defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: res.Document})
		workers = append(workers, &pdfDoc{path: d.path, data: d.data, instance: inst, ref: res.Document, pages: d.pages})
	}
	var wg sync.WaitGroup
	errs := make([]error, n)
	for k, worker := range workers {
		wg.Go(func() {
			for i := k; i < len(d.pages); i += n {
				if err := f(worker, i); err != nil {
					errs[k] = err
					return
				}
			}
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// readPages は全ページの文字と寸法を読み、ページ番号の位置に格納する。
func (d *pdfDoc) readPages() error {
	cnt, err := d.instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: d.ref})
	if err != nil {
		return err
	}
	d.pages = make([]pdfPage, cnt.PageCount)
	return d.forPages(func(worker *pdfDoc, i int) error {
		size, err := worker.instance.GetPageSize(&requests.GetPageSize{Page: worker.page(i)})
		if err != nil {
			return fmt.Errorf("ページ %d の寸法を取得できません: %w", i+1, err)
		}
		txt, err := worker.instance.GetPageTextStructured(&requests.GetPageTextStructured{
			Page:                   worker.page(i),
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
		return nil
	})
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
