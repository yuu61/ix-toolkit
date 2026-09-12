---
name: ix-save
description: NEC IX（IX2000/IX3000、IX-R/IX-V）で write memory を実行し、running-config を startup-config に永続化する。機器内への設定保存の依頼に使う。ファイルへの退避は ix-backup。
argument-hint: "[機器名] (e.g., home)"
allowed-tools: Bash(ix-ssh:*)
compatibility: ix-ssh コマンドが PATH にあること（ix-toolkit をクローンして uv tool install -e <クローン>。手順は README）。対象の NEC IX へ SSH が通ること、インベントリ ~/.ix-toolkit/devices.json があること（場所は ix-ssh --list が表示する）。
license: MIT
---

# NEC IX 設定保存

`ix-ssh --save` で現在の running-config 全体を startup-config に保存する。
直前の変更以外に未保存の変更があれば、それらも一緒に保存される。

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

## 保存の範囲を確かめる

依頼で明示された対象を使う。直前の設定変更を「保存して」と依頼された場合は、
その変更と同じ対象を引き継ぐ。別の機器が明示されたときに直前の機器で上書きしない。
「保存」がローカルファイルへの退避を意味するなら `ix-backup` を使う。

対象への `write memory`、または設定変更と永続化が既に依頼・承認されていれば再確認せず進む。
設定変更だけの依頼から自動的に永続化しない。保存が未承認なら、対象と running-config 全体を
保存することを示して確認する。内容に疑問があれば、先に `ix-show` で必要な設定を確認する。

## 実行と報告

```bash
ix-ssh -d <機器名> --save
```

終了コードと保存結果の本文を確認し、標準エラーの `# target: ...` を対象と照合する。
`# target` は接続前の解決結果で、保存成功を示すものではない。
正常な保存メッセージも `%` で始まるため、その記号だけでエラー扱いしない。
接続切断やタイムアウトで結果が不明なら、保存成功と断定したり無条件に再実行したりせず、
`ix-show` で保存済み設定（無印は `show config`、IX-R/IX-V は `show startup-config`）を確認する。
確認できない場合は結果不明と報告する。

対象と保存の成否を伝える。再起動は保存とは別の操作なので、この skill から実行しない。
