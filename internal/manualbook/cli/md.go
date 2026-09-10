package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/yuu61/ix-toolkit/internal/manualbook/application"
)

func runMD(args []string) {
	fs := flag.NewFlagSet("md", flag.ExitOnError)
	var o application.MDOptions
	fs.StringVar(&o.ProfilePath, "profile", "", "プロファイル JSON (省略時は NEC IX CRM 用の既定値)")
	fs.StringVar(&o.OutDir, "out", "out", "出力ディレクトリ")
	fs.StringVar(&o.Title, "title", "", "資料タイトル (省略時は PDF のファイル名)")
	fs.StringVar(&o.SourceLabel, "source", "", "出典表記 (取得元 URL 等。README に記載する)")
	fs.StringVar(&o.Series, "series", "", "機種の系列 (ix / ix-r。README に記載する)")
	fs.StringVar(&o.Version, "version", "", "資料の版 (README に記載する)")
	fs.BoolVar(&o.Figures, "figures", false, "ページ画像も焼く (figures/ に置き、囲みから辿れるようにする)")
	fs.IntVar(&o.FigureDPI, "figure-dpi", 150, "-figures のときの解像度")
	fs.StringVar(&o.FigurePages, "figure-pages", "", "-figures で焼くページ (例 1050-1060)。省略で全ページ")
	pos := parseFlags(fs, args)

	if len(pos) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: manualbook md <pdf | 取得キャッシュのディレクトリ> [-profile profile.json] [-out out/] [-figures]")
		os.Exit(1)
	}
	o.Input = pos[0]
	if err := application.Convert(o); err != nil {
		fatal(err)
	}
}
