package domain

import (
	"fmt"
	"regexp"
)

// 版の英数字と区切りを一続きとして読む。「1.5a版」の「版」は含めない。
var versionTokenRe = regexp.MustCompile(`[A-Za-z0-9]+(?:[._+-][A-Za-z0-9]+)*`)

// MatchesWebVersion はタイトルに指定した版が独立した値としてあるかを調べる。
// 1.5a に対する 1.5ab や 11.5a のような部分一致は認めない。
func MatchesWebVersion(title, version string) bool {
	for _, token := range versionTokenRe.FindAllString(title, -1) {
		if token == version {
			return true
		}
	}
	return false
}

// CheckDoc は置き場 <系列>/<冊子>/ を決めるのに要る欄が揃っているかを見る。
// 冊子名は diff と ix-manual skill が crm / fd で読むので、それ以外は通さない。
func CheckDoc(d Doc) error {
	switch d.Book {
	case "crm", "fd":
	case "":
		return fmt.Errorf(`book が無い。"crm" (コマンドリファレンス) か "fd" (機能説明書) を書く`)
	default:
		return fmt.Errorf(`book %q は知らない ("crm" か "fd")`, d.Book)
	}
	if d.Series == "" {
		return fmt.Errorf(`series が無い。"ix" (IX2000/IX3000) か "ix-r" (IX-R/IX-V) を書く`)
	}
	return nil
}
