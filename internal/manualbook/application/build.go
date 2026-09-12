package application

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
	"github.com/yuu61/ix-toolkit/internal/manualbook/infrastructure"
)

// Build サブコマンド: マニフェストの資料を取得し、変換し、系列間の差分表まで作る。
//
// fetch → md → diff を資料ごとにフラグを並べて流すのは手順書を写す作業でしかなく、
// その指定 (系列・冊子・版・プロファイル) はすべてマニフェストに書いてある。
// Build はマニフェストを唯一の入力にして、手元に何も無い状態から ix-manual が
// 読む形 (<manuals>/<系列>/<冊子>/ と <manuals>/ix-r/diff.tsv) まで 1 回で作る。
//
// 資料ごとに独立して進め、1 冊が取れなくても残りは作る (PDF は url を書けない
// ことがあり、Web だけ先に揃えたい)。diff は両系列が揃ったときだけ作る。
// 取得済みの資料は取りに行かない。取り直したいときだけ -force を付ける。
//
// 時間の大半は Web の取得で、1 秒おきに数百リクエストを送るので冊で数分掛かる
// (礼儀の方なので縮めない)。その待ち時間で PDF の変換とページ画像を済ませたい
// ので、Web の取得だけ別 goroutine で先に始め、変換は PDF → Web の順に 1 本で
// 進める。PDF の文字抽出と表の解析は infrastructure 内でページごとに並列化する。
// そこで使う各 PDFium インスタンスは独立させ、同時に共有しない。
//
// PDF の機能説明書はページ画像も必ず焼く (図は版面を見ないと向きが分からず、
// エージェントが開けるのは PNG だけ)。全ページで 72 秒・404 MB だが、初回は
// Web を待つ時間に収まる。焼いてあれば飛ばすので、2 回目以降は変換だけになる。

// buildFigureDPI はページ画像の解像度。この資料の最小文字 (約 7pt) が読める下限
// が 150dpi (sections.go の実測)。
const buildFigureDPI = 150

func Build(manifestPath, cacheDir, manualsDir string, force bool, only string) error {
	m, err := infrastructure.ReadManifest(manifestPath)
	if err != nil {
		return err
	}
	if manualsDir == "" {
		return fmt.Errorf("変換結果の置き場が決まりません。-manuals で指定してください")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	// マニフェストの中の相対パス (profile、手で導いた差分) はマニフェストの場所から解く。
	// リポジトリ直下で流すのと、外から -manifest で指すのとで同じ意味になる。
	manifestDir := filepath.Dir(manifestPath)
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(manifestDir, p)
	}

	// 手元で済む資料 (PDF と、取得済みの Web) と、取得を裏で先に始める資料 (これから
	// 取る Web) に分ける。[i/n] はマニフェストの順ではなくこの順で振る。
	// 置き場を決められない資料は取りに行っても無駄なので、裏には回さず
	// 表で CheckDoc の理由を出して終わる。
	var local, web []domain.Doc
	for _, d := range m.Docs {
		if only != "" && d.Name != only {
			continue
		}
		if d.Kind == "web" && domain.CheckDoc(d) == nil && (force || !infrastructure.WebFetched(infrastructure.CachePath(cacheDir, d), d)) {
			web = append(web, d)
		} else {
			local = append(local, d)
		}
	}
	if len(local)+len(web) == 0 {
		return fmt.Errorf("マニフェストに name %q の資料がありません", only)
	}

	client := &http.Client{Timeout: 5 * time.Minute}

	// Web の取得。1 つの goroutine で順に取る (同じサイトなので、並べて投げない)。
	// 進捗は表の出力に混ざるので、行の頭に資料名を付けて見分けられるようにする。
	fetched := make([]chan error, len(web))
	for k := range web {
		fetched[k] = make(chan error, 1)
	}
	go func() {
		for k, d := range web {
			w := &prefixWriter{prefix: d.Name + ": ", dst: os.Stdout}
			fetched[k] <- infrastructure.FetchDoc(w, client, d, cacheDir, force, time.Second, infrastructure.DefaultUserAgent)
		}
	}()
	if len(local) > 0 && len(web) > 0 {
		names := make([]string, len(web))
		for k, d := range web {
			names[k] = d.Name
		}
		fmt.Printf("Web の取得は裏で先に始める: %s\n\n", strings.Join(names, ", "))
	}

	n, i, failed := len(local)+len(web), 0, 0
	step := func(d domain.Doc, fetch func() error) {
		i++
		err := domain.CheckDoc(d)
		if err == nil {
			outDir := filepath.Join(manualsDir, d.Series, d.Book)
			fmt.Printf("[%d/%d] %s (%s) → %s\n", i, n, d.Name, d.Kind, outDir)
			if err = fetch(); err == nil {
				err = convertDoc(d, cacheDir, outDir, resolve(d.Profile))
			}
		} else {
			fmt.Printf("[%d/%d] %s (%s)\n", i, n, d.Name, d.Kind)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %s\n", d.Name, err)
			failed++
		}
		fmt.Println()
	}
	for _, d := range local {
		step(d, func() error { return fetchIfNeeded(os.Stdout, client, d, cacheDir, force) })
	}
	for k, d := range web {
		step(d, func() error { return <-fetched[k] })
	}

	// 系列間の差分表。無印と IX-R の両方が揃ったときだけ作る。
	ixDir, ixrDir := filepath.Join(manualsDir, "ix"), filepath.Join(manualsDir, "ix-r")
	if missing := missingFiles(infrastructure.DiffInputs(ixDir, ixrDir)); len(missing) == 0 {
		fmt.Println("diff.tsv (無印 → IX-R のコマンド対応表)")
		derived := filepath.Join(manifestDir, "profiles", "ix-r-derived-diff.tsv")
		if _, err := os.Stat(derived); err != nil {
			derived = "" // Diff がカレントと実行ファイルの隣を探し、無ければ警告する
		}
		if err := Diff(ixDir, ixrDir, "", derived); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ diff.tsv: %s\n", err)
			failed++
		}
	} else {
		fmt.Printf("diff.tsv は作らない (両系列が揃っていない: %s が無い)\n", missing[0])
	}

	fmt.Printf("\n完了: %d 件中 %d 件 → %s\n", n, n-failed, manualsDir)
	if failed > 0 {
		return ReportedError(1)
	}
	return nil
}

// fetchIfNeeded は取得済みなら取りに行かない FetchDoc。PDF は実体の有無 (fetchOne が
// 見る)、Web は取り切った印 (.manualbook.json の版) で決める。-force ならどちらも取り直す。
func fetchIfNeeded(w io.Writer, client *http.Client, d domain.Doc, cacheDir string, force bool) error {
	if d.Kind == "web" && !force && infrastructure.WebFetched(infrastructure.CachePath(cacheDir, d), d) {
		fmt.Fprintf(w, "  = %s (取得済み)\n", d.Name)
		return nil
	}
	return infrastructure.FetchDoc(w, client, d, cacheDir, force, time.Second, infrastructure.DefaultUserAgent)
}

// convertDoc は取得済みの資料 1 件を <manuals>/<系列>/<冊子>/ に変換する。
// PDF の機能説明書なら、先にページ画像を焼く (囲みに [ページ画像] を付けるかは
// 変換時に figures/ を見て決めるので、この順でないとリンクが付かない)。
func convertDoc(d domain.Doc, cacheDir, outDir, profilePath string) error {
	p := domain.DefaultProfile()
	if profilePath != "" {
		var err error
		if p, err = infrastructure.LoadProfile(profilePath); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	// ページ画像が要るのは PDF の機能説明書だけ。コマンド辞書として読む資料では
	// 誰も参照しない (md の側で -figures を断る条件と同じ)。
	input := infrastructure.CachePath(cacheDir, d)
	if d.Kind == "pdf" {
		defer infrastructure.CloseDoc(input)
	}
	if d.Kind == "pdf" && !p.HasCommandEntries() {
		if infrastructure.FiguresDone(outDir, input, buildFigureDPI) {
			fmt.Printf("ページ画像: 焼いてある (%s)\n", filepath.Join(outDir, "figures"))
		} else {
			n, err := infrastructure.RenderFigures(input, outDir, buildFigureDPI, "")
			if err != nil {
				return err
			}
			fmt.Printf("ページ画像: %d 枚を焼きました\n", n)
			if err := infrastructure.WriteFiguresMark(outDir, input, buildFigureDPI, n); err != nil {
				return err
			}
		}
	}

	o := MDOptions{
		Input: input, OutDir: outDir, ProfilePath: profilePath,
		Title: d.Title, Series: d.Series, Version: d.Version,
	}
	if d.Kind == "pdf" {
		return convertPDF(o)
	}
	return Convert(o)
}

// prefixWriter は各行の頭に印を付ける。裏で走る Web 取得の進捗が表の出力に
// 混ざっても、どの資料の行か分かるようにする。1 回の Write を 1 回で書き出す
// ので、行ごとに書く相手なら表の行と混線しない。
type prefixWriter struct {
	prefix string
	dst    io.Writer
	mid    bool // 行の途中で終わった (次の書き込みには印を付けない)
}

func (w *prefixWriter) Write(p []byte) (int, error) {
	var buf bytes.Buffer
	for rest := p; len(rest) > 0; {
		if !w.mid {
			buf.WriteString(w.prefix)
		}
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			buf.Write(rest)
			w.mid = true
			break
		}
		buf.Write(rest[:i+1])
		w.mid = false
		rest = rest[i+1:]
	}
	if _, err := w.dst.Write(buf.Bytes()); err != nil {
		return 0, err
	}
	return len(p), nil
}

// DefaultManualsDir は ix-manual skill が最初に探す場所と同じ: $IX_MANUALS → ~/.ix-toolkit/manuals。
func DefaultManualsDir() string {
	if d := os.Getenv("IX_MANUALS"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ix-toolkit", "manuals")
}

func missingFiles(paths []string) []string {
	var out []string
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			out = append(out, p)
		}
	}
	return out
}
