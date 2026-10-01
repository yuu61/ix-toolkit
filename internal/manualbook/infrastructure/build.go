package infrastructure

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// figuresMark は figures/ に全ページ焼いたときに build が残す印。どの PDF を何 dpi で
// 焼いたかを持ち、同じなら次の build は焼かない。ページ数だけで判定すると版が
// 上がって PDF が入れ替わっても気づけないので、原本の SHA-256 と全画像の存在を見る。
//
// 焼き直したいときは figures/ を消す (か manualbook figures を手で流す)。
// build の -force は取得の話で、ここには効かない。
type figuresMark struct {
	Source   string `json:"source"` // 元 PDF のファイル名
	SHA256   string `json:"sha256"`
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
	if parseErr := json.Unmarshal(b, &m); parseErr != nil {
		return false
	}
	if m.Source != BaseName(pdf) || m.DPI != dpi || m.Pages <= 0 || m.SHA256 == "" {
		return false
	}
	digest, err := pdfDigest(pdf)
	if err != nil || digest != m.SHA256 {
		return false
	}
	return allFiguresExist(outDir, m.Pages)
}

func allFiguresExist(outDir string, pages int) bool {
	for page := 1; page <= pages; page++ {
		path := filepath.Join(outDir, "figures", fmt.Sprintf("p%d.png", page))
		st, err := os.Stat(path)
		if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
			return false
		}
	}
	return true
}

func WriteFiguresMark(outDir, pdf string, dpi, pages int) error {
	digest, err := pdfDigest(pdf)
	if err != nil {
		return err
	}
	m := figuresMark{Source: BaseName(pdf), SHA256: digest, DPI: dpi, Pages: pages, Rendered: time.Now().Format("2006-01-02")}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "figures", figuresMarkName), append(b, '\n'), 0o644)
}

func pdfDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // Read-only file; io.Copy reports read failures.
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func invalidateFiguresMark(dir string) error {
	path := filepath.Join(dir, figuresMarkName)
	st, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("画像生成済みの印が通常ファイルではありません: %s", path)
	}
	return os.Remove(path)
}
