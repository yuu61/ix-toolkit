# ix-toolkit

NEC IX ルータ（IX3315 / IX2215 等）を [Claude Code](https://claude.com/claude-code) から
運用するための skill 一式と、その参照マニュアルを作る PDF → Markdown 変換ツール。

```
skills/        Claude Code の skill
cmd/pdfbook/   PDF のマニュアルを Markdown に変換する (NEC IX には依存しない)
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

**出力は無損失ではない。** 段組みの判定に許容を持たせてあるので、段間に掛かった数文字が
落ちるか二重になるページがある（852 ページ中 63 ページ）。

### 他の資料に使う

プロファイルの `entryMarker` と `fieldLabels` をその資料の書式に合わせる。
段組み・ページ寸法・柱の位置は `probe` が自動で決める。

```json
{
  "entryMarker": "■",
  "fieldLabels": ["入力形式", "パラメータ", "説明", "..."]
}
```

`probe` がテキスト層をほとんど検出できなければ紙スキャン由来の画像 PDF なので、`md` は
使えない。`pdfbook scan` で見開きを分割して OCR に回す経路になる。

---

## 公開しないもの

`.gitignore` で除外しているもの。

- **変換した Markdown と元 PDF** — 元 PDF の著作物であり、再配布にあたる。
  必要な人はそれぞれの手元で `fetch` → `md` を実行する。
- **接続先インベントリ `~/.claude/ix-devices.json`** — 機器の IP と資格情報が入る。
  `~/.claude/` 直下にあり `skills/` の外なので、そもそも同梱されない。

## ライセンス

MIT
