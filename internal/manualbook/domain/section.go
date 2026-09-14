package domain

// Heading は 1 見出しと、その配下の本文。
type Heading struct {
	Ref     Ref
	Number  string
	Title   string
	Section string
	Blocks  []Block
	Depth   int
	Chapter int
	Line    int
}

// BlockKind は本文の一区切りの種類。
//
// PDF は罫線から復元した表を table、残りの図表を layout とする。
// Web は <table> と <figure> の構造から table と figure を作る。
type BlockKind int

const (
	BlockProse  BlockKind = iota // 地の文
	BlockLayout                  // 版面固定 (表・図・コンソール出力を ```text で囲う)
	BlockTable                   // 表 (Markdown の表に出す)
	BlockFigure                  // 図 (figures/ に置いたファイルへのリンクとラベル列)
)

// Block は本文の一区切り。
type Block struct {
	Ref          Ref
	HeaderRef    Ref
	FigureSource string
	Figure       string
	Lines        []string
	Rows         [][]string
	Kind         BlockKind
}
