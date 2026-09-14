package infrastructure

import (
	"context"
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
func ReadManifest(manifestPath string) (domain.Manifest, error) {
	var m domain.Manifest
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return m, fmt.Errorf("マニフェストを読めません: %w", err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("マニフェストの解析に失敗: %w", err)
	}
	if len(m.Docs) == 0 {
		return m, errors.New("マニフェストに docs がありません")
	}
	for i := range m.Docs {
		d := &m.Docs[i]
		completeDoc(d)
	}
	return m, nil
}

// CachePath は取得キャッシュの中で資料が置かれる場所。pdf は <name>.pdf、web は <name>/。
func CachePath(cacheDir string, d domain.Doc) string {
	if d.Kind == domain.KindPDF {
		return filepath.Join(cacheDir, d.Name+".pdf")
	}
	return filepath.Join(cacheDir, d.Name)
}

// FetchDoc は資料 1 件を取得キャッシュへ取る。kind で作法が変わる。
func FetchDoc(w io.Writer, client *http.Client, d domain.Doc, cacheDir string, force bool, delay time.Duration, ua string) error {
	switch d.Kind {
	case domain.KindPDF:
		return fetchOne(w, client, d, CachePath(cacheDir, d), force)
	case domain.KindWeb:
		// キャッシュを冊単位で置き換えるので、資料名は単一のディレクトリ名に限る。
		if !filepath.IsLocal(d.Name) || d.Name == "." || strings.ContainsAny(d.Name, `/\`) {
			return fmt.Errorf("name は単一のディレクトリ名にしてください: %q", d.Name)
		}
		return fetchWeb(w, client, d, CachePath(cacheDir, d), force, delay, ua)
	case "":
		return errors.New(`kind が無い。"pdf" か "web" を書く (取得と検証の作法が変わるので推測しない)`)
	default:
		return fmt.Errorf("kind %q は知らない", d.Kind)
	}
}

// --- pdf ---

func fetchOne(w io.Writer, client *http.Client, d domain.Doc, dst string, force bool) error {
	// url が空でも、別の経路で手に入れた PDF が置いてあれば取得済みとして扱う。
	if !force {
		if _, err := os.Stat(dst); err == nil {
			_, _ = fmt.Fprintf(w, "  = %s (取得済み)\n", d.Name)
			return nil
		}
	}
	if d.URL == "" {
		return fmt.Errorf("url が空。配布ページを見て転記するか、手元にある PDF を %s に置く", dst)
	}

	_, _ = fmt.Fprintf(w, "  → %s\n", d.URL)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, d.URL, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}

	n, err := writeDownloadedPDF(dst, resp.Body)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "  ✓ %s (%.1f MB)\n", dst, float64(n)/(1<<20))
	return nil
}

// --- web ---

// DefaultUserAgent は配布サイト向けの既定値。サイトはブラウザ以外の User-Agent に 403 を返す (manualbook の名前で名乗ると
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
	base, baseErr := webBaseURL(d)
	if baseErr != nil {
		return baseErr
	}
	// 再取得が途中で失敗しても、次の build が取得済みと判断しないようにする。
	// 本文と ETag は成功するまで以前のものを保つ。
	if err := os.Remove(filepath.Join(dst, WebMetaName)); err != nil && !os.IsNotExist(err) {
		return err
	}
	c := webClient{client: client, base: base, force: force, userAgent: userAgent}

	// 1. index の指定箇所で版を確かめる。違えば取らずに止まる。
	_, _ = fmt.Fprintf(w, "  → %s\n", base)
	index, docnames, err := c.validatedIndex(w, d)
	if err != nil {
		return err
	}

	stage, previous, cleanup, err := prepareWebStage(dst)
	if err != nil {
		return err
	}
	defer cleanup()
	tags := readETags(dst)
	newTags := map[string]etagEntry{"index.html": {}}
	if err := os.WriteFile(filepath.Join(stage, "index.html"), index, 0o644); err != nil { // #nosec G306 -- 資格情報を含まないマニュアル・索引を他の利用者も読める形で出力する。
		return err
	}

	cache := webCache{webClient: c, dst: dst, stage: stage, tags: tags, newTags: newTags, delay: delay}
	if err := cache.fetchContents(w, index, docnames); err != nil {
		return err
	}

	if err := writeWebMetadata(stage, base, d, newTags); err != nil {
		return err
	}
	if err := publishWebCache(stage, dst, previous); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "  ✓ %s\n", dst)
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

// webVersionText は指定した版の記載だけを読む。本文全体から版を探すと、
// 旧版への言及や設定中の数字を誤って受け入れてしまう。
func webVersionText(page []byte, source string) (string, error) {
	if source == "" || source == "title" {
		return htmlTitle(page), nil
	}
	if source != "edition" {
		return "", fmt.Errorf("versionSource %q は知らない (title / edition)", source)
	}
	doc, err := html.Parse(strings.NewReader(string(page)))
	if err != nil {
		return "", err
	}
	h := findNode(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "h2" && domain.Collapse(nodeText(n)) == "版数"
	})
	if text, ok := editionParagraph(h); ok {
		return text, nil
	}
	return "", errors.New("index に「版数」直下の段落がありません (versionSource: edition)")
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
		if n.Type == html.ElementNode && n.Data == htmlImg {
			if src := attr(n, "src"); src != "" && !strings.Contains(src, "://") {
				out = append(out, src)
			}
		}
		return true
	})
	return out
}

func completeDoc(d *domain.Doc) {
	if d.Kind == "" {
		switch d.Series {
		case "ix":
			d.Kind = domain.KindPDF
		case "ix-r":
			d.Kind = domain.KindWeb
		}
	}
	if d.Profile == "" && d.Series != "" && d.Book != "" {
		d.Profile = fmt.Sprintf("profiles/nec-%s-%s.json", d.Series, d.Book)
	}
	inferPDFVersion(d)
	inferDocTitle(d)
	inferDocName(d)
}

func inferPDFVersion(d *domain.Doc) {
	if d.Version != "" || d.Kind != domain.KindPDF {
		return
	}
	base := pdfURLName(d.URL)
	for _, prefix := range []string{"CRM-ver", "FD-ver", "IX1-3K-EX-"} {
		if after, ok := strings.CutPrefix(base, prefix); ok {
			d.Version = after
			return
		}
	}
}

func pdfURLName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(u.Query().Get("file"), ".pdf")
}

func inferDocTitle(d *domain.Doc) {
	if d.Title == "" && d.Series != "" && d.Book != "" && d.Version != "" {
		title := inferredTitle(d)
		d.Title = title
	}
}

func inferDocName(d *domain.Doc) {
	if d.Name == "" && d.Series != "" && d.Book != "" && d.Version != "" {
		if d.Kind == domain.KindPDF {
			d.Name = pdfURLName(d.URL)
		}
		if d.Name == "" {
			d.Name = fmt.Sprintf("%s-%s-%s", strings.ToUpper(d.Series), strings.ToUpper(d.Book), d.Version)
		}
	}
}

type webClient struct {
	client    *http.Client
	base      *url.URL
	userAgent string
	force     bool
}

type webCache struct {
	tags, newTags map[string]etagEntry
	dst, stage    string
	webClient
	delay time.Duration
}

func (c webClient) get(rel string, tag etagEntry) (*http.Response, error) {
	u := c.base.ResolveReference(&url.URL{Path: rel})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	if !c.force {
		if tag.ETag != "" {
			req.Header.Set("If-None-Match", tag.ETag)
		}
		if tag.LastModified != "" {
			req.Header.Set("If-Modified-Since", tag.LastModified)
		}
	}
	return c.client.Do(req)
}

func (c webClient) readAll(rel string) ([]byte, error) {
	resp, err := c.get(rel, etagEntry{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %s", rel, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func (c webCache) fetchFile(rel string) (changed bool, body []byte, err error) {
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return false, nil, fmt.Errorf("キャッシュ内の相対パスではありません: %q", rel)
	}
	// 304 を受け入れるのは、対応する本文を読めたときだけ。
	cached, cacheErr := os.ReadFile(filepath.Join(c.dst, filepath.FromSlash(rel)))
	tag := c.tags[rel]
	if cacheErr != nil {
		tag = etagEntry{}
	}
	time.Sleep(c.delay)
	resp, err := c.get(rel, tag)
	if err != nil {
		return false, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	changed, body, tag, err = cachedResponse(resp, c.force, cached, cacheErr, tag)
	if err != nil {
		return false, nil, err
	}
	if err := writeCacheFile(c.stage, rel, body); err != nil {
		return false, nil, err
	}
	c.newTags[rel] = tag
	return changed, body, nil
}

func (cache webCache) fetchContents(w io.Writer, index []byte, docnames []string) error {
	// 3. ページを順に取り、参照している画像を集める。
	fetched, unchanged := 0, 0
	images := map[string]bool{}
	for _, src := range imageSources(index) {
		images[path.Clean(src)] = true
	}
	for i, name := range docnames {
		if name == "index" {
			continue
		}
		rel := name + ".html"
		changed, body, err := cache.fetchFile(rel)
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
			_, _ = fmt.Fprintf(w, "    %d/%d ページ\n", i+1, len(docnames))
		}
	}
	_, _ = fmt.Fprintf(w, "    ページ: 取得 %d / 変化なし %d\n", fetched, unchanged)

	return cache.fetchImages(w, images)
}

func (c webClient) docnames(w io.Writer, index []byte, d domain.Doc) ([]string, error) {
	versionText, err := webVersionText(index, d.VersionSource)
	if err != nil {
		return nil, err
	}
	if !domain.MatchesWebVersion(versionText, d.Version) {
		return nil, fmt.Errorf("版が合わない\n    マニフェスト: %s\n    サイトの版の記載: %s\n"+
			"    配布ページを確かめて manifest の version と url を更新する", d.Version, versionText)
	}
	_, _ = fmt.Fprintf(w, "    版 %s: %s\n", d.Version, versionText)

	// 2. searchindex.js からページの一覧を取る。これで取る対象が確定する。
	si, err := c.readAll("searchindex.js")
	if err != nil {
		return nil, fmt.Errorf("searchindex.js: %w", err)
	}
	m := docnamesRe.FindSubmatch(si)
	if m == nil {
		return nil, errors.New("searchindex.js に docnames が無い (Sphinx のサイトではない?)")
	}
	var docnames []string
	if err := json.Unmarshal(m[1], &docnames); err != nil {
		return nil, fmt.Errorf("docnames: %w", err)
	}
	_, _ = fmt.Fprintf(w, "    ページ: %d\n", len(docnames))

	return docnames, nil
}

func editionParagraph(h *html.Node) (string, bool) {
	if h != nil {
		for n := h.NextSibling; n != nil; n = n.NextSibling {
			if n.Type != html.ElementNode {
				continue
			}
			if n.Data == "p" {
				return domain.Collapse(nodeText(n)), true
			}
			break
		}
	}
	return "", false
}

func writeDownloadedPDF(dst string, body io.Reader) (int64, error) {
	// 途中で失敗した半端なファイルを残さないよう、一時ファイル経由で置く
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, body)
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}

	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}

	return n, nil
}

func webBaseURL(d domain.Doc) (*url.URL, error) {
	if d.URL == "" {
		return nil, errors.New("url が空 (配布ページを見て転記する)")
	}
	if d.Version == "" {
		return nil, errors.New("version が空 (web は index の版と突き合わせて検証するので必須)")
	}
	base, err := url.Parse(d.URL)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	return base, nil
}

func readETags(dst string) map[string]etagEntry {
	tags := map[string]etagEntry{}
	if b, err := os.ReadFile(filepath.Join(dst, etagFile)); err == nil {
		if err := json.Unmarshal(b, &tags); err != nil || tags == nil {
			tags = map[string]etagEntry{}
		}
	}
	return tags
}

func writeWebMetadata(stage string, base *url.URL, d domain.Doc, newTags map[string]etagEntry) error {
	// 5. 変換が読む覚え書きと、次回の条件付き GET 用の ETag。
	meta := WebMeta{
		Name: d.Name, Title: d.Title, URL: base.String(), Version: d.Version,
		Series: d.Series, Profile: d.Profile, Fetched: time.Now().Format("2006-01-02"),
	}
	b, err := json.MarshalIndent(newTags, "", "  ")
	if err != nil {
		return err
	}
	if writeErr := os.WriteFile(filepath.Join(stage, etagFile), append(b, '\n'), 0o644); writeErr != nil { // #nosec G306 -- 資格情報を含まないマニュアル・索引を他の利用者も読める形で出力する。
		return writeErr
	}
	b, err = json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, WebMetaName), append(b, '\n'), 0o644); err != nil { // #nosec G306 -- 資格情報を含まないマニュアル・索引を他の利用者も読める形で出力する。
		return err
	}
	return nil
}

func inferredTitle(d *domain.Doc) string {
	var title string
	switch d.Series {
	case "ix":
		title = "IX2000/IX3000 "
		switch d.Book {
		case "crm":
			title += "コマンドリファレンスマニュアル"
		case "fd":
			title += "機能説明書"
		case "ex":
			title += "設定事例集"
		}
		v, _, _ := strings.Cut(d.Version, "-")
		title += " " + v
	case "ix-r":
		title = "IX-R/IX-V "
		switch d.Book {
		case "crm":
			title += "コマンドリファレンス"
		case "fd":
			title += "機能説明書"
		case "ex":
			title += "設定事例集"
		}
		title += " " + d.Version
		if d.Book == "fd" {
			title += "版"
		}
	}
	return title
}

func (cache webCache) fetchImages(w io.Writer, images map[string]bool) error {
	// 4. 画像。_static (CSS/JS) は要らない。
	imgFetched, imgUnchanged := 0, 0
	for rel := range images {
		changed, _, err := cache.fetchFile(rel)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if changed {
			imgFetched++
		} else {
			imgUnchanged++
		}
	}
	_, _ = fmt.Fprintf(w, "    画像: 取得 %d / 変化なし %d\n", imgFetched, imgUnchanged)

	return nil
}

func cachedResponse(resp *http.Response, force bool, cached []byte, cacheErr error, tag etagEntry) (changed bool, body []byte, nextTag etagEntry, err error) {
	switch resp.StatusCode {
	case http.StatusNotModified:
		if force || cacheErr != nil || (tag.ETag == "" && tag.LastModified == "") {
			return false, nil, etagEntry{}, errors.New("本文を再利用できないリクエストに HTTP 304 が返りました")
		}
		body = cached
	case http.StatusOK:
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return false, nil, etagEntry{}, err
		}
		changed = true
		tag = etagEntry{ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}
	default:
		return false, nil, etagEntry{}, fmt.Errorf("HTTP %s", resp.Status)
	}
	return changed, body, tag, nil
}

func writeCacheFile(stage, rel string, body []byte) error {
	root, err := os.OpenRoot(stage)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	local := filepath.FromSlash(rel)
	if err := root.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return err
	}
	return root.WriteFile(local, body, 0o644) // #nosec G306 -- 公開マニュアルのキャッシュ。
}

func prepareWebStage(dst string) (stage, previous string, cleanup func(), err error) {
	if mkdirErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkdirErr != nil {
		return "", "", nil, mkdirErr
	}
	work, err := os.MkdirTemp(filepath.Dir(dst), ".manualbook-*")
	if err != nil {
		return "", "", nil, err
	}
	stage, previous = filepath.Join(work, "new"), filepath.Join(work, "previous")
	cleanup = func() {
		// 切り替えも復元も失敗した場合は、以前の本文を消さずに残す。
		if _, err := os.Stat(previous); os.IsNotExist(err) {
			_ = os.RemoveAll(work)
		}
	}
	if err := os.Mkdir(stage, 0o755); err != nil {
		cleanup()
		return "", "", nil, err
	}
	return stage, previous, cleanup, nil
}

func (c webClient) validatedIndex(w io.Writer, d domain.Doc) ([]byte, []string, error) {
	index, err := c.readAll("")
	if err != nil {
		return nil, nil, err
	}
	docnames, err := c.docnames(w, index, d)
	if err != nil {
		return nil, nil, err
	}

	return index, docnames, nil
}
