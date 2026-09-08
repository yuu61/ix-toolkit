---
name: ix-manual
description: NEC IX ルータ (IX3315 / IX2215 等) のコマンドリファレンスマニュアルを引く。コマンドの入力形式・パラメータ・実行モード・ユーザ権限・デフォルト値を確認する。コマンド名やコマンドの綴りが不確かなとき、設定を投入する前に仕様を確かめたいときに使用する
argument-hint: "<コマンド名または調べたいこと> (e.g., ngn ip enable / IPsec の SA 有効期限)"
allowed-tools:
  - Grep
  - Glob
  - Read
---

# NEC IX コマンドリファレンス参照

**読み取り専用。機器には一切接続しない。**

`pdfbook` で Markdown に変換したコマンドリファレンスマニュアルを検索し、
コマンドの正確な綴り・入力形式・パラメータ・実行モード・ユーザ権限を返す。

## マニュアルの置き場所

`~/.claude/ix-manuals/` 配下。1 冊ごとにディレクトリがある。

```
~/.claude/ix-manuals/crm/
├── commands.tsv                        機械可読索引 (これを最初に引く)
├── index.md                            コマンド索引と目次
└── ch03-インタフェース編/NGN.md          本文
```

まず `Glob` で `~/.claude/ix-manuals/*/commands.tsv` を探し、見つからなければ
**その旨をユーザーに伝えて止まる**（記憶からコマンドを答えない）。

未変換なら [ix-toolkit](https://github.com/yuu61/ix-toolkit) の `pdfbook` で生成する。
マニュアル本文は著作物なのでリポジトリには含まれておらず、各自の手元で変換する:

```
pdfbook fetch -manifest manifest.json -out pdf/
pdfbook md pdf/CRM-ver10.11-1.1.pdf -profile profiles/nec-ix-crm.json \
           -out ~/.claude/ix-manuals/crm
```

## 引き方（必ずこの順序で）

### 1. commands.tsv を引く

`commands.tsv` は `command / entry / file / anchor / page` のタブ区切り。
1 行 1 コマンドで、コマンド名がそのまま先頭列にある。

```
Grep: pattern="^ngn ip enable\t" path="~/.claude/ix-manuals/crm/commands.tsv"
```

綴りが不確かなときは部分一致で候補を出す（`ngn.*history`, `^ipsec ` など）。
**候補が複数あれば、推測せずに一覧を提示してユーザーに選ばせる。**

### 2. 本文を読む

`commands.tsv` の `file` 列と `anchor` 列が該当箇所を指す。
`Read` で該当ファイルを開き、`<a id="<anchor>">` の直後の項目を読む。

項目は必ずこの構成になっている:

- **入力形式** — コマンドの構文（`no` 形を含む）
- **パラメータ** — 引数の意味と範囲・書式
- **説明** — 何をするコマンドか
- **デフォルト値**
- **実行モード** — グローバルコンフィグ / インタフェースコンフィグ 等
- **ユーザ権限** — Administrator / Operator / Monitor
- **入力例**
- **ノート** — 制約・注意（再起動要否など重要な情報が入る）

### 3. 見つからないとき

コマンド名で当たらなければ、本文を日本語で全文検索する。

```
Grep: pattern="ヒストリ" path="~/.claude/ix-manuals/crm" glob="*.md" output_mode="content"
```

それでも無ければ「このマニュアルには記載が無い」と答える。
**記憶や類推でコマンドを作らない。** IX のコマンドは機種・バージョンで異なる。

## 報告のしかた

コマンド 1 つにつき、最低限これを示す:

```
コマンド : ngn ip enable
実行モード: インタフェースコンフィグモード（Ether 系インタフェース）
ユーザ権限: Administrator
説明     : 該当インタフェースを NGN 網とのユーザ網インタフェース（UNI）に設定し、NGN 機能を有効化します。
デフォルト: 無効
出典     : NGN p.3-29
```

**ノートに制約が書かれていれば必ず伝える**（「複数のインタフェースに設定することはできません」等）。
これを落とすと、設定投入時に初めて弾かれることになる。

## 他の skill との関係

- `/ix-configure` で設定を投入する前に、このスキルで**入力形式と実行モードを確認する**。
  特に `no` 形の綴りと、そのコマンドがどのコンフィグモードに属するかを確かめる。
- `/ix-show` で出力の読み方が分からないときも、該当コマンドの説明をここで引く。
- ただし**インタフェース名（`GigaEthernet0.0` 等）はマニュアルでは決まらない**。
  機種・構成で異なるので、必ず `/ix-show` の `show interfaces` で実機を確認すること。

## 注意事項

- マニュアルは特定バージョン（例 10.11）のもの。実機のバージョンが違えば差異がありうる。
  `/ix-show show version` と突き合わせ、食い違う可能性があるときはその旨を添える。
- 変換された Markdown は元 PDF の著作物であり、手元での参照用。外部に転記・配布しない。
