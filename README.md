# ix-toolkit

NEC IX ルータを [Claude Code](https://claude.com/claude-code) から
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
| `/ix-manual` | コマンドリファレンスを引く | 読み取り専用・機器に接続しない |
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

`md` は資料の割り方を 2 つ持つ。**どちらで読むかを指す設定は無く、プロファイルが
項目の記号 (`entryMarker`) と見出し語 (`fieldLabels`) を持つかどうかで決まる。**

| プロファイル | 資料の型 | 項目の切れ目 | 索引 |
|---|---|---|---|
| `entryMarker` と `fieldLabels` がある | コマンド辞書 | `■` などの記号 | `commands.tsv` |
| どちらも無い | 解説書 | 階層番号の見出し (`2.11.6`) | `sections.tsv` |

選択肢ではなく前提条件である。項目の記号が無ければ項目を 1 つも開始できず、
見出し語が無ければ項目の中身を割れないので、その資料はコマンド辞書として読めない。

記号の設定を資料間で流用してはいけない。`■` はコマンドリファレンスでは項目の頭
(2039 個) だが、機能説明書では節見出しの頭 (353 個) で、前者の設定で後者を読むと
節見出しがそのまま偽のコマンドとして索引に並ぶ。

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

`/ix-manual` は `~/.claude/ix-manuals/*/commands.tsv` を探すので、そこへ出力すれば
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

**表は `pdftotext -table` で読む。** これは体裁ではなく正しさの問題で、既定の `-layout`
だと諸元表の列が黙ってずれる。実測（1-7 の諸元表）では `VLAN 設定数` の行が
`32/32/32/32/32/1000※1/32/32` ではなく空欄と `8` を含む別の並びになり、機種との対応が
全部崩れていた。抽出は成功しているように見えるので、**出力を見てもこの崩れは分からない。**

**図は変換しない。** 表・図・コンソール出力は版面どおりに ` ```text ` で囲い、直後に元 PDF
の該当ページへのリンクを置く。図のラベル（機器名・インタフェース名）はラスタに焼き込まれて
おらず PDF のテキストとして取れているので囲みの中に残るが、**矢印の向き・包含関係・順序は
失われる。** 構成や流れを答えるにはリンク先の版面を人が見る必要がある。

変換時にモデルで図の説明を書かせる案は採らなかった。ビルドに非決定性が入って利用者ごとに
違うマニュアルが出来上がり、何より生成文が本文と区別できなくなる。参照マニュアルとしては、
それは崩れた表と同じ種類の事故になる。

> 将来この判断を見直して図を画像として出す場合、**`pdfimages` は誤った道具**である。
> ラベルはラスタの上に載った PDF テキストなので、画像 XObject を抽出するとラベルの無い
> 作図だけが取れる。必要なのは抽出ではなくページ描画（`pdftoppm`）。

**出力は無損失ではない。** 段組みの判定に許容を持たせてあるので、段間に掛かった数文字が
落ちるか二重になるページがある（852 ページ中 63 ページ）。

## ライセンス

MIT
