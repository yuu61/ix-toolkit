package application

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/yuu61/ix-toolkit/internal/manualbook/infrastructure"
)

// DefaultUserAgent は web の取得で名乗る既定の User-Agent (理由は infrastructure/fetch.go)。
const DefaultUserAgent = infrastructure.DefaultUserAgent

// Fetch はマニフェストの資料をまとめて取得キャッシュへ取る。1 件が取れなくても
// 残りは取り、最後にまとめて数を出す。
func Fetch(manifestPath, outDir string, force bool, only string, timeout, delay time.Duration, ua string) error {
	m, err := infrastructure.ReadManifest(manifestPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	client := &http.Client{Timeout: timeout}
	failed, total := 0, 0
	for _, d := range m.Docs {
		if only != "" && d.Name != only {
			continue
		}
		total++
		if err := infrastructure.FetchDoc(os.Stdout, client, d, outDir, force, delay, ua); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %s\n", d.Name, err)
			failed++
		}
	}
	fmt.Printf("\n取得完了: %d 件中 %d 件成功\n", total, total-failed)
	if failed > 0 {
		return ReportedError(1)
	}
	return nil
}
