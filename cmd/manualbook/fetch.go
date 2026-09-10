package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// fetch サブコマンド: マニフェストに書いた資料をまとめて取得する。
//
// 配布サイトを巡回してリンクを推測すると、ページ改装のたびに壊れる。
// URL は人が 1 度書き、以降はマニフェストが唯一の真実になる方式を採る。
//
// 資料は 2 種類ある。
//
//   - kind "pdf": PDF を 1 本取る。
//   - kind "web": Sphinx で組まれた Web マニュアルを、ページごとに取得キャッシュへ
//     置く。取る対象は探索しない。searchindex.js にページの一覧があるので、
//     それを 1 本取れば全ページが確定する (リンクを辿って広げる動きは無い)。
//     版は index の <title> に入っているので、マニフェストの version と突き合わせ、
//     版が上がっていたら取得せずに止まる。
//
// どちらも、利用者が明示的に叩いたときにだけ動く。定期的に取りに行く仕組みは無い。

type Manifest struct {
	Docs []Doc `json:"docs"`
}

type Doc struct {
	Name    string `json:"name"`              // 出力ファイル名 / 取得キャッシュのディレクトリ名
	Series  string `json:"series,omitempty"`  // 機種の系列 (ix / ix-r)
	Book    string `json:"book,omitempty"`    // 冊子 (crm / fd)。変換結果の置き場 <系列>/<冊子>/ を決める
	Kind    string `json:"kind"`              // "pdf" か "web"。明示する
	URL     string `json:"url"`               // 取得元 (web は冊子の index の URL)
	Version string `json:"version,omitempty"` // 版。web では <title> と突き合わせる
	Profile string `json:"profile,omitempty"` // 変換に使うプロファイル JSON
	Title   string `json:"title,omitempty"`   // 資料タイトル
}

// readManifest はマニフェストを読む。
func readManifest(path string) (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(path)
	if err != nil {
		return m, fmt.Errorf("マニフェストを読めません: %w", err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("マニフェストの解析に失敗: %w", err)
	}
	if len(m.Docs) == 0 {
		return m, errors.New("マニフェストに docs がありません")
	}
	return m, nil
}

// cachePath は取得キャッシュの中で資料が置かれる場所。pdf は <name>.pdf、web は <name>/。
func (d Doc) cachePath(cacheDir string) string {
	if d.Kind == "pdf" {
		return filepath.Join(cacheDir, d.Name+".pdf")
	}
	return filepath.Join(cacheDir, d.Name)
}

// fetchDoc は資料 1 件を取得キャッシュへ取る。kind で作法が変わる。
func fetchDoc(client *http.Client, d Doc, cacheDir string, force bool, delay time.Duration, ua string) error {
	switch d.Kind {
	case "pdf":
		return fetchOne(client, d, d.cachePath(cacheDir), force)
	case "web":
		return fetchWeb(client, d, d.cachePath(cacheDir), force, delay, ua)
	case "":
		return fmt.Errorf(`kind が無い。"pdf" か "web" を書く (取得と検証の作法が変わるので推測しない)`)
	default:
		return fmt.Errorf("kind %q は知らない", d.Kind)
	}
}

func runFetch(args []string) {
	fs := flag.NewFlagSet("fetch", flag.ExitOnError)
	manifestPath := fs.String("manifest", "manifest.json", "マニフェスト JSON")
	outDir := fs.String("out", "pdf", "保存先 (pdf はここに <name>.pdf、web は <name>/ を置く)")
	force := fs.Bool("force", false, "既存ファイルがあっても再取得する")
	only := fs.String("only", "", "この name の資料だけ取得する")
	timeout := fs.Duration("timeout", 5*time.Minute, "1 件あたりのタイムアウト")
	delay := fs.Duration("delay", time.Second, "web: リクエストの間隔")
	ua := fs.String("user-agent", defaultUserAgent, "web: User-Agent")
	parseFlags(fs, args)

	m, err := readManifest(*manifestPath)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal(err)
	}

	client := &http.Client{Timeout: *timeout}
	failed, total := 0, 0
	for _, d := range m.Docs {
		if *only != "" && d.Name != *only {
			continue
		}
		total++
		if err := fetchDoc(client, d, *outDir, *force, *delay, *ua); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %s\n", d.Name, err)
			failed++
		}
	}
	fmt.Printf("\n取得完了: %d 件中 %d 件成功\n", total, total-failed)
	if failed > 0 {
		os.Exit(1)
	}
}

// --- pdf ---

func fetchOne(client *http.Client, d Doc, dst string, force bool) error {
	// url が空でも、別の経路で手に入れた PDF が置いてあれば取得済みとして扱う。
	if !force {
		if _, err := os.Stat(dst); err == nil {
			fmt.Printf("  = %s (取得済み)\n", d.Name)
			return nil
		}
	}
	if d.URL == "" {
		return fmt.Errorf("url が空。配布ページを見て転記するか、手元にある PDF を %s に置く", dst)
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
	n, err := io.Copy(f, resp.Body)
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}

	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}

	fmt.Printf("  ✓ %s (%.1f MB)\n", dst, float64(n)/(1<<20))
	return nil
}

// --- web ---

// 配布サイトはブラウザ以外の User-Agent に 403 を返す (manualbook の名前で名乗ると
// index すら取れない)。この取得は利用者が自分の手で 1 回叩くもので、ブラウザで
// 同じページを順に開くのと変わらないので、ブラウザとして名乗る。-user-agent で変えられる。
const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// etagFile は取得キャッシュに置く、ページごとの ETag / Last-Modified。
// 2 回目以降は条件付き GET にして、変わったページだけ落とす。
// 更新運用が「告知を見たら取り直す」なので、取り直しを軽くしておく意味は大きい。
const etagFile = ".etags.json"

type etagEntry struct {
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
}

var docnamesRe = regexp.MustCompile(`"docnames"\s*:\s*(\[[^\]]*\])`)

// webFetched は取得キャッシュがその版で取り切ってあるか。.manualbook.json は
// fetchWeb が全ページを置いた最後に書くので、これが版つきで残っていれば完備と
// みなせる (途中で止まったキャッシュには無い)。
//
// fetch 自身はこれを見ない。fetch は ETag で 1 ページずつ確かめる軽い更新の口で、
// 全ページを 1 秒おきに問い合わせる (数分掛かる)。build は初回に 1 回取れば
// よいので、完備なら問い合わせず飛ばす。
func webFetched(dst string, d Doc) bool {
	meta, err := readWebMeta(dst)
	return err == nil && meta.Version == d.Version
}

func fetchWeb(client *http.Client, d Doc, dst string, force bool, delay time.Duration, userAgent string) error {
	if d.URL == "" {
		return errors.New("url が空 (配布ページを見て転記する)")
	}
	if d.Version == "" {
		return errors.New("version が空 (web は index の <title> と突き合わせて検証するので必須)")
	}
	base, err := url.Parse(d.URL)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	get := func(rel string, tag etagEntry) (*http.Response, error) {
		u := base.ResolveReference(&url.URL{Path: rel})
		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		if !force {
			if tag.ETag != "" {
				req.Header.Set("If-None-Match", tag.ETag)
			}
			if tag.LastModified != "" {
				req.Header.Set("If-Modified-Since", tag.LastModified)
			}
		}
		return client.Do(req)
	}
	readAll := func(rel string) ([]byte, error) {
		resp, err := get(rel, etagEntry{})
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: HTTP %s", rel, resp.Status)
		}
		return io.ReadAll(resp.Body)
	}

	// 1. index の <title> で版を確かめる。違えば取らずに止まる。
	fmt.Printf("  → %s\n", base)
	index, err := readAll("")
	if err != nil {
		return err
	}
	title := htmlTitle(index)
	if !strings.Contains(title, d.Version) {
		return fmt.Errorf("版が合わない\n    マニフェスト: %s\n    サイトの <title>: %s\n"+
			"    版が上がっている。配布ページを確かめて manifest の version と url を更新する", d.Version, title)
	}
	fmt.Printf("    版 %s: %s\n", d.Version, title)

	// 2. searchindex.js からページの一覧を取る。これで取る対象が確定する。
	si, err := readAll("searchindex.js")
	if err != nil {
		return fmt.Errorf("searchindex.js: %w", err)
	}
	m := docnamesRe.FindSubmatch(si)
	if m == nil {
		return errors.New("searchindex.js に docnames が無い (Sphinx のサイトではない?)")
	}
	var docnames []string
	if err := json.Unmarshal(m[1], &docnames); err != nil {
		return fmt.Errorf("docnames: %w", err)
	}
	fmt.Printf("    ページ: %d\n", len(docnames))

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	tags := map[string]etagEntry{}
	if b, err := os.ReadFile(filepath.Join(dst, etagFile)); err == nil {
		_ = json.Unmarshal(b, &tags)
	}
	if err := os.WriteFile(filepath.Join(dst, "index.html"), index, 0o644); err != nil {
		return err
	}

	// 3. ページを順に取り、参照している画像を集める。
	fetched, unchanged := 0, 0
	images := map[string]bool{}
	fetchFile := func(rel string) (changed bool, body []byte, err error) {
		time.Sleep(delay)
		resp, err := get(rel, tags[rel])
		if err != nil {
			return false, nil, err
		}
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusNotModified:
			return false, nil, nil
		case http.StatusOK:
		default:
			return false, nil, fmt.Errorf("HTTP %s", resp.Status)
		}
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return false, nil, err
		}
		local := filepath.Join(dst, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
			return false, nil, err
		}
		if err := os.WriteFile(local, body, 0o644); err != nil {
			return false, nil, err
		}
		tags[rel] = etagEntry{ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}
		return true, body, nil
	}
	for i, name := range docnames {
		if name == "index" {
			continue
		}
		rel := name + ".html"
		changed, body, err := fetchFile(rel)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if changed {
			fetched++
		} else {
			unchanged++
			body, _ = os.ReadFile(filepath.Join(dst, filepath.FromSlash(rel)))
		}
		for _, src := range imageSources(body) {
			images[path.Join(path.Dir(rel), src)] = true
		}
		if (i+1)%20 == 0 {
			fmt.Printf("    %d/%d ページ\n", i+1, len(docnames))
		}
	}
	fmt.Printf("    ページ: 取得 %d / 変化なし %d\n", fetched, unchanged)

	// 4. 画像。_static (CSS/JS) は要らない。
	imgFetched, imgUnchanged := 0, 0
	for rel := range images {
		changed, _, err := fetchFile(rel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "    ! %s: %s\n", rel, err)
			continue
		}
		if changed {
			imgFetched++
		} else {
			imgUnchanged++
		}
	}
	fmt.Printf("    画像: 取得 %d / 変化なし %d\n", imgFetched, imgUnchanged)

	// 5. 変換が読む覚え書きと、次回の条件付き GET 用の ETag。
	meta := webMeta{
		Name: d.Name, Title: d.Title, URL: base.String(), Version: d.Version,
		Series: d.Series, Profile: d.Profile, Fetched: time.Now().Format("2006-01-02"),
	}
	if b, err := json.MarshalIndent(meta, "", "  "); err == nil {
		if err := os.WriteFile(filepath.Join(dst, webMetaName), append(b, '\n'), 0o644); err != nil {
			return err
		}
	}
	if b, err := json.MarshalIndent(tags, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dst, etagFile), append(b, '\n'), 0o644)
	}
	fmt.Printf("  ✓ %s\n", dst)
	return nil
}

// htmlTitle は <title> の中身。
func htmlTitle(page []byte) string {
	doc, err := html.Parse(strings.NewReader(string(page)))
	if err != nil {
		return ""
	}
	t := findNode(doc, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == "title" })
	if t == nil {
		return ""
	}
	return collapse(nodeText(t))
}

// imageSources はページが参照する <img src> (ページからの相対パス)。
func imageSources(page []byte) []string {
	doc, err := html.Parse(strings.NewReader(string(page)))
	if err != nil {
		return nil
	}
	var out []string
	walk(doc, func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data == "img" {
			if src := attr(n, "src"); src != "" && !strings.Contains(src, "://") {
				out = append(out, src)
			}
		}
		return true
	})
	return out
}
