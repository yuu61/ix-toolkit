package application

import (
	"errors"
	"fmt"
	"os"

	"github.com/yuu61/ix-toolkit/internal/manualbook/infrastructure"
)

// Probe は PDF を試し読みしてプロファイルを較正し、out に書く。
//
// テキスト層が無い PDF は較正できないので、経路が違うことを伝えて終了コード 2 で
// 止める (md も同じ PDF を読めない)。
func Probe(pdf, out, name string, windows, winSize int) error {
	defer infrastructure.CloseDoc(pdf)
	p, err := infrastructure.Calibrate(os.Stdout, pdf, name, windows, winSize)
	if errors.Is(err, infrastructure.ErrNoTextLayer) {
		fmt.Fprintln(os.Stderr, "\n  ⚠ テキスト層がほとんどありません（紙スキャン由来の画像 PDF の可能性）。")
		fmt.Fprintln(os.Stderr, "    md サブコマンドは使えません。画像化 → manualbook scan で分割 → OCR の経路が必要です。")
		return ReportedError(2)
	}
	if err != nil {
		return err
	}
	if err := infrastructure.SaveProfile(p, out); err != nil {
		return err
	}
	fmt.Printf("\nプロファイルを書き出しました: %s\n", out)
	fmt.Printf("次: manualbook md %s -profile %s -out out/\n", pdf, out)
	return nil
}
