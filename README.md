# ix-toolkit

NEC IX を [Claude Code](https://claude.com/claude-code) から
運用するための skill 一式と、その参照マニュアルを作る PDF → Markdown 変換ツール。

```
skills/        Claude Code の skill
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

```console
$ git clone https://github.com/yuu61/ix-toolkit
$ cp -r ix-toolkit/skills/* ~/.claude/skills/
$ pip install netmiko paramiko
```

接続先はインベントリ `~/.claude/ix-devices.json` に定義する。**このリポジトリには含まれない。**

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

`host` は IP でも `~/.ssh/config` のエイリアスでもよく、`ProxyJump` の踏み台も自動で辿る。

```console
$ python ~/.claude/skills/ix-ssh.py --list      # 登録済み機器の一覧 (パスワードは表示しない)
```

**既定機器は無い。** `--device` を省略するとエラーになる。設定が意図しない機器へ流れ込む
事故を防ぐためで、skill 側でも機器名の推測を禁じている。

---

## pdfbook

```
pdfbook fetch   マニフェストに書いた PDF をまとめて取得する (SHA256 検証つき)
pdfbook probe   段組み・ヘッダ位置を自動較正してプロファイルを作る
pdfbook md      テキスト層のある PDF を構造つき Markdown に変換する
pdfbook scan    見開きスキャン画像を 1 ページずつに分割する (テキスト層が無い場合)
```

必要なもの: Go 1.25 以降（ビルド用）と、PATH に通った `pdftotext`
（[Xpdf](https://www.xpdfreader.com/) 4.x または poppler-utils）。

```console
$ go install github.com/yuu61/ix-toolkit/cmd/pdfbook@latest
```

### 使い方

`manifest.json` に PDF の取得元を書く（配布ページのリンクは版ごとに変わるので、
推測せず配布元を見て転記する）。

```console
$ pdfbook fetch -manifest manifest.json -out pdf/
$ pdfbook probe pdf/CRM-ver10.11-1.1.pdf -out profiles/nec-ix-crm.json
$ pdfbook md    pdf/CRM-ver10.11-1.1.pdf -profile profiles/nec-ix-crm.json \
                -out ~/.claude/ix-manuals/crm
```

`/ix-manual` は `~/.claude/ix-manuals/*/*.tsv` を探すので、そこへ出力すれば
そのまま引ける。852 ページで 8 秒ほど。

```
~/.claude/ix-manuals/crm/
├── commands.tsv      command / entry / file / line / pdfpage のタブ区切り索引
├── index.md          章・節の目次
├── README.md         生成条件と出典
└── ch03-インタフェース編/
    └── NGN.md        本文
```

- `line` は本文ファイル中の見出し行番号。そこから 30 行読めば 1 項目が収まる。
- `pdfpage` は元 PDF の物理ページ。版面に刷られた番号（`3-29`）は章ごとに振り直されて
  いて PDF ビューアにも `pdftotext -f` にも渡せないので、索引には入れていない。

**出力は無損失ではない。** 段組みの判定に許容を持たせてあるので、段間に掛かった数文字が
落ちるか二重になるページがある（852 ページ中 63 ページ）。

### 資料の割り方

`md` は資料の割り方を 2 つ持つ。**どちらで読むかを指す設定は無く、プロファイルが
項目の記号 (`entryMarker`) と見出し語 (`fieldLabels`) を持つかどうかで決まる。**

| プロファイル | 資料の型 | 項目の切れ目 | 索引 |
|---|---|---|---|
| `entryMarker` と `fieldLabels` がある | コマンド辞書 | `■` などの記号 | `commands.tsv` |
| どちらも無い | 解説書 | 階層番号の見出し (`2.11.6`) | `sections.tsv` |

**記号の設定を資料間で流用してはいけない。** `■` はコマンドリファレンスでは項目の頭だが、
機能説明書では節見出しの頭で、前者の設定で後者を読むと節見出しがそのまま偽のコマンドと
して索引に並ぶ。

### 機能説明書

```console
$ pdfbook probe pdf/FD-ver10.11-1.1.pdf -out profiles/nec-ix-fd.json
$ pdfbook md    pdf/FD-ver10.11-1.1.pdf -profile profiles/nec-ix-fd.json \
                -out ~/.claude/ix-manuals/fd
```

1208 ページで 7 秒ほど。索引は `sections.tsv`（`section` / `title` / `file` / `line` /
`pdfpage`）になり、コマンド名ではなく節番号と見出し語から引く。

**`probe` の出力は 3 箇所を手で直す必要がある。** `probe` は既定値をコマンドリファレンス用に
持っているため、図表の空白をガターと誤認して `columns` を `2` と判定し、`entryMarker` と
`fieldLabels` にもコマンド辞書用の値を残す。`columns` を `1` に、残る 2 つを空にする
（`profiles/nec-ix-fd.json` が較正済みの例）。

**図は変換しない。** 表・図・コンソール出力は版面どおりに ` ```text ` で囲い、直後に元 PDF
の該当ページへのリンクを置く。図のラベル（機器名・インタフェース名）は PDF のテキストとして
取れているので囲みの中に残るが、**矢印の向き・包含関係・順序は失われる。** 構成や流れを
答えるにはページそのものを見る必要がある。

### ページ画像

元 PDF へのリンクを辿れるのは PDF ビューアを開ける**人**だけで、`/ix-manual` を動かす
エージェントは `#page=1057` を辿れない。PNG なら `Read` で開けるので、ページを画像にして
置いておくと図が読めるようになる。

**pdfbook は画像を作らない。置いてあれば張り、無ければ張らない。** 焼くのは外部のレンダラ
（poppler-utils の `pdftoppm` か Xpdf の `pdftopng`）の仕事で、`pdftotext` だけを入れた
環境には**入っていない**。まず `pdftoppm -v` で確かめること。

```console
$ mkdir -p ~/.claude/ix-manuals/fd/figures
$ pdftoppm -png -r 150 pdf/FD-ver10.11-1.1.pdf ~/.claude/ix-manuals/fd/figures/p
$ pdfbook md pdf/FD-ver10.11-1.1.pdf -profile profiles/nec-ix-fd.json \
             -out ~/.claude/ix-manuals/fd
```

**順序が要る。** 変換時に `figures/` を見るので、焼く前に変換するとリンクは付かない。
焼いてから変換し直せば付く（変換は `figures/` を消さない）。囲みの直後がこうなる。

```
<sup>[元 PDF p1057](../../../FD-ver10.11-1.1.pdf#page=1057) / [ページ画像](../figures/p-1057.png)</sup>
```

ファイル名は `figures/p<ページ番号>.png`（`.jpg` も可）。レンダラが付けるゼロ詰めの名前
（`p-1057.png` / `p-001057.png`）でも、手で置いた `p1057.png` でも拾う。

**全ページ焼いてよい。** 焼き漏らしても、そのページが今までどおり PDF リンクだけになる
だけで壊れはしない。容量を惜しむなら `-f` / `-l` で区切る（全 1208 ページのうち図があるのは
361 ページほど）。

```console
$ pdftoppm -png -r 150 -f 1050 -l 1060 pdf/FD-ver10.11-1.1.pdf ~/.claude/ix-manuals/fd/figures/p
```

要るのは抽出ではなくページ描画で、`pdfimages` は図 1 枚が網掛けの小片にばらけるので使えない。
解像度は 150dpi で足りるはず（8pt の図中ラベルが読めるかは未検証）。ページ画像は本文テキスト
より直接的な複製物なので、`.gitignore` と同じく**手元限り**で扱う。

## ライセンス

MIT
