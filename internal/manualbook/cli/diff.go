package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/yuu61/ix-toolkit/internal/manualbook/application"
)

func runDiff(args []string) {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	out := fs.String("out", "", "出力ファイル (省略で <ix-r>/diff.tsv)")
	derived := fs.String("derived", "", "手で導いた差分の TSV (省略で profiles/ix-r-derived-diff.tsv、無ければ読まない)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "使い方: manualbook diff <無印の変換結果> <IX-R の変換結果> [-out diff.tsv]")
		fmt.Fprintln(os.Stderr, "  例:   manualbook diff ~/.ix-toolkit/manuals/ix ~/.ix-toolkit/manuals/ix-r")
		fmt.Fprintln(os.Stderr, "  どちらも crm/ と fd/ を持つ系列ディレクトリ。手で導いた差分 (profiles/ix-r-derived-diff.tsv) は")
		fmt.Fprintln(os.Stderr, "  カレントか実行ファイルの隣から読む。リポジトリの外から流すなら -derived で指定する。")
		fs.PrintDefaults()
	}
	pos := parseFlags(fs, args)
	if len(pos) != 2 {
		fs.Usage()
		os.Exit(2)
	}
	if err := application.Diff(pos[0], pos[1], *out, *derived); err != nil {
		fatal(err)
	}
}
