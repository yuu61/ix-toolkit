package application

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/yuu61/ix-toolkit/internal/manualbook/infrastructure"
)

// Scan は見開きスキャン画像 (PNG) を 1 ページずつに分割する。
//
// テキスト層を持たない紙スキャン由来の PDF を画像化したあとの前処理であり、
// テキスト層のある PDF では md サブコマンドを使うこと (分割も OCR も不要)。
// outputDir が空なら <inputDir>/processed に置く。
func Scan(inputDir, outputDir string, dryRun bool) error {
	info, err := os.Stat(inputDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("ディレクトリが見つかりません: %s", inputDir)
	}
	if outputDir == "" {
		outputDir = filepath.Join(inputDir, "processed")
	}
	if !dryRun {
		if mkdirErr := os.MkdirAll(outputDir, 0o755); mkdirErr != nil {
			return fmt.Errorf("出力ディレクトリの作成に失敗: %w", mkdirErr)
		}
	}

	r, err := splitSpreads(inputDir, outputDir, dryRun)
	if err != nil {
		return err
	}
	if r.Input == 0 {
		fmt.Fprintln(os.Stderr, "処理対象のファイルがありません")
		return nil
	}

	fmt.Println()
	if dryRun {
		fmt.Println("=== DryRun完了 ===")
	} else {
		fmt.Println("=== 処理完了 ===")
	}
	fmt.Printf("  分割: %d件\n", r.Split)
	fmt.Printf("  コピー: %d件\n", r.Copied)
	fmt.Printf("  エラー: %d件\n", r.Errors)
	fmt.Printf("  スキップ: %d件\n", r.Skipped)
	fmt.Printf("  合計出力ページ数: %d\n", r.Pages)
	fmt.Printf("  出力先: %s\n", outputDir)
	if r.Errors > 0 {
		return ReportedError(1)
	}
	return nil
}

// scanReport は選別・実行・保存の結果。画像処理層はこの集計や表示を持たない。
type scanReport struct{ Input, Split, Copied, Errors, Skipped, Pages int }

func splitSpreads(inputDir, outputDir string, dryRun bool) (scanReport, error) {
	var report scanReport
	names, err := infrastructure.ListPNGFiles(inputDir)
	if err != nil {
		return report, fmt.Errorf("ディレクトリの読み取りに失敗: %w", err)
	}
	type fileEntry struct {
		name    string
		sortKey *big.Int
	}
	var files []fileEntry
	digitRe := regexp.MustCompile(`\d+`)
	for _, name := range names {
		base := strings.TrimSuffix(name, filepath.Ext(name))
		matches := digitRe.FindAllString(base, -1)
		if len(matches) != 1 {
			fmt.Fprintf(os.Stderr, "数字列が1つでないためスキップ: %s (検出数: %d)\n", name, len(matches))
			report.Skipped++
			continue
		}
		n := new(big.Int)
		n.SetString(matches[0], 10)
		files = append(files, fileEntry{name, n})
	}
	if len(files) == 0 {
		return report, nil
	}
	slices.SortStableFunc(files, func(a, b fileEntry) int { return a.sortKey.Cmp(b.sortKey) })
	report.Input = len(files)
	ext := filepath.Ext(files[0].name)
	base := strings.TrimSuffix(files[0].name, ext)
	loc := digitRe.FindStringIndex(base)
	prefix, suffix := base[:loc[0]], base[loc[1]:]
	digits := max(loc[1]-loc[0], len(strconv.Itoa(len(files)*2)))
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = filepath.Join(inputDir, f.name)
	}

	var writer *infrastructure.SpreadWriter
	if !dryRun {
		writer = infrastructure.NewSpreadWriter()
		defer writer.Close()
	}
	type pendingWrite struct {
		name    string
		outputs []string
		result  <-chan error
	}
	var pending []pendingWrite
	printResult := func(name string, outputs []string) {
		mark := ""
		if dryRun {
			mark = "[DryRun] "
		}
		if len(outputs) == 1 {
			fmt.Printf("%sコピー: %s -> %s\n", mark, name, outputs[0])
		} else {
			fmt.Printf("%s分割: %s -> %s, %s\n", mark, name, outputs[0], outputs[1])
		}
	}
	counter, i := 0, 0
	for spread, err := range infrastructure.ReadSpreads(paths) {
		f := files[i]
		i++
		if err != nil {
			fmt.Fprintf(os.Stderr, "分類失敗: %s - %s\n", f.name, err)
			report.Errors++
			continue
		}
		outputs := make([]string, spread.Pages())
		destinations := make([]string, len(outputs))
		for j := range outputs {
			counter++
			outputs[j] = fmt.Sprintf("%s%0*d%s%s", prefix, digits, counter, suffix, ext)
			destinations[j] = filepath.Join(outputDir, outputs[j])
		}
		if spread.Pages() == 1 {
			report.Copied++
		} else {
			report.Split++
		}
		if dryRun {
			printResult(f.name, outputs)
			report.Pages += len(outputs)
		} else {
			pending = append(pending, pendingWrite{f.name, outputs, writer.Submit(spread, destinations)})
		}
	}
	for _, p := range pending {
		if err := <-p.result; err != nil {
			fmt.Fprintf(os.Stderr, "処理失敗: %s - %s\n", p.name, err)
			report.Errors++
			continue
		}
		printResult(p.name, p.outputs)
		report.Pages += len(p.outputs)
	}
	return report, nil
}
