package infrastructure

import (
	"encoding/json"
	"fmt"
	"os"
	"unicode"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
)

// PDF の読み方。テキストは pdfium.go のエンジンから文字と外接矩形で受け取り、
// 版面の組み直しは layout.go が行う。外部コマンドは使わない。
//
// 段組み・ヘッダ・フッタは座標ではなく「端から N ポイントを削り落とす」形で
// 扱う。切り出したい領域はどれもこの形で書ける。
//
//	左カラムだけ  : 右端から <ガター右まで> を削る
//	右カラムだけ  : 左端から <ガター左まで> を削る
//	ヘッダだけ    : 下端から <ヘッダのすぐ下まで> を削る
//	フッタだけ    : 上端から <フッタのすぐ上まで> を削る
//
// プロファイル (domain.Profile) の数値がこの「端からの幅」であり、以前 pdftotext に
// 渡していた -marginl/r/t/b と同じ意味を持つ。較正済みのプロファイルはそのまま使える。
// プロファイルは probe サブコマンドが自動較正して JSON で吐く。

// LoadProfile はプロファイル JSON を読む。書いていない欄は既定値のまま。
func LoadProfile(path string) (*domain.Profile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p := domain.DefaultProfile()
	if err := json.Unmarshal(b, p); err != nil {
		return nil, fmt.Errorf("プロファイルの解析に失敗: %w", err)
	}
	return p, nil
}

// SaveProfile はプロファイルを JSON で書く。probe の出力。
func SaveProfile(p *domain.Profile, path string) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// --- 抽出 ---

// extractPages は 1 冊を読み、ページごとのテキストを返す。
// PDF は開いたまま使い回されるので、経路ごとに呼び直してよい。
func extractPages(pdf string, c crop, grid bool) ([]string, error) {
	d, err := openDoc(pdf)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(d.pages))
	for i, pg := range d.pages {
		out[i] = renderPage(pg, c, grid)
	}
	return out, nil
}

// bodyCrop は本文帯 (ヘッダ・フッタを除いた領域) の切り出し。
func bodyCrop(p *domain.Profile) crop {
	return crop{top: p.MarginTop, bottom: p.MarginBottom}
}

// ExtractColumns は 1 冊を読み、ページごとの本文を返す。
//
//	left  : 左カラム
//	right : 右カラム
//	full  : ページ全幅 (段抜きの図表・目次・章扉用)
func extractColumns(p *domain.Profile, pdf string) (left, right, full []string, err error) {
	body := bodyCrop(p)

	full, err = extractPages(pdf, body, true)
	if err != nil {
		return nil, nil, nil, err
	}
	if p.Columns < 2 {
		return nil, nil, full, nil
	}

	lc, rc := body, body
	lc.right = p.GutterLeft
	rc.left = p.GutterRight

	left, err = extractPages(pdf, lc, true)
	if err != nil {
		return nil, nil, nil, err
	}
	right, err = extractPages(pdf, rc, true)
	if err != nil {
		return nil, nil, nil, err
	}
	return left, right, full, nil
}

// ExtractBody は本文を読む。段組みを持たない資料の経路。
func extractBody(p *domain.Profile, pdf string) ([]string, error) {
	return extractPages(pdf, bodyCrop(p), true)
}

// ExtractBands はヘッダ帯・フッタ帯を別々に抜く。
// このマニュアルではヘッダ = 節名、フッタ = 印刷ページ番号 (例 "3-29") であり、
// 目次を解析しなくてもここから章・節の構造がそのまま取れる。
//
// 帯は桁を揃えない。1 行の文字列として読むだけなので、空きは空白 1 つに潰す。
func extractBands(p *domain.Profile, pdf string) (headers, footers []string, err error) {
	if p.HeaderBand > 0 {
		headers, err = extractPages(pdf, crop{bottom: p.HeaderBand}, false)
		if err != nil {
			return nil, nil, err
		}
	}
	if p.FooterBand > 0 {
		footers, err = extractPages(pdf, crop{top: p.FooterBand}, false)
		if err != nil {
			return nil, nil, err
		}
	}
	return headers, footers, nil
}

// pageSize は PDF の 1 ページ目の寸法を返す。probe が版面の較正に使う。
func pageSize(pdf string) (width, height float64, err error) {
	d, err := openDoc(pdf)
	if err != nil {
		return 0, 0, err
	}
	if len(d.pages) == 0 {
		return 0, 0, fmt.Errorf("ページがありません: %s", pdf)
	}
	return d.pages[0].width, d.pages[0].height, nil
}

// --- 補助 ---

// countGlyphs は空白を除いた文字数を数える。
// 段組み分割が「取りこぼしも二重取りも無い」ことを検証する不変条件に使う。
func countGlyphs(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

// hasTextLayer は先頭 nPages ページに実テキストがあるか判定する。
// 紙スキャン由来の画像 PDF ならここが 0 に近くなり、md ではなく
// scan + OCR の経路が必要だと分かる。
func hasTextLayer(pdf string, nPages int) (int, error) {
	d, err := openDoc(pdf)
	if err != nil {
		return 0, err
	}
	total := 0
	for i, pg := range d.pages {
		if i >= nPages {
			break
		}
		total += len(pg.glyphs)
	}
	return total, nil
}
