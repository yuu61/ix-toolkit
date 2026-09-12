---
name: ix-show
description: NEC IX（IX2000/IX3000 と IX-R/IX-V）の show コマンドを実行して状態を確認する（インターフェース, ルーティング, IPsec, ログ等）。対象機器は --device で指定する。ユーザーが NEC IX の状態確認・表示を求めたときに使用する
argument-hint: "[機器名] <show コマンド> (e.g., home show ip route)"
allowed-tools: Bash(ix-ssh:*)
compatibility: ix-ssh コマンドが PATH にあること（ix-toolkit をクローンして uv tool install -e <クローン>。手順は README）。対象の NEC IX へ SSH が通ること、インベントリ ~/.ix-toolkit/devices.json があること（場所は ix-ssh --list が表示する）。
license: MIT
---

# NEC IX show コマンド実行

`ix-ssh`（netmiko `nec_ix_ssh`）経由で NEC IX の show コマンドを実行する。**読み取り専用**。

> `ix-ssh` が PATH に無ければ、ix-toolkit の README の手順（クローン → `uv tool install -e <クローン>`）をユーザーに案内する。勝手に入れたり、別の方法で呼んだりしない。

## 接続先の指定

通常は `enable-config` で入り、他ユーザーが config モードを使用中ならエラーで終了する。
ユーザーが対象機器への強制取得（`svintr-config` の使用）を明示した場合だけ、実行する
`ix-ssh` に `--force-config` を付ける。会話中にその指示があれば改めて確認しない。
使用中エラーだけを理由に強制取得へ切り替えない。強制取得は IX2000/IX3000、IX-R/IX-V とも
Administrator 権限が必要で、他ユーザーをオペレーション／EXEC モードへ戻す。

機器はインベントリ `~/.ix-toolkit/devices.json`（`ix-ssh --list` の 1 行目に実際の場所が出る）に定義し、`--device <名前>`（短縮 `-d`）で選ぶ。**既定機器は無い**。指定を省略するとスクリプトはエラーを返し、機器一覧を表示する（誤った機器への接続を防ぐため）。

対象が不明・未確定のときは推測せず、まず一覧を出してユーザーに確認する:

```bash
ix-ssh --list
```

インベントリに無い機器はその場で指定できる:

```bash
ix-ssh --host <IP/ホスト名> --user <ユーザー> "show version"
```

認証はインベントリに書いた `password`（**平文でそのまま直書きしてよい**）が第一。以降 `password_env`（変数名だけ書く方式）→ `key_file`（鍵認証）→ `$IX_PASS` の順に解決される。機器自身の資格情報がグローバルな `$IX_PASS` より優先されるので、変数の消し忘れが別機器に飛ぶことはない。どこからも取得できなければ即エラー（自動でパスワードを聞きに行かない）。

> `host` は IP でも `~/.ssh/config` のエイリアス名でもよい。エイリアスの場合は `Include` を展開したうえで `HostName` / `Port` / `User` / `ProxyJump` を解決し、**踏み台経由も自動で辿る**（paramiko の direct-tcpip チャネルを使うので Windows でも動く。netmiko 任せの ProxyCommand 方式は Windows で必ず失敗する）。解決結果と踏み台は `--list` に表示される。エイリアス解決を切るなら `--no-ssh-config`。

> IP・ユーザー・機種はこの SKILL.md に書かない。すべてインベントリ側に置く。

## 対象と引数の読み取り

ユーザーの依頼（この skill を名指しで呼んだときに続けて書かれた文字列を含む）から機器名と show コマンドを取り出す。先頭が `show` で始まらなければ、そこまでを**機器名**として `--device` に渡し、残りを show コマンドとして扱う。

- 「home show ip route」 → `-d home "show ip route"`
- 「show ip route」 → 機器名なし。会話中で対象機器が既に確定していればそれを使う。確定していなければ `--list` を実行して候補を提示し、ユーザーに選ばせる。

show コマンドは `"show ..."` 全体を 1 つの引数として渡す（複数指定可）。

## NEC IX の重要な特性

- **enable モード = config モード**。`show running-config` や `ipsec` / `ike` / `logging` / `ntp` / `vrrp` 等の多くの show も **config モード内のみ**。スクリプトが `enable-config`（`--force-config` 指定時は `svintr-config`）で入り、show を実行する。サブモードからグローバルへ戻すときは `configure` を使う。
- paging は接続時に `terminal length 0` で自動無効化される。
- EXEC モードでも使える show（version / clock / uptime / ip / ipv6 / interfaces / arp 等）も、本スクリプトは一律 config モードで実行する（config モードの show は EXEC のスーパーセットのため確実）。

## 実行手順

1. 対象機器を確定する（上記「対象と引数の読み取り」）。
2. 引数を show コマンド文字列に組み立てる（必ず `show ` で始める）。
3. 以下を実行する:

   ```bash
   ix-ssh -d <機器名> "show ip route"
   # 複数まとめて:
   ix-ssh -d <機器名> "show interfaces" "show ipv6 route"
   ```

4. 標準エラーに `# target: <機器名> (<user>@<host>:<port>, ...)` が出るので、**意図した機器に接続したことを確認**してから出力を整形し、状態をわかりやすくレポートする。インベントリに `model` が書いてあれば `..., model IX2215` として同じ行に出る。**機種名は系列（下記）と諸元値の読み取りに要るので、`ix-manual` を引くときはこの値を添える**（無印のマニュアルは IX2000/IX3000 の全機種をまとめたもので、諸元表は機種ごとに列が分かれている）。

## 系列で変わるコマンド名

NEC IX は **IX2000/IX3000（無印）と IX-R/IX-V の 2 系列**で、show コマンドの名前が一部違う。系列はインベントリの `model`（`IX-R…` / `IX-V…` なら IX-R/IX-V、`IX2…` / `IX3…` なら無印）か会話から決める。`model` はヒントであってゲートではない。無い・分からなければ `show version` を実行して出力の機種名で決める（この skill は接続するので自分で確かめられる）。

| 無印 | IX-R/IX-V | 用途 |
|---|---|---|
| `show logging` | `show syslog` | ログバッファ |
| `show config` | `show startup-config` | 保存済み設定 |
| `show nm information` | `show nm status` | NetMeister 状態 |

片方の綴りで `% ` エラーが返ったら、もう片方を試す前に `ix-manual` の `diff.tsv` で対応を確かめる。

## よく使う show コマンド

- `show version` / `show clock` / `show uptime` — 基本情報
- `show interfaces` / `show interfaces <IF名>` — インターフェース状態
- `show ip route` / `show ipv6 route` — ルーティングテーブル
- `show arp` — ARP テーブル
- `show running-config` — 現在の設定（config モード内で実行）
- `show ipsec sa` / `show ike sa` — IPsec / IKE 状態
- `show logging`（無印）/ `show syslog`（IX-R）— ログバッファ
- `show environment` — 電圧・温度
- `show ntp` / `show dns` — 時刻同期 / 名前解決

> インターフェース名（`GigaEthernet0.0` 等）は機種・構成で異なる。決め打ちせず `show interfaces` の出力で確認すること。サブコマンドが不明なときは、まず大分類（例: `show ip route`）を実行して出力から判断する。`?` 補完はスクリプト経由では使えないため、フルコマンドで指定すること。

> コマンドの綴りや出力の読み方が分からないときは `ix-manual` でコマンドリファレンスマニュアルを引く（読み取り専用・機器に接続しない）。系列を添えて引くこと。ただしインターフェース名だけはマニュアルでは決まらないので、必ず実機の `show interfaces` で確認すること。
