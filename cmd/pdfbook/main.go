// pdfbook は PDF のマニュアルを Markdown に変換するためのツール。
//
// テキスト層を持つ PDF なら OCR は要らない。段組みとヘッダ・フッタさえ
// 処理できれば本文はそのまま取り出せる、というのがこのツールの前提である。
// 紙のスキャン由来でテキスト層が無い場合だけ、scan サブコマンドで
// 見開き画像を分割してから OCR に回す。
package main

import (
	"flag"
	"fmt"
	"os"
)

const usage = `pdfbook — PDF マニュアルを Markdown にする

使い方:
  pdfbook <サブコマンド> [オプション]

サブコマンド:
  fetch   マニフェストに書いた PDF をまとめて取得する (SHA256 検証つき)
  probe   PDF を試し読みして段組み・ヘッダ位置を自動較正し、プロファイルを作る
  md      テキスト層のある PDF を構造つき Markdown に変換する
  figures ページを PNG に焼く (図のページを画像で引けるようにする)
  scan    見開きスキャン画像 (PNG) を 1 ページずつに分割する (テキスト層が無い場合)

典型的な流れ:
  pdfbook fetch -manifest manifest.json -out pdf/
  pdfbook probe pdf/CRM.pdf -out profiles/crm.json
  pdfbook md    pdf/CRM.pdf -profile profiles/crm.json -out out/crm/

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
// 回避し、`pdfbook md foo.pdf -out out/` のようにフラグを後置しても効くようにする。
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
