package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/yuu61/ix-toolkit/internal/manualbook/application"
)

func runFigures(args []string) {
	fs := flag.NewFlagSet("figures", flag.ExitOnError)
	out := fs.String("out", "out", "出力先ディレクトリ (この下の figures/ に置く)")
	dpi := fs.Int("dpi", 150, "解像度")
	pages := fs.String("pages", "", "焼くページ (例 1050-1060,1100)。省略で全ページ")
	pos := parseFlags(fs, args)

	if len(pos) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: manualbook figures <pdf> [-out out/] [-dpi 150] [-pages 1050-1060]")
		os.Exit(1)
	}

	if err := application.Figures(pos[0], *out, *dpi, *pages); err != nil {
		fatal(err)
	}
}
