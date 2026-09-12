package application

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
	"github.com/yuu61/ix-toolkit/internal/manualbook/infrastructure"
)

// MDOptions は 1 冊分の変換の指定。md サブコマンドはフラグから、build は
// マニフェストから組み立てる。
type MDOptions struct {
	Input       string // PDF か、fetch が置いた取得キャッシュのディレクトリ
	OutDir      string
	ProfilePath string
	Title       string
	SourceLabel string
	Series      string
	Version     string
	Figures     bool
	FigureDPI   int
	FigurePages string
}

// Convert は 1 冊を Markdown にする。
//
// 入力が PDF なら版面を解析する前段、fetch が置いた取得キャッシュの
// ディレクトリなら Sphinx の HTML を読む前段 (html.go)。後段は共通。
func Convert(o MDOptions) error {
	if st, err := os.Stat(o.Input); err == nil && st.IsDir() {
		if o.Figures {
			return fmt.Errorf("-figures は PDF 専用です。Web から読む資料は図をそのまま figures/ に置きます")
		}
		return convertWeb(o)
	}
	defer infrastructure.CloseDoc(o.Input)
	return convertPDF(o)
}

// convertPDF は呼び出し元が寿命を管理する PDF を変換する。
func convertPDF(o MDOptions) error {
	pdf := o.Input

	p := domain.DefaultProfile()
	if o.ProfilePath != "" {
		var err error
		if p, err = infrastructure.LoadProfile(o.ProfilePath); err != nil {
			return err
		}
	}

	docTitle := o.Title
	if docTitle == "" {
		docTitle = strings.TrimSuffix(infrastructure.BaseName(pdf), filepath.Ext(pdf))
	}

	// 画像は変換より先に焼く。囲みに [ページ画像] を付けるかどうかは
	// 出力時に figures/ を見て決めるため、後から焼いてもリンクは付かない。
	//
	// 囲みとページリンクを出すのは節見出し経路 (sections.go) だけなので、
	// コマンド辞書として読む資料では焼いても誰も参照しない。黙って焼くと
	// 時間とディスクだけ使うため断る。
	if o.Figures {
		if p.HasCommandEntries() {
			return fmt.Errorf("-figures はこの資料 (プロファイル %s) では効きません。"+
				"囲みにページ画像を添えるのは節見出しで割る資料だけです", p.Name)
		}
		n, err := infrastructure.RenderFigures(pdf, o.OutDir, o.FigureDPI, o.FigurePages)
		if err != nil {
			return err
		}
		fmt.Printf("ページ画像: %d 枚を焼きました\n", n)
	}

	// 項目の記号と見出し語を持たない資料はコマンド辞書として読めないので、
	// 節見出しで割る経路へ回す (sections.go)。
	src := infrastructure.Source{Kind: "pdf", PDF: pdf, Label: o.SourceLabel, Series: o.Series, Version: o.Version, Profile: p}
	if !p.HasCommandEntries() {
		return infrastructure.ConvertSections(o.OutDir, docTitle, src)
	}

	fmt.Printf("読み込み: %s (プロファイル %s)\n", pdf, p.Name)
	pages, stats, err := infrastructure.ReadPages(p, pdf)
	if err != nil {
		return err
	}
	fmt.Printf("  ページ数: %d (2段組み %d / 全幅 %d)\n", len(pages), stats.Two, stats.One)

	chapters := infrastructure.CollectChapterTitles(pages)
	entries := infrastructure.ParseEntries(p, pages)
	fmt.Printf("  抽出項目: %d 件 / 章: %d\n", len(entries), len(chapters))

	if len(entries) == 0 {
		return fmt.Errorf("項目を 1 件も抽出できませんでした。"+
			"プロファイルの entryMarker (%q) と fieldLabels が資料に合っているか確認してください", p.EntryMarker)
	}

	if err := infrastructure.WriteAll(o.OutDir, docTitle, src, chapters, entries); err != nil {
		return err
	}
	fmt.Printf("\n出力しました: %s\n", o.OutDir)
	fmt.Printf("  機械可読索引: %s\n", filepath.Join(o.OutDir, "commands.tsv"))
	fmt.Printf("  目次:         %s\n", filepath.Join(o.OutDir, "index.md"))
	return nil
}
