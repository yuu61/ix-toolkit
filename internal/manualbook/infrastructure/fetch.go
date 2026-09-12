package infrastructure

import (
	"encoding/json"
	"errors"
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

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
	"golang.org/x/net/html"
)

// 資料の取得。マニフェストに書いた資料を取得キャッシュへ取る。
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

// ReadManifest はマニフェストを読む。
func ReadManifest(path string) (domain.Manifest, error) {
	var m domain.Manifest
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

// CachePath は取得キャッシュの中で資料が置かれる場所。pdf は <name>.pdf、web は <name>/。
func CachePath(cacheDir string, d domain.Doc) string {
	if d.Kind == "pdf" {
		return filepath.Join(cacheDir, d.Name+".pdf")
	}
	return filepath.Join(cacheDir, d.Name)
}

// FetchDoc は資料 1 件を取得キャッシュへ取る。kind で作法が変わる。
func FetchDoc(w io.Writer, client *http.Client, d domain.Doc, cacheDir string, force bool, delay time.Duration, ua string) error {
	switch d.Kind {
	case "pdf":
		return fetchOne(w, client, d, CachePath(cacheDir, d), force)
	case "web":
		// キャッシュを冊単位で置き換えるので、資料名は単一のディレクトリ名に限る。
		if !filepath.IsLocal(d.Name) || d.Name == "." || strings.ContainsAny(d.Name, `/\`) {
			return fmt.Errorf("name は単一のディレクトリ名にしてください: %q", d.Name)
		}
		return fetchWeb(w, client, d, CachePath(cacheDir, d), force, delay, ua)
	case "":
		return fmt.Errorf(`kind が無い。"pdf" か "web" を書く (取得と検証の作法が変わるので推測しない)`)
	default:
		return fmt.Errorf("kind %q は知らない", d.Kind)
	}
}

// --- pdf ---

func fetchOne(w io.Writer, client *http.Client, d domain.Doc, dst string, force bool) error {
	// url が空でも、別の経路で手に入れた PDF が置いてあれば取得済みとして扱う。
	if !force {
		if _, err := os.Stat(dst); err == nil {
			fmt.Fprintf(w, "  = %s (取得済み)\n", d.Name)
			return nil
		}
	}
	if d.URL == "" {
		return fmt.Errorf("url が空。配布ページを見て転記するか、手元にある PDF を %s に置く", dst)
	}

	fmt.Fprintf(w, "  → %s\n", d.URL)
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

	fmt.Fprintf(w, "  ✓ %s (%.1f MB)\n", dst, float64(n)/(1<<20))
	return nil
}

// --- web ---

// 配布サイトはブラウザ以外の User-Agent に 403 を返す (manualbook の名前で名乗ると
// index すら取れない)。この取得は利用者が自分の手で 1 回叩くもので、ブラウザで
// 同じページを順に開くのと変わらないので、ブラウザとして名乗る。-user-agent で変えられる。
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// etagFile は取得キャッシュに置く、ページごとの ETag / Last-Modified。
// 2 回目以降は条件付き GET にして、変わったページだけ落とす。
// 更新運用が「告知を見たら取り直す」なので、取り直しを軽くしておく意味は大きい。
const etagFile = ".etags.json"

type etagEntry struct {
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
}

var docnamesRe = regexp.MustCompile(`"docnames"\s*:\s*(\[[^\]]*\])`)

// WebFetched は取得キャッシュがその版で取り切ってあるか。.manualbook.json は
// fetchWeb が全ファイルを置いた最後に書く。版に加え、取得一覧にあるファイルの
// 実体も確認する (途中で止まったキャッシュや、ファイルの欠損は取得し直す)。
//
// fetch 自身はこれを見ない。fetch は ETag で 1 ページずつ確かめる軽い更新の口で、
// 全ページを 1 秒おきに問い合わせる (数分掛かる)。build は初回に 1 回取れば
// よいので、完備なら問い合わせず飛ばす。
func WebFetched(dst string, d domain.Doc) bool {
	meta, err := ReadWebMeta(dst)
	if err != nil || meta.Version != d.Version {
		return false
	}
	b, err := os.ReadFile(filepath.Join(dst, etagFile))
	var tags map[string]etagEntry
	if err != nil || json.Unmarshal(b, &tags) != nil || tags == nil {
		return false
	}
	tags["index.html"] = etagEntry{}
	for rel := range tags {
		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			return false
		}
		st, err := os.Stat(filepath.Join(dst, filepath.FromSlash(rel)))
		if err != nil || !st.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func fetchWeb(w io.Writer, client *http.Client, d domain.Doc, dst string, force bool, delay time.Duration, userAgent string) error {
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
	// 再取得が途中で失敗しても、次の build が取得済みと判断しないようにする。
	// 本文と ETag は成功するまで以前のものを保つ。
	if err := os.Remove(filepath.Join(dst, WebMetaName)); err != nil && !os.IsNotExist(err) {
		return err
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
	fmt.Fprintf(w, "  → %s\n", base)
	index, err := readAll("")
	if err != nil {
		return err
	}
	title := htmlTitle(index)
	if !domain.MatchesWebVersion(title, d.Version) {
		return fmt.Errorf("版が合わない\n    マニフェスト: %s\n    サイトの <title>: %s\n"+
			"    版が上がっている。配布ページを確かめて manifest の version と url を更新する", d.Version, title)
	}
	fmt.Fprintf(w, "    版 %s: %s\n", d.Version, title)

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
	fmt.Fprintf(w, "    ページ: %d\n", len(docnames))

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	work, err := os.MkdirTemp(filepath.Dir(dst), ".manualbook-*")
	if err != nil {
		return err
	}
	stage, previous := filepath.Join(work, "new"), filepath.Join(work, "previous")
	defer func() {
		// 切り替えも復元も失敗した場合は、以前の本文を消さずに残す。
		if _, err := os.Stat(previous); os.IsNotExist(err) {
			_ = os.RemoveAll(work)
		}
	}()
	if err := os.Mkdir(stage, 0o755); err != nil {
		return err
	}
	tags := map[string]etagEntry{}
	if b, err := os.ReadFile(filepath.Join(dst, etagFile)); err == nil {
		if err := json.Unmarshal(b, &tags); err != nil || tags == nil {
			tags = map[string]etagEntry{}
		}
	}
	newTags := map[string]etagEntry{"index.html": {}}
	if err := os.WriteFile(filepath.Join(stage, "index.html"), index, 0o644); err != nil {
		return err
	}

	// 3. ページを順に取り、参照している画像を集める。
	fetched, unchanged := 0, 0
	images := map[string]bool{}
	for _, src := range imageSources(index) {
		images[path.Clean(src)] = true
	}
	fetchFile := func(rel string) (changed bool, body []byte, err error) {
		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			return false, nil, fmt.Errorf("キャッシュ内の相対パスではありません: %q", rel)
		}
		// 304 を受け入れるのは、対応する本文を読めたときだけ。
		cached, cacheErr := os.ReadFile(filepath.Join(dst, filepath.FromSlash(rel)))
		tag := tags[rel]
		if cacheErr != nil {
			tag = etagEntry{}
		}
		time.Sleep(delay)
		resp, err := get(rel, tag)
		if err != nil {
			return false, nil, err
		}
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusNotModified:
			if force || cacheErr != nil || (tag.ETag == "" && tag.LastModified == "") {
				return false, nil, fmt.Errorf("本文を再利用できないリクエストに HTTP 304 が返りました")
			}
			body = cached
		case http.StatusOK:
			body, err = io.ReadAll(resp.Body)
			if err != nil {
				return false, nil, err
			}
			changed = true
			tag = etagEntry{ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}
		default:
			return false, nil, fmt.Errorf("HTTP %s", resp.Status)
		}
		local := filepath.Join(stage, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
			return false, nil, err
		}
		if err := os.WriteFile(local, body, 0o644); err != nil {
			return false, nil, err
		}
		newTags[rel] = tag
		return changed, body, nil
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
		}
		for _, src := range imageSources(body) {
			images[path.Join(path.Dir(rel), src)] = true
		}
		if (i+1)%20 == 0 {
			fmt.Fprintf(w, "    %d/%d ページ\n", i+1, len(docnames))
		}
	}
	fmt.Fprintf(w, "    ページ: 取得 %d / 変化なし %d\n", fetched, unchanged)

	// 4. 画像。_static (CSS/JS) は要らない。
	imgFetched, imgUnchanged := 0, 0
	for rel := range images {
		changed, _, err := fetchFile(rel)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if changed {
			imgFetched++
		} else {
			imgUnchanged++
		}
	}
	fmt.Fprintf(w, "    画像: 取得 %d / 変化なし %d\n", imgFetched, imgUnchanged)

	// 5. 変換が読む覚え書きと、次回の条件付き GET 用の ETag。
	meta := WebMeta{
		Name: d.Name, Title: d.Title, URL: base.String(), Version: d.Version,
		Series: d.Series, Profile: d.Profile, Fetched: time.Now().Format("2006-01-02"),
	}
	b, err := json.MarshalIndent(newTags, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, etagFile), append(b, '\n'), 0o644); err != nil {
		return err
	}
	b, err = json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, WebMetaName), append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := publishWebCache(stage, dst, previous); err != nil {
		return err
	}
	fmt.Fprintf(w, "  ✓ %s\n", dst)
	return nil
}

// publishWebCache は揃った資料だけを公開する。既存ディレクトリへの Rename は
// Windows ではできないため、以前の本文を退避してから切り替える。
func publishWebCache(stage, dst, previous string) error {
	hadPrevious := false
	if err := os.Rename(dst, previous); err == nil {
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(stage, dst); err != nil {
		if hadPrevious {
			if restoreErr := os.Rename(previous, dst); restoreErr != nil {
				return errors.Join(err, fmt.Errorf("以前のキャッシュは %s に残っています: %w", previous, restoreErr))
			}
		}
		return err
	}
	if hadPrevious {
		return os.RemoveAll(previous)
	}
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
	return domain.Collapse(nodeText(t))
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
