package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// build サブコマンド: マニフェストの資料を取得し、変換し、系列間の差分表まで作る。
//
// fetch → md → diff を資料ごとにフラグを並べて流すのは手順書を写す作業でしかなく、
// その指定 (系列・冊子・版・プロファイル) はすべてマニフェストに書いてある。
// build はマニフェストを唯一の入力にして、手元に何も無い状態から ix-manual が
// 読む形 (<manuals>/<系列>/<冊子>/ と <manuals>/ix-r/diff.tsv) まで 1 回で作る。
//
// 資料ごとに独立して進め、1 冊が取れなくても残りは作る (PDF は url を書けない
// ことがあり、Web だけ先に揃えたい)。diff は両系列が揃ったときだけ作る。
// 取得済みの資料は取りに行かない。取り直したいときだけ -force を付ける。

func runBuild(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	manifestPath := fs.String("manifest", "manifest.json", "マニフェスト JSON (profile 等の相対パスはこのファイルの場所から解く)")
	cacheDir := fs.String("cache", "pdf", "取得キャッシュの置き場 (fetch の -out と同じ)")
	manualsDir := fs.String("manuals", defaultManualsDir(), "変換結果の置き場。この下に <系列>/<冊子>/ を作る ($IX_MANUALS があればそれ)")
	figures := fs.Bool("figures", false, "PDF の機能説明書はページ画像も焼く (全ページで 72 秒・404 MB)")
	force := fs.Bool("force", false, "取得済みの資料も取り直す")
	only := fs.String("only", "", "この name の資料だけ取得・変換する (diff は揃っていれば作る)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "使い方: manualbook build [-figures] [-force] [-only <name>]")
		fmt.Fprintln(os.Stderr, "  manifest.json の資料を pdf/ に取り、~/.ix-toolkit/manuals/<系列>/<冊子>/ に変換し、diff.tsv を作る。")
		fs.PrintDefaults()
	}
	parseFlags(fs, args)

	m, err := readManifest(*manifestPath)
	if err != nil {
		fatal(err)
	}
	if *manualsDir == "" {
		fatal(fmt.Errorf("変換結果の置き場が決まりません。-manuals で指定してください"))
	}
	if err := os.MkdirAll(*cacheDir, 0o755); err != nil {
		fatal(err)
	}
	// マニフェストの中の相対パス (profile、手で導いた差分) はマニフェストの場所から解く。
	// リポジトリ直下で流すのと、外から -manifest で指すのとで同じ意味になる。
	manifestDir := filepath.Dir(*manifestPath)
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(manifestDir, p)
	}

	var docs []Doc
	for _, d := range m.Docs {
		if *only == "" || d.Name == *only {
			docs = append(docs, d)
		}
	}
	if len(docs) == 0 {
		fatal(fmt.Errorf("マニフェストに name %q の資料がありません", *only))
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	failed := 0
	for i, d := range docs {
		fmt.Printf("[%d/%d] %s (%s)", i+1, len(docs), d.Name, d.Kind)
		err := checkDoc(d)
		if err == nil {
			outDir := filepath.Join(*manualsDir, d.Series, d.Book)
			fmt.Printf(" → %s\n", outDir)
			err = buildDoc(client, d, *cacheDir, outDir, resolve(d.Profile), *figures, *force)
		} else {
			fmt.Println()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %s\n", d.Name, err)
			failed++
		}
		fmt.Println()
	}

	// 系列間の差分表。無印と IX-R の両方が揃ったときだけ作る。
	ixDir, ixrDir := filepath.Join(*manualsDir, "ix"), filepath.Join(*manualsDir, "ix-r")
	if missing := missingFiles(diffInputs(ixDir, ixrDir)); len(missing) == 0 {
		fmt.Println("diff.tsv (無印 → IX-R のコマンド対応表)")
		derived := filepath.Join(manifestDir, "profiles", "ix-r-derived-diff.tsv")
		if _, err := os.Stat(derived); err != nil {
			derived = "" // writeDiff がカレントと実行ファイルの隣を探し、無ければ警告する
		}
		if err := writeDiff(ixDir, ixrDir, "", derived); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ diff.tsv: %s\n", err)
			failed++
		}
	} else {
		fmt.Printf("diff.tsv は作らない (両系列が揃っていない: %s が無い)\n", missing[0])
	}

	fmt.Printf("\n完了: %d 件中 %d 件 → %s\n", len(docs), len(docs)-failed, *manualsDir)
	if failed > 0 {
		os.Exit(1)
	}
}

// checkDoc は置き場 <系列>/<冊子>/ を決めるのに要る欄が揃っているかを見る。
// 冊子名は diff と ix-manual skill が crm / fd で読むので、それ以外は通さない。
func checkDoc(d Doc) error {
	switch d.Book {
	case "crm", "fd":
	case "":
		return fmt.Errorf(`book が無い。"crm" (コマンドリファレンス) か "fd" (機能説明書) を書く`)
	default:
		return fmt.Errorf(`book %q は知らない ("crm" か "fd")`, d.Book)
	}
	if d.Series == "" {
		return fmt.Errorf(`series が無い。"ix" (IX2000/IX3000) か "ix-r" (IX-R/IX-V) を書く`)
	}
	return nil
}

// buildDoc は資料 1 件を取得して変換する。
func buildDoc(client *http.Client, d Doc, cacheDir, outDir, profilePath string, figures, force bool) error {
	// 取得。PDF は実体の有無、Web は取り切った印 (.manualbook.json の版) で取得済みを決める。
	if d.Kind == "web" && !force && webFetched(d.cachePath(cacheDir), d) {
		fmt.Printf("  = %s (取得済み)\n", d.Name)
	} else if err := fetchDoc(client, d, cacheDir, force, time.Second, defaultUserAgent); err != nil {
		return err
	}

	// ページ画像は PDF の機能説明書にだけ効く (md の側で断られる条件と同じ)。
	p := DefaultProfile()
	if profilePath != "" {
		var err error
		if p, err = LoadProfile(profilePath); err != nil {
			return err
		}
	}
	o := mdOptions{
		input: d.cachePath(cacheDir), outDir: outDir, profilePath: profilePath,
		title: d.Title, series: d.Series, version: d.Version,
		figures: figures && d.Kind == "pdf" && !p.HasCommandEntries(), figureDPI: 150,
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	return convert(o)
}

// defaultManualsDir は ix-manual skill が最初に探す場所と同じ: $IX_MANUALS → ~/.ix-toolkit/manuals。
func defaultManualsDir() string {
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
