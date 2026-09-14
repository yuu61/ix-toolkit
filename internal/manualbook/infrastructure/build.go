package infrastructure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// figuresMark は figures/ に全ページ焼いたときに build が残す印。どの PDF を何 dpi で
// 焼いたかを持ち、同じなら次の build は焼かない。ページ数だけで判定すると版が
// 上がって PDF が入れ替わっても気づけないので、元の名前で見る。
//
// 焼き直したいときは figures/ を消す (か manualbook figures を手で流す)。
// build の -force は取得の話で、ここには効かない。
type figuresMark struct {
	Source   string `json:"source"`
	Rendered string `json:"rendered"`
	DPI      int    `json:"dpi"`
	Pages    int    `json:"pages"`
}

// figuresMarkName は figureNameRe (p<番号>.png) に掛からない名前にしてある。
const figuresMarkName = ".manualbook.json"

func FiguresDone(outDir, pdf string, dpi int) bool {
	b, err := os.ReadFile(filepath.Join(outDir, "figures", figuresMarkName))
	if err != nil {
		return false
	}
	var m figuresMark
	if err := json.Unmarshal(b, &m); err != nil {
		return false
	}
	return m.Source == BaseName(pdf) && m.DPI == dpi
}

func WriteFiguresMark(outDir, pdf string, dpi, pages int) error {
	m := figuresMark{Source: BaseName(pdf), DPI: dpi, Pages: pages, Rendered: time.Now().Format("2006-01-02")}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "figures", figuresMarkName), append(b, '\n'), 0o644) // #nosec G306 -- 資格情報を含まないマニュアル・索引を他の利用者も読める形で出力する。
}
