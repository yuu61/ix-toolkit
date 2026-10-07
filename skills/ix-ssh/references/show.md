# 状態確認

show コマンドで状態や設定を確認する。設定変更・永続化は行わない。
接続先・系列と共通の結果確認は [ix-ssh の手順](../SKILL.md) に従う。

## コマンドを選ぶ

依頼から対象と調べたい状態、指定された show コマンドを読み取る。
「home show ip route」は `-d home "show ip route"`。自然文を機器名として機械的に切り出さない。
目的だけが指定されていれば、関連する show を選ぶ。

| 用途 | IX2000/IX3000 | IX-R/IX-V |
|---|---|---|
| ログバッファ | `show logging` | `show syslog` |
| 保存済み設定 | `show config` | `show startup-config` |
| NetMeister 状態 | `show nm information` | `show nm status` |

基本情報は `show version`、経路は `show ip route` / `show ipv6 route`、
現在の設定は `show running-config`、IPsec / IKE は `show ipsec sa` / `show ike sa` を使う。
インターフェースを調べる場合は `show interfaces` で実在する名前を確かめる。
ログの意味は両系列とも ix-manual の syslog 参照手順を使う。取得した無印のログも、
IX-R/IX-V の syslog リファレンスにある無印の参考名称から対応する function を探して調べられる。

## 実行と報告

show 全体を1つの引数として渡す。複数の show は1回にまとめられる。
`?` による対話的な補完は使えない。show 以外の操作を `--config` に載せて代行しない。

```bash
ix-ssh -d <機器名> "show ip route"
ix-ssh -d <機器名> "show interfaces" "show ipv6 route"
```

失敗したコマンドは成功した出力と区別して伝える。
コマンドが拒否されたら、別系列の綴りを試す前に ix-manual の対応表と本文で確かめる。
対象機器、確認した状態、根拠となる出力を簡潔に報告する。取得結果と原因の推測を区別する。
