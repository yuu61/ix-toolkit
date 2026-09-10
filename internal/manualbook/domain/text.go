package domain

import (
	"regexp"
	"strings"
)

var spaceRun = regexp.MustCompile(`[ \t\r\n\x{3000}]+`)

// Collapse は空白の並びを 1 つに畳む。日本語の語間に空白は無いので、
// 改行で割れていた語を空白で繋いでも意味は変わらない。
func Collapse(s string) string {
	return strings.TrimSpace(spaceRun.ReplaceAllString(s, " "))
}
