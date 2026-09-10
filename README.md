# ix-toolkit

NEC IX を Claude Codeから運用するための skill 一式と、その参照マニュアルを作る PDF → Markdown 変換ツール。
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

pdfbook でマニュアルを自分で変換する、`ix-ssh` を手元で直す、といった用途はクローンする。

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
- `model`, `note`は任意。
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

## pdfbook

```
pdfbook fetch   マニフェストに書いた PDF をまとめて取得する
pdfbook probe   段組み・ヘッダ位置を自動較正してプロファイルを作る
pdfbook md      テキスト層のある PDF を構造つき Markdown に変換する
pdfbook figures ページを PNG に焼く (図のページを画像で引けるようにする)
pdfbook scan    見開きスキャン画像を 1 ページずつに分割する (テキスト層が無い場合)
```

```console
$ go build -ldflags="-s -w" -o pdfbook ./cmd/pdfbook
```

windows Defender の`Trojan:Win32/Bearfoos.A!ml` の誤検知に引っ掛かるため、`-ldflags="-s -w"` は必須

### 使い方

1. `manifest.json` の `url` を配布ページから転記する。
2. `fetch` で PDF を取る。`sha256` で検証される。
3. 資料ごとに `md` で変換する。プロファイルは同梱のものを使う。

```console
$ pdfbook fetch -manifest manifest.json -out pdf/
```

出力先は `~/.ix-toolkit/manuals/` の下にする。`ix-manual` は `$IX_MANUALS` →
`~/.ix-toolkit/manuals/` → `~/.ix-toolkit/manuals/` の順に探すので、そこへ出せばそのまま引ける。
どちらの資料も索引は同じ形で、

- `line` は本文ファイル中の見出し行番号。そこから 30 行読めば 1 項目が収まる。
- `pdfpage` は元 PDF の物理ページ。PDF ビューアの `#page=` にそのまま渡せる。
- 出力は無損失ではない。段間に掛かった数文字が落ちるページがある
  (2 段として読む 629 ページ中 50 ページ・計 236 文字)。

#### コマンドリファレンス CRM

```console
$ pdfbook md pdf/CRM-ver10.11-1.1.pdf -profile profiles/nec-ix-crm.json -out ~/.ix-toolkit/manuals/crm
```

852 ページで 9 秒ほど。索引は `commands.tsv` (`command` / `entry` / `file` / `line` /
`pdfpage`) で、コマンド名から引く。

```
~/.ix-toolkit/manuals/crm/
├── commands.tsv      command / entry / file / line / pdfpage のタブ区切り索引
├── index.md          章・節の目次
├── README.md         生成条件と出典
└── ch03-インタフェース編/
    └── NGN.md        本文
```

#### 機能説明書 FD

```console
$ pdfbook md pdf/FD-ver10.11-1.1.pdf -profile profiles/nec-ix-fd.json -out ~/.ix-toolkit/manuals/fd
```

1208 ページで 10 秒ほど。索引は `sections.tsv` (`section` / `title` / `file` / `line` /
`pdfpage`) で、コマンド名ではなく節番号と見出し語から引く。

### 図とページ画像

- 図は変換しない。表・図・コンソール出力は版面どおりに ` ```text ` で囲い、直後に元 PDF の該当
  ページへのリンクを置く。
- 図のラベル（機器名・インタフェース名）はテキストとして囲みの中に残るが、矢印の向き・包含関係・
  順序は失われる。構成や流れを答えるにはページそのものを見るしかない。
- その PDF リンクを辿れるのは PDF ビューアを開ける人だけ。`ix-manual` を動かすエージェントは
  `#page=1057` を辿れないが、PNG なら `Read` で開ける。
- ページ画像は `md -figures` で一緒に焼ける。変換と同じ PDFium が描くので、別の道具は要らない。
  効くのは機能説明書だけ。コマンドリファレンスに付けると、焼く前に断られる (囲みとページ
  リンクを出すのが機能説明書の側だけのため)。

```console
$ pdfbook md pdf/FD-ver10.11-1.1.pdf -profile profiles/nec-ix-fd.json -out ~/.ix-toolkit/manuals/fd -figures
```

囲みの直後がこうなる。

```
<sup>[元 PDF p1057](../../../FD-ver10.11-1.1.pdf#page=1057) / [ページ画像](../figures/p1057.png)</sup>
```

- あとから焼き足すなら `pdfbook figures <pdf> -out ~/.ix-toolkit/manuals/fd` を流し、`md` を
  もう一度流す。囲みに `[ページ画像]` を付けるかは変換時に `figures/` を見て決めるので、
  この順序が要る（変換は `figures/` を消さない）。
- 全ページ焼いてよい。焼き漏らしても、そのページが今までどおり PDF リンクだけになるだけで壊れ
  はしない。FD 全 1208 ページで 72 秒・404 MB（150dpi の PNG が 1 ページ平均 340 KB）。区切る
  なら `-figure-pages 1050-1060`（`figures` サブコマンドでは `-pages`）。
- 解像度は 150dpi でよい (`-figure-dpi`)。100dpi でも読めるが線が痩せる。
- ファイル名は `figures/p<ページ番号>.png`。外の道具で焼いたゼロ詰めの名前
  (`p-1057.png` / `p-001057.png`) や手で置いた `.jpg` も拾う。
- ページ画像は本文テキストより直接的な複製物なので、`.gitignore` と同じく手元限りで扱う。

## ライセンス

MIT。バイナリに含まれる [PDFium](https://pdfium.googlesource.com/pdfium/) は Apache-2.0。
