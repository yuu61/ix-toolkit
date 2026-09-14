package domain

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
)

// 版の英数字と区切りを一続きとして読む。「1.5a版」の「版」は含めない。
var versionTokenRe = regexp.MustCompile(`[A-Za-z0-9]+(?:[._+-][A-Za-z0-9]+)*`)

// MatchesWebVersion はタイトルに指定した版が独立した値としてあるかを調べる。
// 1.5a に対する 1.5ab や 11.5a のような部分一致は認めない。
func MatchesWebVersion(title, version string) bool {
	return slices.Contains(versionTokenRe.FindAllString(title, -1), version)
}

// CheckDoc は置き場 <系列>/<冊子>/ を決めるのに要る欄が揃っているかを見る。
// 冊子名は ix-manual skill が読む crm / fd / ex / slog に限る。
func CheckDoc(d Doc) error {
	switch d.Book {
	case "crm", "fd", "ex", "slog":
	case "":
		return errors.New(`book が無い。"crm" (コマンドリファレンス)、"fd" (機能説明書)、"ex" (設定事例集)、"slog" (syslog リファレンス) を書く`)
	default:
		return fmt.Errorf(`book %q は知らない ("crm" / "fd" / "ex" / "slog")`, d.Book)
	}
	if d.Series == "" {
		return errors.New(`series が無い。"ix" (IX2000/IX3000) か "ix-r" (IX-R/IX-V) を書く`)
	}
	return nil
}
