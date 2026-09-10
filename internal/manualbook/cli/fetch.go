package cli

import (
	"flag"
	"time"

	"github.com/yuu61/ix-toolkit/internal/manualbook/application"
)

func runFetch(args []string) {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	manifestPath := fs.String("manifest", "manifest.json", "マニフェスト JSON")
	outDir := fs.String("out", "pdf", "保存先 (pdf はここに <name>.pdf、web は <name>/ を置く)")
	force := fs.Bool("force", false, "既存ファイルがあっても再取得する")
	only := fs.String("only", "", "この name の資料だけ取得する")
	timeout := fs.Duration("timeout", 5*time.Minute, "1 件あたりのタイムアウト")
	delay := fs.Duration("delay", time.Second, "web: リクエストの間隔")
	ua := fs.String("user-agent", application.DefaultUserAgent, "web: User-Agent")
	parseFlags(fs, args)

	if err := application.Fetch(*manifestPath, *outDir, *force, *only, *timeout, *delay, *ua); err != nil {
		fatal(err)
	}
}
