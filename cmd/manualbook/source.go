package main

import (
	"fmt"
	"strings"
)

// 資料の出どころ。前段 (PDF / Sphinx) が違っても後段の書き出しは同じにしたい。
// 後段が元資料について知る必要があるのは「本文のこの箇所は元のどこか」を
// 指すことと、README に出どころを書くことだけなので、その 2 つをここに寄せる。

// ref は本文の一箇所を元資料の中で指す。
//
// PDF なら物理ページ番号。版面に刷られた番号 (3-29) は章ごとに振り直されていて
// PDF ビューアにも渡せないので持たない。Web ならページのパスと節の id で、
// そのまま URL の末尾になる。
type ref struct {
	page   int    // PDF の物理ページ (1 始まり)。Web では 0
	path   string // Web: 冊子の起点からのページのパス (cli/remoteaccess/cli_aaa.html)
	anchor string // Web: ページ内の節 id (aaa-enable)。無ければ空
}

// String は索引の source 列に載せる形。"p1057" か "cli/…html#aaa-enable"。
func (r ref) String() string {
	if r.path != "" {
		if r.anchor != "" {
			return r.path + "#" + r.anchor
		}
		return r.path
	}
	if r.page > 0 {
		return fmt.Sprintf("p%d", r.page)
	}
	return ""
}

func (r ref) isWeb() bool { return r.path != "" }

// source は 1 冊の出どころと、README に書く生成条件。
type source struct {
	kind     string // "pdf" か "web"
	pdf      string // kind=pdf: PDF のパス
	baseURL  string // kind=web: 冊子の URL (末尾 /)。出典リンクの起点
	cacheDir string // kind=web: fetch が置いた取得キャッシュ
	label    string // -source で渡された出典表記 (取得元 URL 等)
	series   string // 機種の系列 (ix / ix-r)
	version  string // 資料の版
	fetched  string // 取得日 (web)
	profile  *Profile
}

// urlOf は ref を辿れる URL にする。web でなければ空。
func (s source) urlOf(r ref) string {
	if s.kind != "web" || !r.isWeb() {
		return ""
	}
	return strings.TrimSuffix(s.baseURL, "/") + "/" + r.String()
}

// writeOrigin は README の「生成条件」のうち、出どころに依る行を書く。
func (s source) writeOrigin(b *strings.Builder) {
	switch s.kind {
	case "web":
		fmt.Fprintf(b, "- 元資料: %s\n", s.baseURL)
		if s.fetched != "" {
			fmt.Fprintf(b, "- 取得日: %s\n", s.fetched)
		}
	default:
		fmt.Fprintf(b, "- 元 PDF: `%s`\n", baseName(s.pdf))
		if s.label != "" {
			fmt.Fprintf(b, "- 取得元: %s\n", s.label)
		}
	}
	if s.version != "" {
		fmt.Fprintf(b, "- 版: %s\n", s.version)
	}
	if s.series != "" {
		fmt.Fprintf(b, "- 系列: %s\n", s.series)
	}
	p := s.profile
	switch {
	case s.kind == "web":
		fmt.Fprintf(b, "- プロファイル: `%s`\n", p.Name)
	case p.HasCommandEntries():
		fmt.Fprintf(b, "- プロファイル: `%s` (%d 段組み, 天地マージン %.0f/%.0f pt)\n",
			p.Name, p.Columns, p.MarginTop, p.MarginBottom)
	default:
		fmt.Fprintf(b, "- プロファイル: `%s` (節見出しで割る, 天地マージン %.0f/%.0f pt)\n",
			p.Name, p.MarginTop, p.MarginBottom)
	}
}

// route は README の「変換経路」。
func (s source) route() string {
	if s.kind == "web" {
		return "Sphinx が出した HTML を節と項目の構造どおりに読んだもの (表は Markdown の表、図は SVG のまま)"
	}
	if s.profile.HasCommandEntries() {
		return "PDF のテキスト層を版面どおりに組み直したもの (OCR 不使用)"
	}
	return "PDF のテキスト層を版面どおりに組み直したもの (OCR・画像解析・モデル不使用)"
}

// sourceColumnNote は索引の source 列の読み方。
func (s source) sourceColumnNote() string {
	if s.kind == "web" {
		return fmt.Sprintf("`source` は元のページと節のアンカー (`cli/….html#…`)。`%s` に続ければ URL になる。",
			strings.TrimSuffix(s.baseURL, "/")+"/")
	}
	return "`source` は元 PDF の物理ページ (`p61`)。そのまま PDF ビューアのページ指定に使える。"
}

// --- 見出し語の綴り ---

// 見出し語は元資料の綴りで照合し、出力は 1 つに揃える。
//
// コマンドリファレンスは PDF 版が「ユーザ権限」、Web 版が「ユーザー権限」と
// 綴りが違う。プロファイルの fieldLabels は照合に使うので元資料どおりに書き、
// 本文と索引に出すときにここで揃える。
var labelSpelling = map[string]string{
	"ユーザ権限": "ユーザー権限",
}

func normalizeLabel(l string) string {
	if n, ok := labelSpelling[l]; ok {
		return n
	}
	return l
}

// syntaxLabels と commandsOf は揃えたあとの綴りで見出し語を見る。
// 揃える先がそれらと食い違うと、構文の欄が地の文として整形され、索引が空になる。
// 黙って壊れないよう、起動時に確かめる。
func init() {
	for l := range syntaxLabels {
		if normalizeLabel(l) != l {
			panic(fmt.Sprintf("labelSpelling が syntaxLabels の %q を別の綴りに写している", l))
		}
	}
	for from, to := range labelSpelling {
		if syntaxLabels[from] && !syntaxLabels[to] {
			panic(fmt.Sprintf("labelSpelling: %q → %q で構文の欄の判定が外れる", from, to))
		}
	}
}
