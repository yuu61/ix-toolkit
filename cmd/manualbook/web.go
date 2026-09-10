package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// convertWeb は md の、取得キャッシュ (Sphinx の HTML) を読む経路。
//
// 入力がディレクトリならこちらに来る。fetch が置いた .manualbook.json があれば
// 題・版・系列・プロファイルの既定値にし、指定があればそちらが勝つ。
func convertWeb(o mdOptions) error {
	dir, outDir := o.input, o.outDir
	meta, err := readWebMeta(dir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("%s: %w", filepath.Join(dir, webMetaName), err)
	}
	profilePath := o.profilePath
	if profilePath == "" {
		profilePath = meta.Profile
	}
	p := DefaultProfile()
	if profilePath != "" {
		if p, err = LoadProfile(profilePath); err != nil {
			return err
		}
	}
	title := o.title
	if title == "" {
		title = meta.Title
	}
	if title == "" {
		title = baseName(dir)
	}
	series := o.series
	if series == "" {
		series = meta.Series
	}
	version := o.version
	if version == "" {
		version = meta.Version
	}
	if meta.URL == "" {
		fmt.Fprintln(os.Stderr, "⚠ 取得元 URL が分かりません (.manualbook.json が無い)。出典リンクは相対パスだけになります。")
	}
	src := source{
		kind: "web", baseURL: meta.URL, cacheDir: dir, label: o.sourceLabel,
		series: series, version: version, fetched: meta.Fetched, profile: p,
	}

	fmt.Printf("読み込み: %s (プロファイル %s)\n", dir, p.Name)
	pages, err := readWebPages(dir)
	if err != nil {
		return err
	}
	fmt.Printf("  ページ数: %d\n", len(pages))
	if len(pages) == 0 {
		return fmt.Errorf("番号付きの見出しを持つページがありません: %s", dir)
	}

	if p.HasCommandEntries() {
		entries, chapters := parseWebEntries(pages, p)
		fmt.Printf("  抽出項目: %d 件 / 章: %d\n", len(entries), len(chapters))
		if len(entries) == 0 {
			return fmt.Errorf("項目を 1 件も抽出できませんでした。" +
				"プロファイルの fieldLabels が資料の <dt> の見出し語に合っているか確認してください")
		}
		if err := writeAll(outDir, title, src, chapters, entries); err != nil {
			return err
		}
		fmt.Printf("\n出力しました: %s\n", outDir)
		fmt.Printf("  機械可読索引: %s\n", filepath.Join(outDir, "commands.tsv"))
		fmt.Printf("  目次:         %s\n", filepath.Join(outDir, "index.md"))
		return nil
	}

	figs := newFigureStore(dir, outDir)
	heads, chapters := parseWebHeadings(pages, figs)
	nTable, nFigure, nLayout := 0, 0, 0
	for i := range heads {
		for _, b := range heads[i].blocks {
			switch b.kind {
			case blockTable:
				nTable++
			case blockFigure:
				nFigure++
			case blockLayout:
				nLayout++
			}
		}
	}
	fmt.Printf("  見出し: %d 件 / 章: %d / 表: %d / 図: %d / 版面ブロック: %d\n",
		len(heads), len(chapters), nTable, nFigure, nLayout)
	if len(heads) == 0 {
		return fmt.Errorf("見出しを 1 件も抽出できませんでした")
	}
	if err := writeSections(outDir, title, src, heads, chapters); err != nil {
		return err
	}
	fmt.Printf("\n出力しました: %s\n", outDir)
	fmt.Printf("  機械可読索引: %s\n", filepath.Join(outDir, "sections.tsv"))
	fmt.Printf("  目次:         %s\n", filepath.Join(outDir, "index.md"))
	return nil
}
