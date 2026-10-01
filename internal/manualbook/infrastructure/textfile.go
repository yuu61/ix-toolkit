package infrastructure

import (
	"bytes"
	"os"
)

// 手で直すファイル (manifest.json・プロファイル・手で導いた差分 TSV) は、先頭の
// UTF-8 BOM を読み飛ばす。Windows PowerShell 5.1 の Set-Content -Encoding UTF8 や
// Out-File、古いメモ帳は保存時に BOM を付ける。そのままだと JSON は 1 文字目で
// 解析に失敗し、TSV は先頭のコメント行がコメントに見えず見出し行の検査で止まる。
// 生成物 (commands.tsv など) は自分で BOM なしに書くので対象にしない。

const utf8BOM = "\xEF\xBB\xBF"

// readHandEdited は手で直すファイルを読み、先頭の BOM を除いて返す。
func readHandEdited(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return bytes.TrimPrefix(b, []byte(utf8BOM)), nil
}
