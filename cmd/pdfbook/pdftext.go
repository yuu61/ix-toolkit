package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode"
)

// pdftotext (Xpdf 4.x / poppler) を唯一のテキスト抽出エンジンとして使う。
//
// 段組み・ヘッダ・フッタは座標ではなく「マージンで削り落とす」ことで扱う。
// pdftotext の -marginl/-marginr/-margint/-marginb は、指定した端から
// N ポイント以内にあるテキストを捨てる。これを使うと、
//
//	左カラムだけ  : -marginr <ガター左端>
//	右カラムだけ  : -marginl <ガター右端>
//	ヘッダだけ    : -marginb <ヘッダのすぐ下>
//	フッタだけ    : -margint <フッタのすぐ上>
//
// が 1 コマンドで得られる。OCR も座標計算も要らない。

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
	GutterLeft  float64 `json:"gutterLeft"`  // 左カラム抽出時の -marginr
	GutterRight float64 `json:"gutterRight"` // 右カラム抽出時の -marginl

	// --- ヘッダ・フッタ帯 (メタデータとして別に抜く) ---
	HeaderBand float64 `json:"headerBand"` // ヘッダだけ残す -marginb。0 なら抽出しない
	FooterBand float64 `json:"footerBand"` // フッタだけ残す -margint。0 なら抽出しない

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

// --- pdftotext 実行 ---

// pdftotextAvailable は pdftotext が PATH にあるか調べる。
func pdftotextAvailable() error {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return fmt.Errorf("pdftotext が見つかりません。poppler-utils または Xpdf をインストールしてください: %w", err)
	}
	return nil
}

// extract は pdftotext を 1 回だけ起動し、ページごとに分割した文字列を返す。
// pdftotext はページ境界に改ページ (\f) を出すので、それで分ける。
// 852 ページでも 1 回の起動で 2 秒足らずで終わるため、ページ単位で呼んではいけない。
func extract(pdf string, extraArgs ...string) ([]string, error) {
	args := append([]string{"-enc", "UTF-8", "-eol", "unix"}, extraArgs...)
	args = append(args, pdf, "-")

	cmd := exec.Command("pdftotext", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pdftotext 失敗 (%w): %s", err, strings.TrimSpace(stderr.String()))
	}

	pages := strings.Split(string(out), "\f")
	// 末尾の改ページで余分な空要素が 1 つ出る
	if n := len(pages); n > 0 && strings.TrimSpace(pages[n-1]) == "" {
		pages = pages[:n-1]
	}
	for i := range pages {
		pages[i] = strings.ReplaceAll(pages[i], "\r", "")
	}
	return pages, nil
}

// bodyArgs は本文帯 (ヘッダ・フッタを除いた領域) を切り出す共通マージン。
func (p *Profile) bodyArgs() []string {
	return []string{
		"-margint", ftoa(p.MarginTop),
		"-marginb", ftoa(p.MarginBottom),
	}
}

// ExtractColumns は 1 冊を高々 3 回のパスで読み、ページごとの本文を返す。
//
//	left  : 左カラム
//	right : 右カラム
//	full  : ページ全幅 (段抜きの図表・目次・章扉用)
func (p *Profile) ExtractColumns(pdf string) (left, right, full []string, err error) {
	body := p.bodyArgs()

	full, err = extract(pdf, append([]string{"-layout"}, body...)...)
	if err != nil {
		return nil, nil, nil, err
	}
	if p.Columns < 2 {
		return nil, nil, full, nil
	}

	left, err = extract(pdf, append([]string{"-layout", "-marginr", ftoa(p.GutterLeft)}, body...)...)
	if err != nil {
		return nil, nil, nil, err
	}
	right, err = extract(pdf, append([]string{"-layout", "-marginl", ftoa(p.GutterRight)}, body...)...)
	if err != nil {
		return nil, nil, nil, err
	}
	return left, right, full, nil
}

// ExtractBody は本文を 1 パスで読む。段組みを持たない資料の経路。
//
// -layout ではなく -table を使う。この違いは体裁ではなく正しさの問題である。
// -layout は本文の座標をそのまま空白に写すため、罫線で組まれた表の行がずれる。
// 実測 (機能説明書 1-7 の諸元表) では、-layout だと
//
//	VLAN 設定数    (空)  8  -  1000※1  32  同左
//
// となり、正しい 32/32/32/32/32/1000※1/32/32 と機種の対応が全部ずれた。
// 抽出は成功しているように見えるので、この崩れは出力を見ても分からない。
// -table は語の x 座標を列に束ね直すので、同じ表が正しく出る。
func (p *Profile) ExtractBody(pdf string) ([]string, error) {
	return extract(pdf, append([]string{"-table"}, p.bodyArgs()...)...)
}

// ExtractBands はヘッダ帯・フッタ帯を別々に抜く。
// このマニュアルではヘッダ = 節名、フッタ = 印刷ページ番号 (例 "3-29") であり、
// 目次を解析しなくてもここから章・節の構造がそのまま取れる。
func (p *Profile) ExtractBands(pdf string) (headers, footers []string, err error) {
	if p.HeaderBand > 0 {
		headers, err = extract(pdf, "-raw", "-marginb", ftoa(p.HeaderBand))
		if err != nil {
			return nil, nil, err
		}
		for i := range headers {
			headers[i] = strings.TrimSpace(headers[i])
		}
	}
	if p.FooterBand > 0 {
		footers, err = extract(pdf, "-raw", "-margint", ftoa(p.FooterBand))
		if err != nil {
			return nil, nil, err
		}
		for i := range footers {
			footers[i] = strings.TrimSpace(footers[i])
		}
	}
	return headers, footers, nil
}

// --- 補助 ---

func ftoa(f float64) string {
	return fmt.Sprintf("%g", f)
}

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
	pages, err := extract(pdf, "-raw", "-f", "1", "-l", fmt.Sprint(nPages))
	if err != nil {
		return 0, err
	}
	total := 0
	for _, pg := range pages {
		total += countGlyphs(pg)
	}
	return total, nil
}
