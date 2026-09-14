package domain

// Entry は entryMarker で始まる 1 コマンド項目。
type Entry struct {
	Ref     Ref
	Title   string
	Section string
	Fields  []Field
	Cmds    []string
	Chapter int
	Line    int
}

type Field struct {
	Label string
	Lines []string
}

// SectionKey は出力ファイルの単位。章 + 節で 1 ファイルにする。
type SectionKey struct {
	Section string
	Chapter int
}
