package infrastructure

import (
	"bufio"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"iter"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// ListPNGFiles は入力ディレクトリの PNG ファイル名を返す。
func ListPNGFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".png") {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// Spread は上下の余白を除き、中央の白帯で分類した画像。ページは左から右の順。
// 出力先、連番、dry-run、失敗後の続行は呼び出し元が決める。
type Spread struct{ pages []image.Image }

func (s *Spread) Pages() int { return len(s.pages) }

func readSpread(path string) (*Spread, error) {
	img, err := loadPNG(path)
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	const cropPercent = 7
	const whiteThreshold byte = 250
	top := height * cropPercent / 100
	height -= 2 * top
	if height <= 0 {
		return nil, fmt.Errorf("高さ不足: %dpx", bounds.Dy())
	}
	half := width / 2
	if !isCenterStripeAllWhite(img, max(0, half-16), min(width, half+16), top, height, whiteThreshold) {
		return &Spread{pages: []image.Image{cropImage(img, 0, top, width, height)}}, nil
	}
	return &Spread{pages: []image.Image{
		cropImage(img, 0, top, half, height),
		cropImage(img, half, top, width-half, height),
	}}, nil
}

// ReadSpreads は最大 GOMAXPROCS 枚を先読みし、指定された順で解析結果を返す。
// 途中で反復を止めてもワーカーを終了する。読み込み失敗もその位置で返す。
func ReadSpreads(paths []string) iter.Seq2[*Spread, error] {
	return func(yield func(*Spread, error) bool) {
		n := min(runtime.GOMAXPROCS(0), len(paths))
		if n == 0 {
			return
		}
		type result struct {
			spread *Spread
			err    error
		}
		results := make([]chan result, len(paths))
		for i := range results {
			results[i] = make(chan result, 1)
		}
		jobs := make(chan int, n)
		var workers sync.WaitGroup
		for range n {
			workers.Go(func() {
				for i := range jobs {
					spread, err := readSpread(paths[i])
					results[i] <- result{spread, err}
				}
			})
		}
		defer func() { close(jobs); workers.Wait() }()
		for i := range n {
			jobs <- i
		}
		for i := range paths {
			r := <-results[i]
			if !yield(r.spread, r.err) {
				return
			}
			if i+n < len(paths) {
				jobs <- i + n
			}
		}
	}
}

type spreadWrite struct {
	spread *Spread
	paths  []string
	result chan error
}

// SpreadWriter は指定されたページを並列保存する。キューにも上限を設ける。
// Close はすべての保存の完了を待つ。個別の失敗は Submit の戻り値から受け取る。
type SpreadWriter struct {
	jobs    chan spreadWrite
	workers sync.WaitGroup
}

func NewSpreadWriter() *SpreadWriter {
	n := runtime.GOMAXPROCS(0)
	w := &SpreadWriter{jobs: make(chan spreadWrite, n)}
	for range n {
		w.workers.Go(func() {
			for job := range w.jobs {
				job.result <- job.spread.save(job.paths)
			}
		})
	}
	return w
}

// Submit の後は、呼び出し元は spread を変更しない。
func (w *SpreadWriter) Submit(spread *Spread, paths []string) <-chan error {
	result := make(chan error, 1)
	w.jobs <- spreadWrite{spread, append([]string(nil), paths...), result}
	return result
}

func (w *SpreadWriter) Close() { close(w.jobs); w.workers.Wait() }

func (s *Spread) save(paths []string) error {
	if len(paths) != len(s.pages) {
		return fmt.Errorf("画像と出力先の数が一致しません")
	}
	for i, page := range s.pages {
		if err := savePNG(paths[i], page); err != nil {
			for _, path := range paths[:i] {
				_ = os.Remove(path)
			}
			return err
		}
	}
	return nil
}

// --- PNG読み込み（バッファ付き） ---

func loadPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(bufio.NewReaderSize(f, 1<<16))
}

// --- 中央帯の白判定（閾値付き・型別に最適化） ---

func isCenterStripeAllWhite(img image.Image, centerStart, centerEnd, yOffset, height int, threshold byte) bool {
	switch m := img.(type) {
	case *image.NRGBA:
		for y := range height {
			off := (yOffset+y)*m.Stride + centerStart*4
			for x := centerStart; x < centerEnd; x++ {
				if m.Pix[off] < threshold || m.Pix[off+1] < threshold || m.Pix[off+2] < threshold {
					return false
				}
				off += 4
			}
		}
		return true
	case *image.RGBA:
		for y := range height {
			off := (yOffset+y)*m.Stride + centerStart*4
			for x := centerStart; x < centerEnd; x++ {
				if m.Pix[off] < threshold || m.Pix[off+1] < threshold || m.Pix[off+2] < threshold {
					return false
				}
				off += 4
			}
		}
		return true
	case *image.Gray:
		for y := range height {
			off := (yOffset+y)*m.Stride + centerStart
			for x := centerStart; x < centerEnd; x++ {
				if m.Pix[off] < threshold {
					return false
				}
				off++
			}
		}
		return true
	case *image.Paletted:
		// パレット事前評価で高速化
		isWhite := make([]bool, len(m.Palette))
		threshold16 := uint32(threshold) * 257
		for i, c := range m.Palette {
			r, g, b, a := c.RGBA()
			isWhite[i] = r >= threshold16 && g >= threshold16 && b >= threshold16 && a >= threshold16
		}
		for y := range height {
			off := (yOffset+y)*m.Stride + centerStart
			for x := centerStart; x < centerEnd; x++ {
				if !isWhite[m.Pix[off]] {
					return false
				}
				off++
			}
		}
		return true
	default:
		threshold16 := uint32(threshold) * 257
		for y := yOffset; y < yOffset+height; y++ {
			for x := centerStart; x < centerEnd; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r < threshold16 || g < threshold16 || b < threshold16 {
					return false
				}
			}
		}
		return true
	}
}

// --- 画像切り出し ---

func cropImage(src image.Image, x, y, w, h int) image.Image {
	// SubImageが使えればゼロコピー
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if si, ok := src.(subImager); ok {
		return si.SubImage(image.Rect(x, y, x+w, y+h))
	}
	// フォールバック: 手動コピー
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, image.Pt(x, y), draw.Src)
	return dst
}

// --- PNG保存（バッファ付き） ---

func savePNG(path string, img image.Image) (retErr error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); retErr == nil {
			retErr = cerr
		}
		if retErr != nil {
			os.Remove(path)
		}
	}()

	w := bufio.NewWriterSize(f, 1<<16)
	if err := png.Encode(w, img); err != nil {
		return err
	}
	return w.Flush()
}
