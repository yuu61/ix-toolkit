package domain

// Manifest は取得・変換する資料の一覧。
type Manifest struct {
	Docs []Doc `json:"docs"`
}

type Doc struct {
	Name          string `json:"name,omitempty"`          // 出力ファイル名 / 取得キャッシュのディレクトリ名
	Series        string `json:"series,omitempty"`        // 機種の系列 (ix / ix-r)
	Book          string `json:"book,omitempty"`          // 冊子 (crm / fd / ex / slog)。変換結果の置き場 <系列>/<冊子>/ を決める
	Kind          string `json:"kind,omitempty"`          // "pdf" か "web"。省略時は系列から導出する
	URL           string `json:"url,omitempty"`           // 取得元 (web は冊子の index の URL)
	Version       string `json:"version,omitempty"`       // 版。web では <title> と突き合わせる
	VersionSource string `json:"versionSource,omitempty"` // web の版の場所。空なら title、edition なら本文の「版数」直下
	Profile       string `json:"profile,omitempty"`       // 変換に使うプロファイル JSON
	Title         string `json:"title,omitempty"`         // 資料タイトル
}
