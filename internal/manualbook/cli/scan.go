package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/yuu61/ix-toolkit/internal/manualbook/application"
)

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

	if err := application.Scan(*inputDir, *outputDir, *dryRun); err != nil {
		fatal(err)
	}
}
