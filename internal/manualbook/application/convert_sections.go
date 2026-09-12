package application

import (
	"fmt"
	"path/filepath"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
	"github.com/yuu61/ix-toolkit/internal/manualbook/infrastructure"
)

// convertSections は md の、PDF を節見出しで割る経路。
func convertSections(outDir, docTitle string, src infrastructure.Source) error {
	p, pdf := src.Profile, src.PDF
	fmt.Printf("読み込み: %s (プロファイル %s / 節見出しで割る)\n", pdf, p.Name)
	pages, err := infrastructure.ReadSectionPages(p, pdf, func(completed, total int) {
		if completed%100 == 0 {
			fmt.Printf("  表の解析: %d / %d ページ\n", completed, total)
		}
	})
	if err != nil {
		return err
	}
	fmt.Printf("  ページ数: %d\n", len(pages))

	heads, chapters := infrastructure.ParseHeadings(p, pages)
	nLayout, nTables := 0, 0
	for i := range heads {
		for _, b := range heads[i].Blocks {
			if b.Kind == domain.BlockTable {
				nTables++
			}
			if b.Kind != domain.BlockProse {
				nLayout++
			}
		}
	}
	fmt.Printf("  見出し: %d 件 / 章: %d / 版面ブロック: %d\n", len(heads), len(chapters), nLayout)
	fmt.Printf("  Markdown の表: %d 件\n", nTables)

	if len(heads) == 0 {
		return fmt.Errorf("見出しを 1 件も抽出できませんでした。" +
			"この資料の見出しが階層番号 (2.11.6 の形) で始まっているか確認してください")
	}

	if err := infrastructure.WriteSections(outDir, docTitle, src, heads, chapters); err != nil {
		return err
	}
	fmt.Printf("\n出力しました: %s\n", outDir)
	fmt.Printf("  機械可読索引: %s\n", filepath.Join(outDir, "sections.tsv"))
	fmt.Printf("  目次:         %s\n", filepath.Join(outDir, "index.md"))
	return nil
}
