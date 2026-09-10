---
name: ix-backup
description: NEC IX の running-config をファイルに退避する。対象機器は --device で指定する。設定変更前のバックアップや定期保存に使用する
argument-hint: "[機器名] [保存先パス（省略可）]"
allowed-tools: Bash(ix-ssh:*) Bash(uvx:*) Bash(uv:*)
compatibility: uv と ix-ssh コマンドが要る（uv tool install git+https://github.com/yuu61/ix-toolkit）。対象の NEC IX へ SSH が通ること、インベントリ ~/.ix-toolkit/devices.json があること（場所は ix-ssh --list が表示する）。
license: MIT
---

# NEC IX 設定バックアップ

`ix-ssh --backup` で running-config を取得してファイルに保存する。**読み取り専用**。スクリプトが親ディレクトリ作成・タイムスタンプ既定名・UTF-8 書き込みまで行うため、`mkdir` やシェルリダイレクトは不要（単一の python コマンドで完結）。

> `ix-ssh` が PATH に無ければ `uvx --from git+https://github.com/yuu61/ix-toolkit ix-ssh` が同じもの。以降の例の `ix-ssh` をこれに置き換える（初回だけ解決に数秒）。

## 接続先の指定

機器はインベントリ `~/.ix-toolkit/devices.json`（`ix-ssh --list` の 1 行目に実際の場所が出る）に定義し、`--device <名前>`（短縮 `-d`）で選ぶ。**既定機器は無い**。省略するとエラーと機器一覧が返る。

対象が不明なときは推測せず、まず一覧を出してユーザーに確認する:

```bash
ix-ssh --list
```

インベントリに無い機器は `--host <IP> --user <ユーザー>` でその場指定できる。認証はインベントリに書いた `password`（**平文でそのまま直書きしてよい**）が第一。以降 `password_env`（変数名だけ書く方式）→ `key_file`（鍵認証）→ `$IX_PASS` の順に解決される。機器自身の資格情報がグローバルな `$IX_PASS` より優先されるので、変数の消し忘れが別機器に飛ぶことはない。どこからも取得できなければ即エラー（自動でパスワードを聞きに行かない）。

> `host` は IP でも `~/.ssh/config` のエイリアス名でもよい。エイリアスの場合は `Include` を展開したうえで `HostName` / `Port` / `User` / `ProxyJump` を解決し、**踏み台経由も自動で辿る**（paramiko の direct-tcpip チャネルを使うので Windows でも動く。netmiko 任せの ProxyCommand 方式は Windows で必ず失敗する）。解決結果と踏み台は `--list` に表示される。エイリアス解決を切るなら `--no-ssh-config`。

> IP・ユーザー・機種はこの SKILL.md に書かない。すべてインベントリ側に置く。

## 対象と引数の読み取り

ユーザーの依頼から**機器名**を、書かれていれば**保存先パス**を取り出す。

- 「home をバックアップ」 → `-d home --backup`（既定名に保存）
- 「home を backups/before-change.conf に退避」 → 保存先を明示
- 機器名が無い場合 → `--list` で候補を提示し、ユーザーに選ばせる（会話中で対象機器が既に確定していればそれを使う）

既定の保存先は `backups/<機器名>-<YYYYMMDD-HHMMSS>.conf`（機器名はインベントリのキー、`--host` 直指定時はホスト名）。カレントディレクトリ基準の相対パスなので、複数機器を扱っても取り違えない。

## 実行手順

1. 対象機器と保存先の指定有無を確認する。
2. 実行する:

   ```bash
   # 既定名（backups/<機器名>-<タイムスタンプ>.conf）に保存:
   ix-ssh -d <機器名> --backup

   # 保存先を明示:
   ix-ssh -d <機器名> --backup backups/before-change.conf
   ```

3. 標準エラーの `# target: ...` 行で接続先が意図した機器かを確認する。
4. スクリプトが `[OK] running-config of <機器名> saved to <path> (<N> lines)` を出力する。その行を確認し、機器名・保存パス・行数をユーザーにレポートする。`[OK]` が出ない、または行数が極端に少ない場合は失敗とみなして原因を調べる。

> `ix-backup` は running-config の取得・退避のみ（機器内の startup は変更しない）。機器側での永続化は `ix-save`（write memory）。設定変更時は **`ix-backup` → `ix-configure` → `ix-save`** の順が安全。いずれも同じ機器名を通すこと。
