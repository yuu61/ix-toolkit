package infrastructure

import (
	"bufio"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// --- 型定義 ---

type fileEntry struct {
	sortKey *big.Int
	name    string
	path    string
}

type taskInfo struct {
	img        image.Image
	action     string
	filePath   string
	fileName   string
	outName    string
	rightName  string
	leftName   string
	cropTop    int
	newHeight  int
	halfWidth  int
	rightWidth int
	srcWidth   int
}

type decodeResult struct {
	img image.Image
	err error
}

// ScanReport は SplitSpreads の集計。
type ScanReport struct {
	Input   int // 取り上げた入力ファイル数。0 なら何もしていない
	Split   int // 見開きとして左右に分けた数
	Copied  int // 1 ページとしてそのまま写した数
	Errors  int
	Skipped int
	Pages   int // 出力したページ数
}

// SplitSpreads は inputDir の見開き PNG を 1 ページずつ outputDir に置く。
// dryRun なら何をするかを出すだけで書かない。経過は標準出力に、失敗は標準エラーに出す。
func SplitSpreads(inputDir, outputDir string, dryRun bool) (ScanReport, error) {
	var report ScanReport

	// --- 1. ファイル列挙・ソート ---
	digitRe := regexp.MustCompile(`\d+`)
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		return report, fmt.Errorf("ディレクトリの読み取りに失敗: %w", err)
	}

	var validFiles []fileEntry
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".png") {
			continue
		}
		baseName := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		matches := digitRe.FindAllString(baseName, -1)
		if len(matches) != 1 {
			fmt.Fprintf(os.Stderr, "数字列が1つでないためスキップ: %s (検出数: %d)\n", e.Name(), len(matches))
			continue
		}
		n := new(big.Int)
		n.SetString(matches[0], 10)
		validFiles = append(validFiles, fileEntry{
			name:    e.Name(),
			path:    filepath.Join(inputDir, e.Name()),
			sortKey: n,
		})
	}

	if len(validFiles) == 0 {
		return report, nil
	}
	report.Input = len(validFiles)

	slices.SortFunc(validFiles, func(a, b fileEntry) int {
		return a.sortKey.Cmp(b.sortKey)
	})

	// --- 2. 命名パターン抽出 ---
	firstBase := strings.TrimSuffix(validFiles[0].name, filepath.Ext(validFiles[0].name))
	ext := filepath.Ext(validFiles[0].name)
	loc := digitRe.FindStringIndex(firstBase)
	prefix := firstBase[:loc[0]]
	suffix := firstBase[loc[1]:]
	inputDigitWidth := loc[1] - loc[0]

	maxPages := len(validFiles) * 2
	neededWidth := 1
	if maxPages > 0 {
		neededWidth = int(math.Ceil(math.Log10(float64(maxPages + 1))))
	}
	digitWidth := max(neededWidth, inputDigitWidth)

	// --- 3. パイプライン: 並列デコード → 順序付き分類 → 並列処理 ---
	// 上下それぞれ入力画像の高さの7%をクロップ (例: 3037px -> 212px)
	const cropPercent = 7
	const whiteThreshold byte = 250

	counter := 0
	splitCount := 0
	copyCount := 0
	errorCount := 0
	skipCount := 0
	numWorkers := runtime.GOMAXPROCS(0)

	var (
		taskCh         chan taskInfo
		processWg      sync.WaitGroup
		mu             sync.Mutex
		parallelErrors atomic.Int64
		failedPages    atomic.Int64
	)

	// --- 3a. 処理ワーカープール起動（DryRun以外） ---
	if !dryRun {
		taskCh = make(chan taskInfo, numWorkers)
		for range numWorkers {
			processWg.Go(func() {
				for t := range taskCh {
					if err := processTask(&t, outputDir); err != nil {
						mu.Lock()
						fmt.Fprintf(os.Stderr, "処理失敗: %s - %s\n", t.fileName, err)
						mu.Unlock()
						parallelErrors.Add(1)
						pages := int64(1)
						if t.action == "split" {
							pages = 2
						}
						failedPages.Add(pages)
					} else {
						mu.Lock()
						if t.action == "copy" {
							fmt.Printf("コピー: %s -> %s\n", t.fileName, t.outName)
						} else {
							fmt.Printf("分割: %s -> %s, %s\n", t.fileName, t.leftName, t.rightName)
						}
						mu.Unlock()
					}
				}
			})
		}
	}

	// --- 3b. 先行デコードパイプライン ---
	// permits がバックプレッシャーを制御:
	//   メインgoroutineが結果を消費 → permit返却 → 次のデコード開始
	//   同時にメモリ上に存在するデコード済み画像は最大 numWorkers 枚
	resultChs := make([]chan decodeResult, len(validFiles))
	for i := range resultChs {
		resultChs[i] = make(chan decodeResult, 1)
	}

	permits := make(chan struct{}, numWorkers)
	for range numWorkers {
		permits <- struct{}{}
	}

	go func() {
		for i, f := range validFiles {
			<-permits // permit取得（メインgoroutineが返却するまでブロック）
			go func() {
				img, err := loadPNG(f.path)
				resultChs[i] <- decodeResult{img, err}
			}()
		}
	}()

	// --- 3c. 分類ループ（順序保証、シーケンシャル） ---
	for i, f := range validFiles {
		result := <-resultChs[i]
		permits <- struct{}{} // permit返却 → 次のデコードを許可
		resultChs[i] = nil    // チャネル解放

		if result.err != nil {
			fmt.Fprintf(os.Stderr, "分類失敗: %s - %s\n", f.name, result.err)
			errorCount++
			continue
		}

		img := result.img
		bounds := img.Bounds()
		srcWidth := bounds.Dx()
		srcHeight := bounds.Dy()
		cropTop := srcHeight * cropPercent / 100
		newHeight := srcHeight - 2*cropTop

		if newHeight <= 0 {
			fmt.Fprintf(os.Stderr, "スキップ(高さ不足): %s (%dpx)\n", f.name, srcHeight)
			skipCount++
			continue
		}

		halfWidth := srcWidth / 2
		centerStart := max(0, halfWidth-16)
		centerEnd := min(srcWidth, halfWidth+16)
		hasCenterContent := !isCenterStripeAllWhite(img, centerStart, centerEnd, cropTop, newHeight, whiteThreshold)

		if hasCenterContent {
			counter++
			outName := fmt.Sprintf("%s%0*d%s%s", prefix, digitWidth, counter, suffix, ext)
			if dryRun {
				fmt.Printf("[DryRun] コピー: %s -> %s\n", f.name, outName)
			} else {
				taskCh <- taskInfo{
					action:    "copy",
					filePath:  f.path,
					fileName:  f.name,
					outName:   outName,
					cropTop:   cropTop,
					newHeight: newHeight,
					srcWidth:  srcWidth,
					img:       img,
				}
			}
			copyCount++
		} else {
			counter++
			rightWidth := srcWidth - halfWidth
			leftName := fmt.Sprintf("%s%0*d%s%s", prefix, digitWidth, counter, suffix, ext)
			counter++
			rightName := fmt.Sprintf("%s%0*d%s%s", prefix, digitWidth, counter, suffix, ext)
			if dryRun {
				fmt.Printf("[DryRun] 分割: %s -> %s, %s\n", f.name, leftName, rightName)
			} else {
				taskCh <- taskInfo{
					action:     "split",
					filePath:   f.path,
					fileName:   f.name,
					rightName:  rightName,
					leftName:   leftName,
					cropTop:    cropTop,
					newHeight:  newHeight,
					halfWidth:  halfWidth,
					rightWidth: rightWidth,
					srcWidth:   srcWidth,
					img:        img,
				}
			}
			splitCount++
		}
	}

	// ワーカー完了待ち
	if !dryRun {
		close(taskCh)
		processWg.Wait()
		errorCount += int(parallelErrors.Load())
	}

	// --- 4. 集計 ---
	report.Split, report.Copied, report.Errors, report.Skipped = splitCount, copyCount, errorCount, skipCount
	report.Pages = counter - int(failedPages.Load())
	return report, nil
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

// --- 画像処理タスク実行 ---

func processTask(t *taskInfo, outDir string) error {
	img := t.img
	t.img = nil // GCで解放可能にする

	if t.action == "copy" {
		cropped := cropImage(img, 0, t.cropTop, t.srcWidth, t.newHeight)
		return savePNG(filepath.Join(outDir, t.outName), cropped)
	}

	// 左半分（若い番号）
	leftPath := filepath.Join(outDir, t.leftName)
	left := cropImage(img, 0, t.cropTop, t.halfWidth, t.newHeight)
	if err := savePNG(leftPath, left); err != nil {
		return err
	}

	// 右半分
	right := cropImage(img, t.halfWidth, t.cropTop, t.rightWidth, t.newHeight)
	if err := savePNG(filepath.Join(outDir, t.rightName), right); err != nil {
		os.Remove(leftPath) // 左半分もクリーンアップ
		return err
	}
	return nil
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
