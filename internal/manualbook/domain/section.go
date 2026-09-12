package domain

// Heading は階層番号を持つ 1 見出しと、その配下の本文。
type Heading struct {
	Number  string // "2.11.6"
	Title   string
	Depth   int // 番号の階層の深さ (2.11 なら 2)
	Chapter int
	Section string
	Ref     Ref // 見出しの元資料上の位置 (PDF の物理ページ / Web のページとアンカー)
	Blocks  []Block
	Line    int // 出力ファイル中の見出し行番号 (1 始まり)。索引はここを指す
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
	Kind         BlockKind
	Ref          Ref        // この塊が現れた元資料上の位置
	Lines        []string   // prose / layout の本文行。figure では図中のラベル列
	Rows         [][]string // table の行。先頭行が見出し
	HeaderRef    Ref        // table: 前ページの列見出しを補った場合の出典。それ以外は空
	FigureSource string     // Web: 冊子の起点からの元画像の相対パス
	Figure       string     // figure: figures/ に置いたファイル名
}
