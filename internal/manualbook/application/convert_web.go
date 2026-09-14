package application

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
	"github.com/yuu61/ix-toolkit/internal/manualbook/infrastructure"
)

// convertWeb は md の、取得キャッシュ (Sphinx の HTML) を読む経路。
//
// 入力がディレクトリならこちらに来る。fetch が置いた .manualbook.json があれば
// 題・版・系列・プロファイルの既定値にし、指定があればそちらが勝つ。
func convertWeb(o MDOptions) error {
	dir, outDir := o.Input, o.OutDir
	meta, err := infrastructure.ReadWebMeta(dir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %w", filepath.Join(dir, infrastructure.WebMetaName), err)
	}
	o = webOptions(o, meta)
	p, err := conversionProfile(o.ProfilePath)
	if err != nil {
		return err
	}
	title := o.Title

	if meta.URL == "" {
		fmt.Fprintln(os.Stderr, "⚠ 取得元 URL が分かりません (.manualbook.json が無い)。出典は索引の相対パスとアンカーだけになります。")
	}
	src := infrastructure.Source{
		Kind: domain.KindWeb, BaseURL: meta.URL, CacheDir: dir, Label: o.SourceLabel,
		Series: o.Series, Version: o.Version, Fetched: meta.Fetched, Profile: p,
	}

	fmt.Printf("読み込み: %s (プロファイル %s)\n", dir, p.Name)
	pages, err := infrastructure.ReadWebPages(dir, p.WebUnnumberedHeadings)
	if err != nil {
		return err
	}
	fmt.Printf("  ページ数: %d\n", len(pages))
	if len(pages) == 0 {
		return fmt.Errorf("プロファイルに合う見出しを持つページがありません: %s", dir)
	}

	if p.HasCommandEntries() {
		return convertWebEntries(outDir, title, src, pages)
	}

	return convertWebSections(dir, outDir, title, src, pages)
}

func webOptions(o MDOptions, meta infrastructure.WebMeta) MDOptions {
	if o.ProfilePath == "" {
		o.ProfilePath = meta.Profile
	}
	if o.Title == "" {
		o.Title = meta.Title
	}
	if o.Title == "" {
		o.Title = infrastructure.BaseName(o.Input)
	}
	if o.Series == "" {
		o.Series = meta.Series
	}
	if o.Version == "" {
		o.Version = meta.Version
	}
	return o
}

func convertWebEntries(outDir, title string, src infrastructure.Source, pages []infrastructure.WebPage) error {
	entries, chapters := infrastructure.ParseWebEntries(pages, src.Profile)
	fmt.Printf("  抽出項目: %d 件 / 章: %d\n", len(entries), len(chapters))
	if len(entries) == 0 {
		return errors.New("項目を 1 件も抽出できませんでした。" +
			"プロファイルの fieldLabels が資料の <dt> の見出し語に合っているか確認してください")
	}
	if err := infrastructure.WriteAll(outDir, title, src, chapters, entries); err != nil {
		return err
	}
	fmt.Printf("\n出力しました: %s\n", outDir)
	fmt.Printf("  機械可読索引: %s\n", filepath.Join(outDir, "commands.tsv"))
	fmt.Printf("  目次:         %s\n", filepath.Join(outDir, "index.md"))
	return nil
}

func convertWebSections(dir, outDir, title string, src infrastructure.Source, pages []infrastructure.WebPage) error {
	heads, chapters := infrastructure.ParseWebHeadings(pages)
	nTable, nFigure, nLayout := 0, 0, 0
	for i := range heads {
		for j := range heads[i].Blocks {
			b := &heads[i].Blocks[j]
			switch b.Kind {
			case domain.BlockTable:
				nTable++
			case domain.BlockFigure:
				nFigure++
			case domain.BlockLayout:
				nLayout++
			}
		}
	}
	fmt.Printf("  見出し: %d 件 / 章: %d / 表: %d / 図: %d / 版面ブロック: %d\n",
		len(heads), len(chapters), nTable, nFigure, nLayout)
	if len(heads) == 0 {
		return errors.New("見出しを 1 件も抽出できませんでした")
	}
	if err := infrastructure.WriteWebFigures(dir, outDir, heads); err != nil {
		return err
	}
	if err := infrastructure.WriteSections(outDir, title, src, heads, chapters); err != nil {
		return err
	}
	fmt.Printf("\n出力しました: %s\n", outDir)
	fmt.Printf("  機械可読索引: %s\n", filepath.Join(outDir, "sections.tsv"))
	fmt.Printf("  目次:         %s\n", filepath.Join(outDir, "index.md"))
	return nil
}
