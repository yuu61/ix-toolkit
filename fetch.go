package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// fetch サブコマンド: マニフェストに書いた PDF をまとめて取得する。
//
// 配布サイトを巡回してリンクを推測すると、ページ改装のたびに壊れる。
// URL は人が 1 度書き、以降はマニフェストが唯一の真実になる方式を採る。

type Manifest struct {
	Docs []Doc `json:"docs"`
}

type Doc struct {
	Name    string `json:"name"`              // 出力ファイル名 (拡張子なし)
	URL     string `json:"url"`               // 取得元
	Version string `json:"version,omitempty"` // 版 (記録用)
	SHA256  string `json:"sha256,omitempty"`  // 既知なら検証、空なら取得後に記録を促す
	Profile string `json:"profile,omitempty"` // 変換に使うプロファイル JSON
	Title   string `json:"title,omitempty"`   // 資料タイトル
}

func runFetch(args []string) {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	manifestPath := fs.String("manifest", "manifest.json", "マニフェスト JSON")
	outDir := fs.String("out", "pdf", "PDF の保存先")
	force := fs.Bool("force", false, "既存ファイルがあっても再取得する")
	timeout := fs.Duration("timeout", 5*time.Minute, "1 件あたりのタイムアウト")
	parseFlags(fs, args)

	b, err := os.ReadFile(*manifestPath)
	if err != nil {
		fatal(fmt.Errorf("マニフェストを読めません: %w", err))
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		fatal(fmt.Errorf("マニフェストの解析に失敗: %w", err))
	}
	if len(m.Docs) == 0 {
		fatal(fmt.Errorf("マニフェストに docs がありません"))
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal(err)
	}

	client := &http.Client{Timeout: *timeout}
	failed := 0
	for _, d := range m.Docs {
		dst := filepath.Join(*outDir, d.Name+".pdf")
		if err := fetchOne(client, d, dst, *force); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %s\n", d.Name, err)
			failed++
		}
	}
	fmt.Printf("\n取得完了: %d 件中 %d 件成功\n", len(m.Docs), len(m.Docs)-failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func fetchOne(client *http.Client, d Doc, dst string, force bool) error {
	if !force {
		if sum, err := sha256File(dst); err == nil {
			if d.SHA256 == "" {
				fmt.Printf("  = %s (取得済み, sha256 %s)\n", d.Name, sum[:16])
				return nil
			}
			if strings.EqualFold(sum, d.SHA256) {
				fmt.Printf("  = %s (取得済み・検証 OK)\n", d.Name)
				return nil
			}
			fmt.Printf("  ! %s: 既存ファイルの sha256 が不一致。取り直します\n", d.Name)
		}
	}

	fmt.Printf("  → %s\n", d.URL)
	resp, err := client.Get(d.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}

	// 途中で失敗した半端なファイルを残さないよう、一時ファイル経由で置く
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), resp.Body)
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}

	sum := hex.EncodeToString(h.Sum(nil))
	if d.SHA256 != "" && !strings.EqualFold(sum, d.SHA256) {
		os.Remove(tmp)
		return fmt.Errorf("sha256 不一致\n    期待: %s\n    実際: %s", d.SHA256, sum)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}

	fmt.Printf("  ✓ %s (%.1f MB)\n", dst, float64(n)/(1<<20))
	if d.SHA256 == "" {
		fmt.Printf("    sha256: %s  ← マニフェストに書いておくと次回から検証されます\n", sum)
	}
	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
