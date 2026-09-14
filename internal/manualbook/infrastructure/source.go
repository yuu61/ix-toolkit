package infrastructure

import (
	"fmt"
	"strings"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

// Source は 1 冊の出どころと、README に書く生成条件。
type Source struct {
	Profile  *domain.Profile
	Kind     string
	PDF      string
	BaseURL  string
	CacheDir string
	Label    string
	Series   string
	Version  string
	Fetched  string
}

// writeOrigin は README の「生成条件」のうち、出どころに依る行を書く。
func (s Source) writeOrigin(b *strings.Builder) {
	switch s.Kind {
	case domain.KindWeb:
		fmt.Fprintf(b, "- 元資料: %s\n", s.BaseURL)
		if s.Fetched != "" {
			fmt.Fprintf(b, "- 取得日: %s\n", s.Fetched)
		}
	default:
		fmt.Fprintf(b, "- 元 PDF: `%s`\n", BaseName(s.PDF))
		if s.Label != "" {
			fmt.Fprintf(b, "- 取得元: %s\n", s.Label)
		}
	}
	if s.Version != "" {
		fmt.Fprintf(b, "- 版: %s\n", s.Version)
	}
	if s.Series != "" {
		fmt.Fprintf(b, "- 系列: %s\n", s.Series)
	}
	p := s.Profile
	switch {
	case s.Kind == domain.KindWeb:
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
func (s Source) route() string {
	if s.Kind == domain.KindWeb {
		return "Sphinx が出した HTML を節と項目の構造どおりに読んだもの (表は Markdown の表、図は SVG のまま)"
	}
	if s.Profile.HasCommandEntries() {
		return "PDF のテキスト層を版面どおりに組み直したもの (OCR 不使用)"
	}
	return "PDF のテキスト層とページ画像の罫線から本文・表を組み直したもの (OCR・モデル不使用)"
}

// sourceColumnNote は索引の source 列の読み方。
func (s Source) sourceColumnNote() string {
	if s.Kind == domain.KindWeb {
		return fmt.Sprintf("`source` は元のページと節のアンカー (`cli/….html#…`)。`%s` に続ければ URL になる。",
			strings.TrimSuffix(s.BaseURL, "/")+"/")
	}
	return "`source` は元 PDF の物理ページ (`p61`)。そのまま PDF ビューアのページ指定に使える。"
}
