---
name: ix-save
description: NEC IX の running-config を startup-config に保存する（write memory）。対象機器は --device で指定する。設定変更後の永続化に使用する
argument-hint: "[機器名] (e.g., home)"
allowed-tools:
  - Bash(uv:*)
  - Bash(python:*)
  - Bash(python3:*)
---

# NEC IX 設定保存（write memory）

`${CLAUDE_PLUGIN_ROOT}/scripts/ix-ssh.py --save` 経由で、現在の running-config を startup-config に保存する。

**save（write memory）を実行しないと、再起動時に設定が失われる。`/ix-configure` 実行後に必ず提案すること。**

## 接続先の指定

機器はインベントリ `~/.claude/ix-devices.json` に定義し、`--device <名前>`（短縮 `-d`）で選ぶ。**既定機器は無い**。省略するとエラーと機器一覧が返る。

```bash
uv run --script ${CLAUDE_PLUGIN_ROOT}/scripts/ix-ssh.py --list
```

インベントリに無い機器は `--host <IP> --user <ユーザー>` でその場指定できる。認証はインベントリに書いた `password`（**平文でそのまま直書きしてよい**）が第一。以降 `password_env`（変数名だけ書く方式）→ `key_file`（鍵認証）→ `$IX_PASS` の順に解決される。機器自身の資格情報がグローバルな `$IX_PASS` より優先されるので、変数の消し忘れが別機器に飛ぶことはない。どこからも取得できなければ即エラー（自動でパスワードを聞きに行かない）。

> `host` は IP でも `~/.ssh/config` のエイリアス名でもよい。エイリアスの場合は `Include` を展開したうえで `HostName` / `Port` / `User` / `ProxyJump` を解決し、**踏み台経由も自動で辿る**（paramiko の direct-tcpip チャネルを使うので Windows でも動く。netmiko 任せの ProxyCommand 方式は Windows で必ず失敗する）。解決結果と踏み台は `--list` に表示される。エイリアス解決を切るなら `--no-ssh-config`。

> IP・ユーザー・機種はこの SKILL.md に書かない。すべてインベントリ側に置く。

## 引数の解釈

`$ARGUMENTS` は機器名。直前の `/ix-configure` と**同じ機器名**を渡すこと。機器名が無く、会話中でも対象が確定していない場合は `--list` で候補を提示してユーザーに選ばせる（推測しない）。

## 実行手順

### 1. ユーザーへの確認

```
対象機器: <機器名>（<user>@<host>）
running-config を startup-config に保存します（write memory）。よろしいですか？
```

> 注意: `write memory` は**現在の running-config をそのまま startup へ永続化する**。直前の `/ix-configure` 以外にも未保存の変更があれば、それらも一緒に保存される。意図しない変更が running に含まれていないか、必要なら先に `/ix-backup <機器名>` や `show running-config` で確認すること。

### 2. 保存実行（どこからでも実行可。スクリプトは絶対パス指定）

```bash
uv run --script ${CLAUDE_PLUGIN_ROOT}/scripts/ix-ssh.py -d <機器名> --save
```

スクリプトは config モードに入り `write memory` を実行する（netmiko `save_config`）。

### 3. 結果確認

標準エラーの `# target: ...` 行で接続先が意図した機器かを確認し、出力から保存の成功/失敗をレポートする。エラー（`% ...`）が出ていないことを確認する。

> バックアップを残したい場合は、保存前に `/ix-backup <機器名>` で running-config をファイルへ退避しておくとよい。
