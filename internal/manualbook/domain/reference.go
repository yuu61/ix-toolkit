package domain

import (
	"fmt"
)

// 資料の出どころ。前段 (PDF / Sphinx) が違っても後段の書き出しは同じにしたい。
// 後段が元資料について知る必要があるのは「本文のこの箇所は元のどこか」を
// 指すことと、README に出どころを書くことだけなので、その 2 つをここに寄せる。

// Ref は本文の一箇所を元資料の中で指す。
//
// PDF なら物理ページ番号。版面に刷られた番号 (3-29) は章ごとに振り直されていて
// PDF ビューアにも渡せないので持たない。Web ならページのパスと節の id で、
// そのまま URL の末尾になる。
type Ref struct {
	Page   int    // PDF の物理ページ (1 始まり)。Web では 0
	Path   string // Web: 冊子の起点からのページのパス (cli/remoteaccess/cli_aaa.html)
	Anchor string // Web: ページ内の節 id (aaa-enable)。無ければ空
}

// String は索引の source 列に載せる形。"p1057" か "cli/…html#aaa-enable"。
func (r Ref) String() string {
	if r.Path != "" {
		if r.Anchor != "" {
			return r.Path + "#" + r.Anchor
		}
		return r.Path
	}
	if r.Page > 0 {
		return fmt.Sprintf("p%d", r.Page)
	}
	return ""
}

func (r Ref) IsWeb() bool { return r.Path != "" }

// --- 見出し語の綴り ---

// 見出し語は元資料の綴りで照合し、出力は 1 つに揃える。
//
// コマンドリファレンスは PDF 版が「ユーザ権限」、Web 版が「ユーザー権限」と
// 綴りが違う。プロファイルの fieldLabels は照合に使うので元資料どおりに書き、
// 本文と索引に出すときにここで揃える。
var LabelSpelling = map[string]string{
	"ユーザ権限": "ユーザー権限",
}

func NormalizeLabel(l string) string {
	if n, ok := LabelSpelling[l]; ok {
		return n
	}
	return l
}
