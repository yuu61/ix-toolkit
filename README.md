# ix-toolkit

NEC IX を [Claude Code](https://claude.com/claude-code) から
運用するための skill 一式と、その参照マニュアルを作る PDF → Markdown 変換ツール。

```
.claude-plugin/ plugin manifest
skills/        Claude Code の skill
scripts/       skill が呼ぶ NEC IX 用 SSH クライアント
cmd/pdfbook/   PDF のマニュアルを Markdown に変換する
profiles/      pdfbook の変換プロファイル
```

---

## skills

| skill | 用途 | 種別 |
|---|---|---|
| `/ix-show` | show コマンドで状態確認 | 読み取り専用 |
| `/ix-manual` | コマンドリファレンス・機能説明書を引く | 読み取り専用・機器に接続しない |
| `/ix-backup` | running-config をファイルに退避 | 読み取り専用 |
| `/ix-configure` | 設定を投入 | **破壊的**・実行前に確認必須 |
| `/ix-save` | `write memory` で永続化 | **破壊的** |

### インストール

`~/.claude/skills/` の下へ clone する。

```console
$ git clone https://github.com/yuu61/ix-toolkit $HOME/.claude/skills/ix-toolkit
```

- `uv` が PATH に通っていること。
- Python 3.10 以上と依存は初回実行時に揃う。skill は `scripts/ix-ssh.py` 先頭の PEP 723 メタデータ
  (`requires-python` と netmiko / paramiko) を [uv](https://docs.astral.sh/uv/) の
  `uv run --script` で呼ぶ (2 回目以降はキャッシュが効き、素の `python` との差は 0.2 秒ほど)。
- skill には名前空間が付いて `/ix-toolkit:ix-show` になる (衝突しなければ `/ix-show` でも引ける)。
- 更新は `git pull`。止めるのは `claude plugin disable ix-toolkit@skills-dir` かディレクトリ削除。

#### uv を置かない場合

venv を作る。

```console
$ python3 -m venv $HOME/.venvs/ix-toolkit
$ $HOME/.venvs/ix-toolkit/bin/pip install "netmiko>=4.7" "paramiko>=3.0"
```

- SKILL.md の `uv run --script` をその venv の python に書き換える (そのままだと
  `uv: command not found` で止まる)。`allowed-tools` は `python` / `python3` も許可してある。
- システムの Python へ直接 `pip install` する手は使えない。Debian / Ubuntu / Fedora や Homebrew の
  Python は PEP 668 で外部管理と宣言されていて、`error: externally-managed-environment` で止まる。

### 接続先

インベントリ `~/.claude/ix-devices.json` を作って定義する。

```json
{
  "devices": {
    "home": {
      "host": "192.0.2.1",
      "username": "admin",
      "password": "...",
      "note": "IX2215 / WAN は GigaEthernet0.0"
    }
  }
}
```

- `host` は IP でも `~/.ssh/config` のエイリアスでもよい。`ProxyJump` の踏み台も自動で辿る。
- 既定機器は無く、`--device` を省略するとエラーになる。意図しない機器へ設定が流れ込む事故を防ぐ
  ためで、skill 側でも機器名の推測を禁じている。

```console
$ uv run --script $HOME/.claude/skills/ix-toolkit/scripts/ix-ssh.py --list   # 登録済み機器の一覧
```

---

## pdfbook

```
pdfbook fetch   マニフェストに書いた PDF をまとめて取得する (SHA256 検証つき)
pdfbook probe   段組み・ヘッダ位置を自動較正してプロファイルを作る
pdfbook md      テキスト層のある PDF を構造つき Markdown に変換する
pdfbook scan    見開きスキャン画像を 1 ページずつに分割する (テキスト層が無い場合)
```

必要なもの:

- Go 1.25 以降（ビルド用）
- PATH に通った [Xpdf](https://www.xpdfreader.com/) 4.x の `pdftotext`。**poppler-utils の同名
  コマンドでは動かない**（`-table` も `-marginl/r/t/b` も持たないため、`exit 99` と usage が出る）

```console
$ go install github.com/yuu61/ix-toolkit/cmd/pdfbook@latest
```

### 使い方

1. `manifest.json` の `url` を配布ページから転記する。
2. `fetch` で PDF を取る。`sha256` で検証される。
3. 資料ごとに `md` で変換する。プロファイルは同梱のものを使う。

```console
$ pdfbook fetch -manifest manifest.json -out pdf/
```

出力先は `~/.claude/ix-manuals/` の下にする。`/ix-manual` は `~/.claude/ix-manuals/*/*.tsv` を
探すので、そこへ出せばそのまま引ける。どちらの資料も索引は同じ形で、

- `line` は本文ファイル中の見出し行番号。そこから 30 行読めば 1 項目が収まる。
- `pdfpage` は元 PDF の物理ページ。PDF ビューアにも `pdftotext -f` にもそのまま渡せる。
- 出力は無損失ではない。段間に掛かった数文字が落ちるか二重になるページがある
  (852 ページ中 63 ページ)。

#### コマンドリファレンス

```console
$ pdfbook md pdf/CRM-ver10.11-1.1.pdf -profile profiles/nec-ix-crm.json -out ~/.claude/ix-manuals/crm
```

852 ページで 8 秒ほど。索引は `commands.tsv` (`command` / `entry` / `file` / `line` /
`pdfpage`) で、コマンド名から引く。

```
~/.claude/ix-manuals/crm/
├── commands.tsv      command / entry / file / line / pdfpage のタブ区切り索引
├── index.md          章・節の目次
├── README.md         生成条件と出典
└── ch03-インタフェース編/
    └── NGN.md        本文
```

#### 機能説明書

```console
$ pdfbook md pdf/FD-ver10.11-1.1.pdf -profile profiles/nec-ix-fd.json -out ~/.claude/ix-manuals/fd
```

1208 ページで 7 秒ほど。索引は `sections.tsv` (`section` / `title` / `file` / `line` /
`pdfpage`) で、コマンド名ではなく節番号と見出し語から引く。

### 図とページ画像

- 図は変換しない。表・図・コンソール出力は版面どおりに ` ```text ` で囲い、直後に元 PDF の該当
  ページへのリンクを置く。
- 図のラベル（機器名・インタフェース名）はテキストとして囲みの中に残るが、矢印の向き・包含関係・
  順序は失われる。構成や流れを答えるにはページそのものを見るしかない。
- その PDF リンクを辿れるのは PDF ビューアを開ける人だけ。`/ix-manual` を動かすエージェントは
  `#page=1057` を辿れないが、PNG なら `Read` で開ける。
- pdfbook は `figures/` に置いてある画像を張る。焼くのは別の手順。

焼く手順:

1. レンダラを `bin/` に用意する（一度だけ。ビルドに Go 1.26 以降が要る）。
2. 画像を焼く。
3. 焼いてから変換し直す。変換時に `figures/` を見るので、焼く前に変換するとリンクは付かない
   (変換は `figures/` を消さないので、焼いてから変換し直せば付く)。

```console
$ go -C tools build -o ../bin/ github.com/klippa-app/pdfium-cli   # 一度だけ・ルートで
$ mkdir -p ~/.claude/ix-manuals/fd/figures
$ bin/pdfium-cli render pdf/FD-ver10.11-1.1.pdf \
      ~/.claude/ix-manuals/fd/figures/p%d.png --file-type png --dpi 150
$ pdfbook md pdf/FD-ver10.11-1.1.pdf -profile profiles/nec-ix-fd.json \
             -out ~/.claude/ix-manuals/fd
```

囲みの直後がこうなる。

```
<sup>[元 PDF p1057](../../../FD-ver10.11-1.1.pdf#page=1057) / [ページ画像](../figures/p-1057.png)</sup>
```

- ファイル名は `figures/p<ページ番号>.png`（`.jpg` も可）。レンダラが付けるゼロ詰めの名前
  (`p-1057.png` / `p-001057.png`) でも、手で置いた `p1057.png` でも拾う。
- 全ページ焼いてよい。焼き漏らしても、そのページが今までどおり PDF リンクだけになるだけで壊れ
  はしない。FD 全 1208 ページで 63 秒・404 MB（150dpi の PNG が 1 ページ平均 340 KB）。区切る
  なら `--pages 1050-1060`。
- 要るのはページ描画。`pdfimages` のような画像の抽出では図にならない。
- 解像度は 150dpi でよい。100dpi でも読めるが線が痩せる。
- ページ画像は本文テキストより直接的な複製物なので、`.gitignore` と同じく手元限りで扱う。

```console
$ bin/pdfium-cli render pdf/FD-ver10.11-1.1.pdf \
      ~/.claude/ix-manuals/fd/figures/p%d.png --file-type png --dpi 150 --pages 1050-1060
```

## ライセンス

MIT
