---
name: ix-configure
description: NEC IX ルータに設定を投入する。config モードで設定行を適用する。対象機器は --device で指定する。ユーザーが NEC IX の設定変更・投入を求めたときに使用する
argument-hint: "[機器名] [変更内容の説明] (e.g., home デフォルトルート追加)"
allowed-tools:
  - Bash(python:*)
---

# NEC IX 設定投入

`~/.claude/skills/ix-ssh.py --config` 経由で NEC IX に設定を投入する。

**これは破壊的操作である。投入前に必ず「対象機器」と「設定行」をユーザーに提示し、確認を取ること。**

## 接続先の指定

機器はインベントリ `~/.claude/ix-devices.json` に定義し、`--device <名前>`（短縮 `-d`）で選ぶ。**既定機器は無い**。省略するとエラーと機器一覧が返る（設定が別の機器に流れ込む事故を防ぐための設計であり、埋め合わせに機器名を推測してはならない）。

```bash
python ~/.claude/skills/ix-ssh.py --list
```

インベントリに無い機器は `--host <IP> --user <ユーザー>` でその場指定できる。認証はインベントリに書いた `password`（**平文でそのまま直書きしてよい**）が第一。以降 `password_env`（変数名だけ書く方式）→ `key_file`（鍵認証）→ `$IX_PASS` の順に解決される。機器自身の資格情報がグローバルな `$IX_PASS` より優先されるので、変数の消し忘れが別機器に飛ぶことはない。どこからも取得できなければ即エラー（自動でパスワードを聞きに行かない）。

> `host` は IP でも `~/.ssh/config` のエイリアス名でもよい。エイリアスの場合は `Include` を展開したうえで `HostName` / `Port` / `User` / `ProxyJump` を解決し、**踏み台経由も自動で辿る**（paramiko の direct-tcpip チャネルを使うので Windows でも動く。netmiko 任せの ProxyCommand 方式は Windows で必ず失敗する）。解決結果と踏み台は `--list` に表示される。エイリアス解決を切るなら `--no-ssh-config`。

> IP・ユーザー・機種はこの SKILL.md に書かない。すべてインベントリ側に置く。機器固有の注意（機種、インターフェース名の体系、運用上の制約）はインベントリの `note` フィールドに書き、`--list` で読める状態にしておく。

## 引数の解釈

まず `--list` を実行して機器名の一覧を取得し、`$ARGUMENTS` の第 1 トークンがその中の名前と一致すればそれを `--device` に、残りを変更内容の説明として扱う（破壊的操作なので、機器名は毎回この照合で確定させる）。一致しない・トークンが無い場合は `--list` の結果を提示して**必ずユーザーに選ばせる**（`/ix-configure` では機器の推測は禁止）。

## NEC IX の重要な特性

- **enable モード = config モード**。スクリプトが `svintr-config` / `configure` で自動的に config モードへ入り、設定行を適用して抜ける（netmiko `send_config_set`）。
- NEC IX は IOS 風で**明示的な commit は不要**。多くの設定は適用時に反映されるが、**一部の変更は反映に再起動が必要**（機器が `% You must restart the router for this configuration to take effect.` と警告する）。
- 設定は**再起動で消えるため、永続化には `write memory` が必要**（`--save` または `/ix-save`）。
- 設定削除は IOS 風に行頭へ `no ` を付ける。
- 複数行は 1 回の config セッションでまとめて適用される。

## 実行手順（必ずこの順序で）

### 1. 対象機器と設定内容の確定

対象機器を確定し（不明なら `--list` で確認）、ユーザーの要求と関連設計書（`docs/design/` 等がある場合）から投入すべき設定行を特定する。インターフェース名は機種・構成で異なるため、`/ix-show` で `show interfaces` / `show running-config` を確認してから決める（決め打ちしない）。

**コマンドの綴り・入力形式・実行モードが不確かなときは、記憶で書かずに `/ix-manual <コマンド名>` で調べる。** 特に `no` 形の綴りと、そのコマンドが属するコンフィグモードは間違えやすい。`/ix-manual` はコマンドリファレンスマニュアルの該当項目（入力形式・パラメータ・実行モード・ユーザ権限・ノート）を返す。ノートに書かれた制約（再起動要否、併用不可など）は投入前にユーザーへ伝えること。

### 2. ユーザーへの確認提示

投入する設定行を一覧表示し、**必ず確認を取る**。以下のフォーマットで提示する:

```
対象機器: <機器名>（<user>@<host>） ← --list の表示と一致すること
投入する設定:
  ip route default GigaEthernet1.0
  logging buffered 100
永続化: write memory を実行する（--save）

この設定を投入しますか？
```

### 3. 投入実行

確認が取れたら実行する（どこからでも実行可。スクリプトは絶対パス指定）:

```bash
python ~/.claude/skills/ix-ssh.py -d <機器名> \
  --config "ip route default GigaEthernet1.0" \
  --config "logging buffered 100" \
  --save
```

行数が多い場合はファイルから:

```bash
python ~/.claude/skills/ix-ssh.py -d <機器名> --config-file changes.ix --save
```

（`--config-file` は 1 行 1 コマンド、`#` 始まりはコメント）

### 4. 結果確認

- 標準エラーの `# target: ...` 行が**確認を取った機器と一致している**ことを最初に確かめる。
- 出力にエラー（`% ...`）が無いか確認する。
- `% You must restart the router ...` が出た場合は、その変更の反映に**再起動が必要**な旨をユーザーに伝える（勝手に reload しない）。
- `/ix-show` で設定が反映されたことを確認する（例: `show running-config`）。同じ機器名を渡すこと。
- `--save` を付けなかった場合は、永続化のため `/ix-save` をユーザーに提案する。

## 注意事項

- `no` を伴う削除は特に慎重に。対象を必ず提示してから実行する。
- 投入前に `/ix-backup <機器名>` で running-config を退避しておくと安全。
- 初回の本番投入時は、まず 1 行だけの低リスクな変更で挙動を確認することを推奨。
- 複数機器へ同じ設定を流すときも、1 機器ずつ確認・実行する（一括ループにしない）。
