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
// PDF から読むと prose と layout の 2 つしか出ない。テキストからは表と図を
// 見分けられないので、どちらも版面どおりに囲う (layout)。Web から読むと
// 表は <table>、図は <figure> と元から分かれているので、table と figure が増える。
type BlockKind int

const (
	BlockProse  BlockKind = iota // 地の文
	BlockLayout                  // 版面固定 (表・図・コンソール出力を ```text で囲う)
	BlockTable                   // 表 (Markdown の表に出す)
	BlockFigure                  // 図 (figures/ に置いたファイルへのリンクとラベル列)
)

// Block は本文の一区切り。
type Block struct {
	Kind   BlockKind
	Ref    Ref        // この塊が現れた元資料上の位置
	Lines  []string   // prose / layout の本文行。figure では図中のラベル列
	Rows   [][]string // table の行。先頭行が見出し
	Figure string     // figure: figures/ に置いたファイル名
}
