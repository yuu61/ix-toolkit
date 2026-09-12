---
name: ix-backup
description: NEC IX（IX2000/IX3000、IX-R/IX-V）の running-config をローカルファイルに退避する。バックアップや変更前の設定取得に使う。機器内への永続化は ix-save。
argument-hint: "[機器名] [保存先パス（省略可）]"
allowed-tools: Bash(ix-ssh:*)
compatibility: ix-ssh コマンドが PATH にあること（ix-toolkit をクローンして uv tool install -e <クローン>。手順は README）。対象の NEC IX へ SSH が通ること、インベントリ ~/.ix-toolkit/devices.json があること（場所は ix-ssh --list が表示する）。
license: MIT
---

# NEC IX 設定バックアップ

`ix-ssh --backup` で running-config をファイルに保存する。機器内の startup-config は変更しない。

## 接続先と実行条件

ユーザーの依頼と会話で確定した対象を使う。インベントリの機器は毎回
`--device <機器名>`（短縮 `-d`）で明示する。
対象が未確定なら `ix-ssh --list` で候補を示して尋ねる。機器名を推測しない。
一覧は機器に接続せず、インベントリの実際の場所、接続先、踏み台、`model`、`note` を表示する。
通常のインベントリは `~/.ix-toolkit/devices.json`。資格情報を含むファイル全体を表示する必要はない。
インベントリに無い対象が明示されていれば `--host <IP/ホスト名> --user <ユーザー>` も使える。
SSH のエイリアスと `ProxyJump` は `ix-ssh` が解決する。

`ix-ssh` は PATH のコマンドとして呼ぶ。見つからなければ README のインストール手順
（クローン → `uv tool install -e <クローン>`）を案内する。認証不足ならエラーの不足項目を伝え、
無人実行で `--ask-password` を付けない。

通常は `enable-config` を使う。他ユーザーが config モードを使用中なら停止し、
ユーザーが対象機器への強制取得を明示した場合だけ `--force-config` を付ける。
既にある指示を再確認しない。強制取得は `svintr-config` を使い、両系列とも Administrator 権限が必要で、
他ユーザーをオペレーション／EXEC モードへ戻す。使用中エラーだけを理由に切り替えない。

## 保存と確認

依頼から保存先を読み取り、指定がなければ既定名を使う。
既定はカレントディレクトリ基準の `backups/<機器名>-<YYYYMMDD-HHMMSS>.conf`。
`--host` 直指定時はホスト名を基にした名前になる。
資格情報を含み得るため、リポジトリへの追加や外部への共有を行わない。

```bash
ix-ssh -d <機器名> --backup
ix-ssh -d <機器名> --backup "<保存先パス>"
```

`ix-ssh` が親ディレクトリ作成と UTF-8 書き込みを行う。シェルリダイレクトは不要。
明示したパスに既存ファイルがあれば上書きされるため、上書きの依頼がなければ未使用の名前にする。

終了コード、標準エラーの `# target: ...`、次の成功メッセージを確認する。
`# target` は接続前の解決結果であり、この行だけで成功と判断しない。

```text
[OK] running-config of <機器名> saved to <path> (<N> lines)
```

成功時は機器名・保存パス・行数を報告する。成功メッセージが無い場合は診断を確認する。
空や予想外に短い内容ならファイルを調べ、不完全な取得をバックアップ成功として扱わない。
設定本文を報告へ丸ごと貼り付ける必要はない。

変更前の退避に続いて設定を投入する場合は `ix-configure`、機器内に永続化する場合は `ix-save` を使う。
対象機器を引き継ぎ、バックアップだけの依頼から設定投入や永続化へ進まない。
