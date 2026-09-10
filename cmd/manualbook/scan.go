package main

import (
	"bufio"
	"flag"
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

// --- scan サブコマンド ---
//
// 見開きスキャン画像 (PNG) を 1 ページずつに分割する。
// テキスト層を持たない紙スキャン由来の PDF を画像化したあとの前処理であり、
// テキスト層のある PDF では md サブコマンドを使うこと（分割も OCR も不要）。

func runScan(args []string) {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	inputDir := fs.String("input", "", "入力ディレクトリ (必須)")
	outputDir := fs.String("output", "", "出力ディレクトリ (デフォルト: <input>/processed)")
	dryRun := fs.Bool("dry-run", false, "DryRunモード")
	pos := parseFlags(fs, args)

	if *inputDir == "" {
		if len(pos) > 0 {
			*inputDir = pos[0]
		} else {
			fmt.Fprintln(os.Stderr, "Error: 入力ディレクトリを指定してください")
			fmt.Fprintln(os.Stderr, "Usage: manualbook scan -input <dir> [-output <dir>] [-dry-run]")
			os.Exit(1)
		}
	}

	info, err := os.Stat(*inputDir)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "ディレクトリが見つかりません: %s\n", *inputDir)
		os.Exit(1)
	}

	if *outputDir == "" {
		*outputDir = filepath.Join(*inputDir, "processed")
	}
	if !*dryRun {
		if mkdirErr := os.MkdirAll(*outputDir, 0o755); mkdirErr != nil {
			fmt.Fprintf(os.Stderr, "出力ディレクトリの作成に失敗: %s\n", mkdirErr)
			os.Exit(1)
		}
	}

	// --- 1. ファイル列挙・ソート ---
	digitRe := regexp.MustCompile(`\d+`)
	entries, err := os.ReadDir(*inputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ディレクトリの読み取りに失敗: %s\n", err)
		os.Exit(1)
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
			path:    filepath.Join(*inputDir, e.Name()),
			sortKey: n,
		})
	}

	if len(validFiles) == 0 {
		fmt.Fprintln(os.Stderr, "処理対象のファイルがありません")
		os.Exit(0)
	}

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
	if !*dryRun {
		taskCh = make(chan taskInfo, numWorkers)
		for range numWorkers {
			processWg.Go(func() {
				for t := range taskCh {
					if err := processTask(&t, *outputDir); err != nil {
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
			if *dryRun {
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
			if *dryRun {
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
	if !*dryRun {
		close(taskCh)
		processWg.Wait()
		errorCount += int(parallelErrors.Load())
	}

	// --- 4. 完了レポート ---
	totalPages := counter - int(failedPages.Load())
	fmt.Println()
	if *dryRun {
		fmt.Println("=== DryRun完了 ===")
	} else {
		fmt.Println("=== 処理完了 ===")
	}
	fmt.Printf("  分割: %d件\n", splitCount)
	fmt.Printf("  コピー: %d件\n", copyCount)
	fmt.Printf("  エラー: %d件\n", errorCount)
	fmt.Printf("  スキップ: %d件\n", skipCount)
	fmt.Printf("  合計出力ページ数: %d\n", totalPages)
	fmt.Printf("  出力先: %s\n", *outputDir)
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
