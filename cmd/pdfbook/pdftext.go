package main

import (
	"encoding/json"
	"fmt"
	"os"
	"unicode"
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
// プロファイルの数値がこの「端からの幅」であり、以前 pdftotext に渡していた
// -marginl/r/t/b と同じ意味を持つ。較正済みのプロファイルはそのまま使える。

// Profile は 1 冊の PDF のページ幾何と構造マーカーを表す。
// probe サブコマンドが自動較正して JSON で吐く。
type Profile struct {
	Name string `json:"name"`

	// --- ページ幾何 (ポイント) ---
	PageWidth  float64 `json:"pageWidth"`
	PageHeight float64 `json:"pageHeight"`

	// 本文帯を切り出すための天地マージン。ヘッダ・フッタはここで落ちる。
	MarginTop    float64 `json:"marginTop"`
	MarginBottom float64 `json:"marginBottom"`

	// --- 段組み ---
	// Columns=1 なら GutterLeft/GutterRight は使わない。
	Columns     int     `json:"columns"`
	GutterLeft  float64 `json:"gutterLeft"`  // 左カラム抽出時に右端から削る幅
	GutterRight float64 `json:"gutterRight"` // 右カラム抽出時に左端から削る幅

	// --- ヘッダ・フッタ帯 (メタデータとして別に抜く) ---
	HeaderBand float64 `json:"headerBand"` // ヘッダだけ残すため下端から削る幅。0 なら抽出しない
	FooterBand float64 `json:"footerBand"` // フッタだけ残すため上端から削る幅。0 なら抽出しない

	// --- 構造マーカー ---
	//
	// コマンド辞書 (コマンドリファレンス) は、項目が EntryMarker で始まり、
	// 中身が FieldLabels で割れる、という形をしている。この 2 つが揃っている
	// 資料だけがコマンド項目として読める。
	//
	// 揃っていない資料 (機能説明書のような解説書) は、階層番号の見出しで割る。
	// 同じ記号でも資料ごとに指すものが違うので、記号の設定を流用してはいけない。
	// "■" はコマンドリファレンスでは項目の頭 (2039 個) だが、機能説明書では
	// 節見出しの頭 (353 個) であり、前者の設定で後者を読むと節見出しが
	// そのまま偽のコマンドとして索引に並ぶ。
	EntryMarker string   `json:"entryMarker"` // 項目の先頭記号 (例 "■")
	FieldLabels []string `json:"fieldLabels"` // 項目内の見出し語 (例 入力形式/パラメータ...)

	// ChapterSep は版面ヘッダの「章名/節名」の区切り。機能説明書のヘッダは
	// "ルータの設定・PPP の設定" の形をしている。節名自体が "運用・保守" のように
	// 区切りを含むことがあるので、最初の 1 つだけで割る。
	ChapterSep string `json:"chapterSep,omitempty"`
}

// HasCommandEntries は、この資料をコマンド項目として読めるかを返す。
//
// 読めない資料は節見出しで読む。どちらで読むかを指す設定は別に持たない。
// EntryMarker が無ければ parseEntries は項目を 1 つも開始できず、
// FieldLabels が無ければ項目の中身を割れないので、この 2 つの有無は
// 選択肢ではなく前提条件そのものである。
func (p *Profile) HasCommandEntries() bool {
	return p.EntryMarker != "" && len(p.FieldLabels) > 0
}

// DefaultProfile は NEC IX コマンドリファレンスマニュアル用の既定値。
// 他の資料では probe で較正した値に置き換える。
func DefaultProfile() *Profile {
	return &Profile{
		Name:         "nec-ix-crm",
		PageWidth:    516,
		PageHeight:   728.88,
		MarginTop:    45,
		MarginBottom: 45,
		Columns:      2,
		GutterLeft:   256,
		GutterRight:  262,
		HeaderBand:   690,
		FooterBand:   690,
		EntryMarker:  "■",
		FieldLabels: []string{
			"入力形式", "パラメータ", "説明", "デフォルト値",
			"実行モード", "ユーザ権限", "入力例", "ノート",
		},
	}
}

func LoadProfile(path string) (*Profile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p := DefaultProfile()
	if err := json.Unmarshal(b, p); err != nil {
		return nil, fmt.Errorf("プロファイルの解析に失敗: %w", err)
	}
	return p, nil
}

func (p *Profile) Save(path string) error {
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
func (p *Profile) bodyCrop() crop {
	return crop{top: p.MarginTop, bottom: p.MarginBottom}
}

// ExtractColumns は 1 冊を読み、ページごとの本文を返す。
//
//	left  : 左カラム
//	right : 右カラム
//	full  : ページ全幅 (段抜きの図表・目次・章扉用)
func (p *Profile) ExtractColumns(pdf string) (left, right, full []string, err error) {
	body := p.bodyCrop()

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
func (p *Profile) ExtractBody(pdf string) ([]string, error) {
	return extractPages(pdf, p.bodyCrop(), true)
}

// ExtractBands はヘッダ帯・フッタ帯を別々に抜く。
// このマニュアルではヘッダ = 節名、フッタ = 印刷ページ番号 (例 "3-29") であり、
// 目次を解析しなくてもここから章・節の構造がそのまま取れる。
//
// 帯は桁を揃えない。1 行の文字列として読むだけなので、空きは空白 1 つに潰す。
func (p *Profile) ExtractBands(pdf string) (headers, footers []string, err error) {
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
