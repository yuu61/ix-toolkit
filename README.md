# ix-toolkit

NEC IX を Claude Codeから運用するための skill 一式と、その参照マニュアルを作る PDF / Web → Markdown 変換ツール。
IX2000/IX3000（無印）と IX-R/IX-V の 2 系列を扱う。
skill は SKILL.md 形式なので、Codexなど同じ形式を読むエージェントでもそのまま動く。

---

## skills

| skill | 用途 | 種別 |
|---|---|---|
| `ix-show` | show コマンドで状態確認 | 読み取り専用 |
| `ix-manual` | コマンドリファレンス・機能説明書を引く | 読み取り専用・機器に接続しない |
| `ix-backup` | running-config をファイルに退避 | 読み取り専用 |
| `ix-configure` | 設定を投入 | **破壊的**・実行前に確認必須 |
| `ix-save` | `write memory` で永続化 | **破壊的** |

### インストール (Claude Code)

[uv](https://docs.astral.sh/uv/) が PATH に通っていれば、これだけ。

```console
$ gh skill install yuu61/ix-toolkit --all --agent claude-code --scope user
```

skill が呼ぶ SSH クライアント `ix-ssh` は入らないが、PATH に無ければ skill が
`uvx --from git+https://github.com/yuu61/ix-toolkit ix-ssh` へ切り替えるので、初回に自動で揃う
(数秒。以降はキャッシュ)。毎回の解決を省くなら一度だけ:

```console
$ uv tool install git+https://github.com/yuu61/ix-toolkit
```

<details>
<summary>細かい話</summary>

- リポジトリは位置引数で、`--agent` はエージェント名を取る。`--agent yuu61/ix-toolkit` とは書けない。
- `--scope user` は `~/.claude/skills/<skill 名>/`、既定の `--scope project` はカレントリポジトリの
  `.claude/skills/`。skill が 1 つずつ独立するので名前空間は付かず `/ix-show` で引ける。
- 1 つだけなら `--all` の代わりに名前を渡す。`ix-manual` は機器に接続しないので単体で足りる。
- 版は「最新のタグ付きリリース → 既定ブランチの HEAD」の順。固定は `--pin <tag/SHA>`。
- 更新は skill が `gh skill update`、`ix-ssh` が `uv tool upgrade ix-toolkit`。
- `uvx` のフォールバックはタグを付けない限り HEAD を取る。設定を投入する `ix-configure` は
  `uv tool install` で固定したものを使うほうがよい。
- `uv` を置けない場合は venv に入れて PATH を通す
  (`pip install git+https://github.com/yuu61/ix-toolkit`)。システムの Python への直接 `pip install`
  は PEP 668 の `externally-managed-environment` で止まる。

</details>

#### ソースごと入れる場合 (Claude Code)

manualbook でマニュアルを自分で変換する、`ix-ssh` を手元で直す、といった用途はクローンする。

```console
$ git clone https://github.com/yuu61/ix-toolkit $HOME/.claude/skills/ix-toolkit
$ uv tool install -e $HOME/.claude/skills/ix-toolkit
```

- `.claude-plugin/plugin.json` があるので plugin として読まれ、`/ix-toolkit:ix-show` になる
  (衝突しなければ `/ix-show` でも引ける)。更新は `git pull`。
- `-e` は手元のソースを `ix-ssh` に使わせるため。省くと skill は GitHub の HEAD を取りに行き、
  クローン側の変更が効かない。
- 上の `gh skill install` と併用しない。同じ skill が二重に並ぶ。

### インストール (Codex)

クローン先を `~/.codex/skills/` の下にする。

```console
$ git clone https://github.com/yuu61/ix-toolkit $HOME/.codex/skills/ix-toolkit
$ uv tool install -e $HOME/.codex/skills/ix-toolkit
```

- Codex は skill ディレクトリを入れ子まで辿るので、クローンしたままの `skills/ix-*/SKILL.md` が
  5 つとも載る。`~/.agents/skills/` に置いても同じように読まれる。
- Claude Code はこの場所を読まない (`~/.claude/skills/` と plugin だけ)。両方で使うなら両方に置く。
- frontmatter の `argument-hint` / `allowed-tools` / `compatibility` / `license` は Claude Code
  向けで、Codex は `name` と `description` だけを読んで残りは無視する。
- `uv tool install -e` を省くと skill は `uvx --from git+...` に落ちる (手元の `ix-ssh` は使われない)。
- 更新は `git pull`。

### インストール (その他のエージェント)

SKILL.md を読むエージェントなら、`skills/ix-*/` をそのエージェントの skill ディレクトリへ
置けば動く。`ix-ssh` は PATH から呼ぶだけなので、入れ方は上と同じ
(`uv tool install git+https://github.com/yuu61/ix-toolkit`)。

### 接続先

インベントリ `~/.ix-toolkit/devices.json` を作って定義する。

```json
{
  "devices": {
    "home": {
      "host": "192.0.2.1",
      "username": "admin",
      "password": "...",
      "model": "IX2215",
      "note": "Core Router"
    }
  }
}
```

- `host` は IP でも `~/.ssh/config` のエイリアスでもよい。`ProxyJump` の踏み台も自動で辿る。
- `model`, `note`は任意。ただし `model` は skill がどの系列のマニュアルを引くかを決める手がかりになる
  （`IX-R…` / `IX-V…` なら IX-R/IX-V、`IX2…` / `IX3…` なら無印）。無くても止まらず、エージェントが
  会話から判断するかユーザーに尋ねる。
- 置き場所は `$IX_INVENTORY` → `~/.ix-toolkit/devices.json` → `~/.claude/ix-devices.json` の順に
  探す。実際に読んだファイルは `ix-ssh --list` の 1 行目に出る。
- 既定機器は無く、`--device` を省略するとエラーになる。意図しない機器へ設定が流れ込む事故を防ぐ
  ためで、skill 側でも機器名の推測を禁じている。

手で確かめるなら skill を通さず直接叩く。

```console
$ ix-ssh --list                                                     # uv tool install 済み
$ uvx --from git+https://github.com/yuu61/ix-toolkit ix-ssh --list   # 入れていない場合
```

---

## manualbook

NEC のマニュアルを取得して、`ix-manual` が引く Markdown と索引に変換するツール。

```console
$ go build -ldflags="-s -w" -o manualbook ./cmd/manualbook
$ ./manualbook build
```

windows Defender の`Trojan:Win32/Bearfoos.A!ml` の誤検知に引っ掛かるため、`-ldflags="-s -w"` は必須

`build` は `manifest.json` を読んで、取得 → 変換 → 系列間の差分表まで 1 回で作る。指定はすべて
`manifest.json` にあるので、フラグは要らない。

1. 無印 (PDF) の 2 冊は配布ページの URL が版ごとに変わるので `manifest.json` の `url` が空のまま。
   手元にある PDF を `pdf/<name>.pdf`（`name` は `manifest.json` の値。`pdf/CRM-ver10.11-1.1.pdf`）に
   置いておく。無ければその 2 冊だけ失敗して、残りは作られる。
2. `manualbook build`。IX-R/IX-V (Web) はサイトから取る（1 ページ 1 秒の間隔で、2 冊で 8 分ほど）。
   Web の取得は裏で先に始め、その間に PDF の 2 冊を変換する（機能説明書はページ画像も焼く）ので、
   初回の所要時間はこの 8 分とほぼ同じ（実測 500 秒）。変換だけなら 4 冊で 20 秒ほど。
3. `~/.ix-toolkit/manuals/<系列>/<冊子>/` に本文と索引、`~/.ix-toolkit/manuals/ix-r/diff.tsv` に
   系列間のコマンド対応表ができる。

- 取得済みの資料は取りに行かない（PDF は実体の有無、Web はキャッシュに残る版で判定）。2 回目以降は
  変換だけなので 20 秒で終わる。
- PDF の機能説明書はページ画像も焼く（404 MB。初回は Web の取得を待つ間に焼くので時間は増えず、
  焼いてあれば飛ばす）。図の向きや構成をエージェントに見せるのに要る（[後述](#図とページ画像-pdf)）。
- 定期的に取りに行く仕組みは無い。NEC の更新情報を見て版が上がっていたら、`manifest.json` の
  `version` / `url`（Web は `name` も）を直して `build` を流す（Web は版が違うと取得せずに止まる）。
  同じ版を取り直すなら `-force`（Web は全ページ落とし直すので初回と同じ 8 分ほど掛かる）。
- 置き場を変えるなら `-manuals <dir>`（`$IX_MANUALS` があればそれが既定）。1 冊だけなら `-only <name>`。

### 資料と置き場所

| 系列 | 機種 | 資料 | `manifest.json` の `kind` |
|---|---|---|---|
| `ix` | IX2000/IX3000 | PDF（CRM / FD Ver 10.11） | `pdf` |
| `ix-r` | IX-R/IX-V | Web（Sphinx で組まれた HTML、Ver 1.5a） | `web` |

冊子は `manifest.json` の `book` で、`crm`（コマンドリファレンス）と `fd`（機能説明書）の 2 種類。
変換結果は `~/.ix-toolkit/manuals/<系列>/<冊子>/` に置く。`ix-manual` は `$IX_MANUALS` →
`~/.ix-toolkit/manuals/` → `~/.claude/ix-manuals/` の順に探し、その下を `<系列>/<冊子>/` として読む。

```
~/.ix-toolkit/manuals/
├── ix/
│   ├── crm/            commands.tsv  index.md  README.md  ch03-インタフェース編/NGN.md …
│   └── fd/             sections.tsv  index.md  README.md  figures/  ch02-ルータの設定/…
└── ix-r/
    ├── crm/            commands.tsv  …
    ├── fd/             sections.tsv  figures/*.svg  …
    └── diff.tsv        無印 → IX-R のコマンド対応表
```

どちらの系列・冊子も索引は同じ形で、

- `commands.tsv` は `command / entry / file / line / source`、`sections.tsv` は
  `section / title / file / line / source`。
- `line` は本文ファイル中の見出し行番号。そこから 30 行読めば 1 項目が収まる。
- `source` は元資料上の位置。PDF 由来なら物理ページ `p1057`（PDF ビューアの `#page=` にそのまま
  渡せる）、Web 由来なら元のページと節のアンカー `cli/interface/cli_ngn.html#ngn-ip-enable`
  （冊子の `README.md` にある URL に続ければ開く）。
- 見出し語は両系列で同じ綴りに揃えてある（無印 PDF の「ユーザ権限」は「ユーザー権限」で出る）。

### 変換の中身

#### 無印 (PDF)

- テキスト層を版面どおりに組み直す（OCR 不使用）。CRM は 852 ページで 9 秒ほど、FD は 1208 ページで
  10 秒ほど。
- 出力は無損失ではない。段間に掛かった数文字が落ちるページがある
  （2 段として読む 629 ページ中 50 ページ・計 236 文字）。
- 表・図・コンソール出力は版面どおりに ` ```text ` で囲い、直後に元 PDF の該当ページへのリンクを置く。

#### IX-R/IX-V (Web)

- 取得は 1 ページずつ間隔を置き、`searchindex.js` にあるページだけを取る（サイトを這わない）。
  索引ページの題に `version` が含まれるかで版を確かめてから取る。
- サイトはブラウザ以外の User-Agent に 403 を返すので、既定でブラウザの UA を名乗る
  （`fetch -user-agent` で変えられる）。
- 表は Markdown の表になる（結合セルは覆う範囲に値を繰り返す）。図は SVG のまま `figures/` に
  置き、本文には図中のラベルを ` ```text ` で並べたうえで `[図]` リンクを付ける。
- 各ブロックの直後に `[出典](https://…#…)` が付く。

#### 系列間の差分 (diff.tsv)

`kind / ix / ix-r / source / ref_ix / ref_ixr / note` のタブ区切り。行の出どころが `source` 列で分かる。

- `ch8` — IX-R 機能説明書 8 章「IXシリーズとの差分」の表の書き起こし（一次情報）。`kind` は
  `removed` / `renamed` / `moved` / `range` / `changed`。
- `derived` — 両系列の制限事項を読み比べて導いた差（`limit`）。`profiles/ix-r-derived-diff.tsv` に
  手で書いてあり、ファイル頭に前提の版の組が書いてある。版が上がったら読み直す。
- `setdiff` — 両索引の集合差から機械的に出した、族ごと無いコマンド（`absent`、`ip pim *` など）。目安。

### 図とページ画像 (PDF)

- PDF の図は変換しない。図のラベル（機器名・インタフェース名）はテキストとして囲みの中に残るが、
  矢印の向き・包含関係・順序は失われる。構成や流れを答えるにはページそのものを見るしかない。
- 囲みの直後の PDF リンクを辿れるのは PDF ビューアを開ける人だけ。`ix-manual` を動かすエージェントは
  `#page=1057` を辿れないが、PNG なら `Read` で開ける。
- `build` が機能説明書の全ページを PNG に焼き、囲みの直後に `[ページ画像]` を付ける。
  変換と同じ PDFium が描くので、別の道具は要らない。コマンドリファレンスと Web 由来の冊子は
  焼かない（囲みとページリンクを出すのが機能説明書の側だけ。Web は図が SVG で残る）。
- 焼いてあれば飛ばす（`figures/.manualbook.json` に残る元 PDF の名前と dpi で判定。版が上がって
  PDF が入れ替われば焼き直す）。焼き直したいときは `figures/` を消して `build` を流す（+72 秒）。
  `-force` は取得の話で、ここには効かない。

```
<sup>[元 PDF p1057](<変換時の PDF の場所への相対パス>/FD-ver10.11-1.1.pdf#page=1057) / [ページ画像](../figures/p1057.png)</sup>
```

- 全ページ焼いてよい。焼き漏らしても、そのページが今までどおり PDF リンクだけになるだけで壊れ
  はしない。FD 全 1208 ページで 72 秒・404 MB（150dpi の PNG が 1 ページ平均 340 KB）。
- ファイル名は `figures/p<ページ番号>.png`。外の道具で焼いたゼロ詰めの名前
  (`p-1057.png` / `p-001057.png`) や手で置いた `.jpg` も拾う。
- ページ画像は本文テキストより直接的な複製物なので、`.gitignore` と同じく手元限りで扱う。

### 1 冊ずつ手で流す

`build` がしていることは、次のサブコマンドを順に流すのと同じ（違いは Web の取得を裏で先に
始めることだけ）。プロファイルを較正し直す、ページ画像を一部だけ焼く、といったときはこちらを使う。

```
manualbook fetch   マニフェストに書いた資料をまとめて取得する (PDF / Web)
manualbook probe   段組み・ヘッダ位置を自動較正してプロファイルを作る (PDF)
manualbook md      PDF か Web の取得キャッシュを構造つき Markdown に変換する
manualbook diff    無印と IX-R の変換結果から系列間のコマンド対応表 diff.tsv を作る
manualbook figures ページを PNG に焼く (PDF の図のページを画像で引けるようにする)
manualbook scan    見開きスキャン画像を 1 ページずつに分割する (テキスト層が無い場合)
```

```console
$ manualbook fetch -manifest manifest.json -out pdf/
$ manualbook md pdf/CRM-ver10.11-1.1.pdf -profile profiles/nec-ix-crm.json -series ix -version 10.11-1.1 -out ~/.ix-toolkit/manuals/ix/crm
$ manualbook md pdf/FD-ver10.11-1.1.pdf  -profile profiles/nec-ix-fd.json  -series ix -version 10.11-1.1 -out ~/.ix-toolkit/manuals/ix/fd -figures
$ manualbook md pdf/IX-R-CRM-1.5a -out ~/.ix-toolkit/manuals/ix-r/crm
$ manualbook md pdf/IX-R-FD-1.5a  -out ~/.ix-toolkit/manuals/ix-r/fd
$ manualbook diff ~/.ix-toolkit/manuals/ix ~/.ix-toolkit/manuals/ix-r
```

- `fetch` は取得済みの Web も `ETag` / `Last-Modified` で 1 ページずつ問い合わせ、変わったページだけ
  取り直す（同じ版のまま直されたページを拾う軽い更新の口。全ページ問い合わせるので数分掛かる）。
- Web の `md` は題・版・系列・プロファイルをキャッシュ内の `.manualbook.json` から読むので、フラグは
  `-out` だけ。PDF は `-series` / `-version` を渡す（変換結果の `README.md` に載り、`ix-manual` が
  回答に系列と版を添えるために読む）。
- `diff` の `derived` 行はリポジトリ直下で流せば `profiles/ix-r-derived-diff.tsv` を自動で読む。外から
  流すなら `-derived <リポジトリ>/profiles/ix-r-derived-diff.tsv` を付ける（無いと警告して 0 行になる）。
- ページ画像をあとから焼き足すなら `manualbook figures <pdf> -out ~/.ix-toolkit/manuals/ix/fd` を流し、
  `md` をもう一度流す。囲みに `[ページ画像]` を付けるかは変換時に `figures/` を見て決めるので、
  この順序が要る（変換は `figures/` を消さない）。区切るなら `md -figure-pages 1050-1060`
  （`figures` サブコマンドでは `-pages`）。解像度は 150dpi でよい (`-figure-dpi`)。100dpi でも
  読めるが線が痩せる。

## ライセンス

MIT。バイナリに含まれる [PDFium](https://pdfium.googlesource.com/pdfium/) は Apache-2.0。
