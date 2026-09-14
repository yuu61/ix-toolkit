package domain

// Profile は 1 冊の PDF のページ幾何と構造マーカーを表す。
// PDF の版面条件と、PDF / Web 共通の構造マーカーを持つ。
type Profile struct {
	Name                  string   `json:"name"`
	ChapterSep            string   `json:"chapterSep,omitempty"`
	EntryMarker           string   `json:"entryMarker"`
	FieldLabels           []string `json:"fieldLabels"`
	HeaderBand            float64  `json:"headerBand"`
	Columns               int      `json:"columns"`
	GutterLeft            float64  `json:"gutterLeft"`
	GutterRight           float64  `json:"gutterRight"`
	MarginBottom          float64  `json:"marginBottom"`
	FooterBand            float64  `json:"footerBand"`
	MarginTop             float64  `json:"marginTop"`
	PageHeight            float64  `json:"pageHeight"`
	PageWidth             float64  `json:"pageWidth"`
	HeadingMinSize        float64  `json:"headingMinSize,omitempty"`
	WebUnnumberedHeadings bool     `json:"webUnnumberedHeadings,omitempty"`
	FooterSection         bool     `json:"footerSection,omitempty"`
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
			SyntaxLabel, "パラメータ", "説明", "デフォルト値",
			"実行モード", "ユーザ権限", "入力例", "ノート",
		},
	}
}
