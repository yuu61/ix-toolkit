package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/yuu61/ix-toolkit/internal/manualbook/application"
)

func runBuild(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	manifestPath := fs.String("manifest", "manifest.json", "マニフェスト JSON (profile 等の相対パスはこのファイルの場所から解く)")
	cacheDir := fs.String("cache", "pdf", "取得キャッシュの置き場 (fetch の -out と同じ)")
	manualsDir := fs.String("manuals", application.DefaultManualsDir(), "変換結果の置き場。この下に <系列>/<冊子>/ を作る ($IX_MANUALS があればそれ)")
	force := fs.Bool("force", false, "取得済みの資料も取り直す")
	only := fs.String("only", "", "この name の資料だけ取得・変換する (diff は揃っていれば作る)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "使い方: manualbook build [-force] [-only <name>]")
		fmt.Fprintln(os.Stderr, "  manifest.json の資料を pdf/ に取り、~/.ix-toolkit/manuals/<系列>/<冊子>/ に変換し、diff.tsv を作る。")
		fmt.Fprintln(os.Stderr, "  PDF の機能説明書はページ画像も焼く (焼いてあれば飛ばす)。")
		fs.PrintDefaults()
	}
	parseFlags(fs, args)

	if err := application.Build(*manifestPath, *cacheDir, *manualsDir, *force, *only); err != nil {
		fatal(err)
	}
}
