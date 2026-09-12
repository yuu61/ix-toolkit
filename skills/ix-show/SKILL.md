---
name: ix-show
description: NEC IX（IX2000/IX3000、IX-R/IX-V）の show コマンドを実行し、状態や設定を確認する。実機の状態確認・障害調査・表示の依頼に使う。マニュアルだけの調査は ix-manual。
argument-hint: "[機器名] <show コマンド> (e.g., home show ip route)"
allowed-tools: Bash(ix-ssh:*)
compatibility: ix-ssh コマンドが PATH にあること（ix-toolkit をクローンして uv tool install -e <クローン>。手順は README）。対象の NEC IX へ SSH が通ること、インベントリ ~/.ix-toolkit/devices.json があること（場所は ix-ssh --list が表示する）。
license: MIT
---

# NEC IX の状態確認

`ix-ssh` で show コマンドを実行する。設定変更・永続化は行わない。

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

## コマンドを選ぶ

依頼から対象と調べたい状態、指定された show コマンドを読み取る。
「home show ip route」は `-d home "show ip route"`。自然文を機器名として機械的に切り出さない。
目的だけが指定されていれば、関連する show を選ぶ。

系列はインベントリの `model` → 会話 → ユーザーへの質問の順で決める。
`IX2…` / `IX3…` は `ix`（IX2000/IX3000）、`IX-R…` / `IX-V…` は `ix-r`（IX-R/IX-V）。
`model` はヒントであり、無くても会話から決められる。機種確認が依頼に含まれるなら
`show version` の結果も使える。系列が不明なまま系列固有の綴りを試さない。

| 用途 | IX2000/IX3000 | IX-R/IX-V |
|---|---|---|
| ログバッファ | `show logging` | `show syslog` |
| 保存済み設定 | `show config` | `show startup-config` |
| NetMeister 状態 | `show nm information` | `show nm status` |

基本情報は `show version`、経路は `show ip route` / `show ipv6 route`、
現在の設定は `show running-config`、IPsec / IKE は `show ipsec sa` / `show ike sa` を使う。
インターフェース名は機種名から決め打ちせず、`show interfaces` で実在する名前を確かめる。
綴りや出力の意味が不確かなら、系列と機種を添えて `ix-manual` を引く。

## 実行と報告

show 全体を1つの引数として渡す。複数の show は1回にまとめられる。
`?` による対話的な補完は使えない。show 以外の操作を `--config` に載せて代行しない。

```bash
ix-ssh -d <機器名> "show ip route"
ix-ssh -d <機器名> "show interfaces" "show ipv6 route"
```

`ix-ssh` は show も config モード内で実行し、ページングを自動で無効にする。
モード移行や端末設定を手動で追加する必要はない。

終了コードと診断本文を確認し、標準エラーの `# target: ...` を対象と照合する。
この行は接続前の解決結果であり、接続成功の証拠ではない。`%` で始まる行には正常な通知もあるため、
先頭記号だけで失敗と判定しない。失敗したコマンドは成功した出力と区別して伝える。
コマンドが拒否されたら、別系列の綴りを試す前に `ix-manual` の対応表と本文で確かめる。

対象機器、確認した状態、根拠となる出力を簡潔に報告する。取得結果と原因の推測を区別する。
