package domain

// Entry は entryMarker で始まる 1 コマンド項目。
type Entry struct {
	Title   string
	Chapter int
	Section string
	Ref     Ref // 項目の元資料上の位置 (PDF の物理ページ / Web のページとアンカー)
	Fields  []Field
	Cmds    []string // 入力形式から抜いたコマンド行 = 索引のキー
	Line    int      // 出力ファイル中の見出し行番号 (1 始まり)。索引はここを指す
}

type Field struct {
	Label string
	Lines []string
}

// SectionKey は出力ファイルの単位。章 + 節で 1 ファイルにする。
type SectionKey struct {
	Chapter int
	Section string
}
