package application

import (
	"fmt"
	"os"
	"path/filepath"

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

	r, err := infrastructure.SplitSpreads(inputDir, outputDir, dryRun)
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
