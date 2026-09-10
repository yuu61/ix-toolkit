// manualbook は NEC IX のマニュアルを Markdown に変換するためのツール。
//
// 資料は 2 系列ある。無印 (IX2000/IX3000) は PDF、IX-R/IX-V は Sphinx で組まれた
// Web ページで、どちらも同じ形の Markdown と索引 (commands.tsv / sections.tsv) にする。
//
// テキスト層を持つ PDF なら OCR は要らない。段組みとヘッダ・フッタさえ
// 処理できれば本文はそのまま取り出せる、というのがこのツールの前提である。
// 紙のスキャン由来でテキスト層が無い場合だけ、scan サブコマンドで
// 見開き画像を分割してから OCR に回す。Web は取得キャッシュの DOM をそのまま読む。
package main

import (
	"flag"
	"fmt"
	"os"
)

const usage = `manualbook — NEC IX のマニュアル (PDF / Web) を Markdown にする

使い方:
  manualbook <サブコマンド> [オプション]

サブコマンド:
  fetch   マニフェストに書いた資料をまとめて取得する (PDF は SHA256、Web は版で検証)
  probe   PDF を試し読みして段組み・ヘッダ位置を自動較正し、プロファイルを作る
  md      PDF か Web の取得キャッシュを構造つき Markdown に変換する
  diff    無印と IX-R の変換結果から系列間のコマンド対応表 diff.tsv を作る
  figures ページを PNG に焼く (PDF の図のページを画像で引けるようにする)
  scan    見開きスキャン画像 (PNG) を 1 ページずつに分割する (テキスト層が無い場合)

典型的な流れ:
  manualbook fetch -manifest manifest.json -out pdf/
  manualbook md    pdf/CRM-ver10.11-1.1.pdf -profile profiles/nec-ix-crm.json -series ix -version 10.11-1.1 -out ~/.ix-toolkit/manuals/ix/crm
  manualbook md    pdf/IX-R-CRM-1.5a -out ~/.ix-toolkit/manuals/ix-r/crm
  manualbook diff  ~/.ix-toolkit/manuals/ix ~/.ix-toolkit/manuals/ix-r

各サブコマンドの詳細は -h を付けて実行してください。
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	switch cmd := os.Args[1]; cmd {
	case "fetch":
		runFetch(os.Args[2:])
	case "probe":
		runProbe(os.Args[2:])
	case "md":
		runMD(os.Args[2:])
	case "diff":
		runDiff(os.Args[2:])
	case "figures":
		runFigures(os.Args[2:])
	case "scan":
		runScan(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "不明なサブコマンド: %s\n\n", cmd)
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}
}

// parseFlags は flag パッケージの「最初の非フラグ引数で解析を打ち切る」挙動を
// 回避し、`manualbook md foo.pdf -out out/` のようにフラグを後置しても効くようにする。
// 位置引数を順に取り出しながら、残りを繰り返し解析する。
func parseFlags(fs *flag.FlagSet, args []string) []string {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			os.Exit(2)
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}
