package domain

// Profile は 1 冊の PDF のページ幾何と構造マーカーを表す。
// PDF の版面条件と、PDF / Web 共通の構造マーカーを持つ。
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
// FieldLabels が無ければ項目の中身を割れないので、その有無は選択肢ではなく
// 前提条件そのものである。EntryMarker は見ない。PDF では項目の頭を記号で
// 見つけるが、Web (Sphinx) では項目が <section> と <dl> の構造で分かれていて
// 記号が無い。記号は PDF 前段の解析の都合であって、資料の型を決めるものではない。
func (p *Profile) HasCommandEntries() bool {
	return len(p.FieldLabels) > 0
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
