package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/yuu61/ix-toolkit/internal/manualbook/application"
)

func runProbe(args []string) {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	out := fs.String("out", "profile.json", "出力するプロファイル JSON")
	name := fs.String("name", "", "プロファイル名 (デフォルト: PDF のファイル名)")
	windows := fs.Int("windows", 3, "較正に使うサンプル窓の数")
	winSize := fs.Int("window-size", 8, "サンプル窓 1 つあたりのページ数")
	pos := parseFlags(fs, args)

	if len(pos) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: manualbook probe <pdf> [-out profile.json]")
		os.Exit(1)
	}

	if err := application.Probe(pos[0], *out, *name, *windows, *winSize); err != nil {
		fatal(err)
	}
}
