---
name: ix-configure
description: NEC IX（IX2000/IX3000 と IX-R/IX-V）に設定を投入する。config モードで設定行を適用する。対象機器は --device で指定する。ユーザーが NEC IX の設定変更・投入を求めたときに使用する
argument-hint: "[機器名] [変更内容の説明] (e.g., home デフォルトルート追加)"
allowed-tools: Bash(ix-ssh:*)
compatibility: ix-ssh コマンドが PATH にあること（ix-toolkit をクローンして uv tool install -e <クローン>。手順は README）。対象の NEC IX へ SSH が通ること、インベントリ ~/.ix-toolkit/devices.json があること（場所は ix-ssh --list が表示する）。
license: MIT
---

# NEC IX 設定投入

`ix-ssh --config` 経由で NEC IX に設定を投入する。

**これは破壊的操作である。投入前に必ず「対象機器」と「設定行」をユーザーに提示し、確認を取ること。**

> `ix-ssh` が PATH に無ければ、ix-toolkit の README の手順（クローン → `uv tool install -e <クローン>`）をユーザーに案内する。勝手に入れたり、別の方法で呼んだりしない。

## 接続先の指定

通常は `enable-config` で入り、他ユーザーが config モードを使用中ならエラーで終了する。
ユーザーが対象機器への強制取得（`svintr-config` の使用）を明示した場合だけ、実行する
`ix-ssh` に `--force-config` を付ける。会話中にその指示があれば改めて確認しない。
使用中エラーだけを理由に強制取得へ切り替えない。強制取得は IX2000/IX3000、IX-R/IX-V とも
Administrator 権限が必要で、他ユーザーをオペレーション／EXEC モードへ戻す。

機器はインベントリ `~/.ix-toolkit/devices.json`（`ix-ssh --list` の 1 行目に実際の場所が出る）に定義し、`--device <名前>`（短縮 `-d`）で選ぶ。**既定機器は無い**。省略するとエラーと機器一覧が返る（設定が別の機器に流れ込む事故を防ぐための設計であり、埋め合わせに機器名を推測してはならない）。

```bash
ix-ssh --list
```

インベントリに無い機器は `--host <IP> --user <ユーザー>` でその場指定できる。認証はインベントリに書いた `password`（**平文でそのまま直書きしてよい**）が第一。以降 `password_env`（変数名だけ書く方式）→ `key_file`（鍵認証）→ `$IX_PASS` の順に解決される。機器自身の資格情報がグローバルな `$IX_PASS` より優先されるので、変数の消し忘れが別機器に飛ぶことはない。どこからも取得できなければ即エラー（自動でパスワードを聞きに行かない）。

> `host` は IP でも `~/.ssh/config` のエイリアス名でもよい。エイリアスの場合は `Include` を展開したうえで `HostName` / `Port` / `User` / `ProxyJump` を解決し、**踏み台経由も自動で辿る**（paramiko の direct-tcpip チャネルを使うので Windows でも動く。netmiko 任せの ProxyCommand 方式は Windows で必ず失敗する）。解決結果と踏み台は `--list` に表示される。エイリアス解決を切るなら `--no-ssh-config`。

> IP・ユーザー・機種はこの SKILL.md に書かない。すべてインベントリ側に置く。機種名は任意項目 `model`（`"model": "IX2215"`）に書く。それ以外の機器固有の注意（インターフェース名の体系、運用上の制約）は `note` フィールドに書き、どちらも `--list` で読める状態にしておく。

## 対象と引数の読み取り

まず `--list` を実行して機器名の一覧を取得し、依頼に現れる語がその中の名前と一致すればそれを `--device` に、残りを変更内容の説明として扱う（破壊的操作なので、機器名は毎回この照合で確定させる）。一致しない・機器名が無い場合は `--list` の結果を提示して**必ずユーザーに選ばせる**（この skill では機器の推測は禁止）。

## NEC IX の重要な特性

- **enable モード = config モード**。スクリプトが `enable-config`（`--force-config` 指定時は `svintr-config`）で自動的に config モードへ入り、設定行を適用して抜ける（netmiko `send_config_set`）。サブモードからグローバルへ戻すときは `configure` を使う。
- NEC IX は IOS 風で**明示的な commit は不要**。多くの設定は適用時に反映されるが、**一部の変更は反映に再起動が必要**（機器が `% You must restart the router for this configuration to take effect.` と警告する）。
- 設定は**再起動で消えるため、永続化には `write memory` が必要**（`--save` または `ix-save`）。
- 設定削除は IOS 風に行頭へ `no ` を付ける。
- 複数行は 1 回の config セッションでまとめて適用される。

## 実行手順（必ずこの順序で）

### 1. 対象機器と設定内容の確定

対象機器を確定し（不明なら `--list` で確認）、ユーザーの要求と関連設計書（`docs/design/` 等がある場合）から投入すべき設定行を特定する。インターフェース名は機種・構成で異なるため、`ix-show` で `show interfaces` / `show running-config` を確認してから決める（決め打ちしない）。

**系列を先に決める。** NEC IX は IX2000/IX3000（無印）と IX-R/IX-V の 2 系列でコマンドが違う（同じ名前でも登録するモードが違うものがある）。`--list` や `# target: ...` 行の `model` が `IX-R…` / `IX-V…` なら IX-R/IX-V、`IX2…` / `IX3…` なら無印。`model` はヒントであってゲートではない。無い・分からなければ会話の文脈から決め、それでも決まらなければ `ix-show` で `show version` を実行して機種名を取る（推測で系列を決めて設定を書かない）。

**コマンドの綴り・入力形式・実行モードが不確かなときは、記憶で書かずに `ix-manual` で調べる。** その際、系列と `model`（機種名）を添える。無印のマニュアルは IX2000/IX3000 の全機種をまとめたもので、諸元値（設定数の上限など）は機種ごとに列が分かれているため、機種名が無いと別機種の値を読む。 特に `no` 形の綴りと、そのコマンドが属するコンフィグモードは間違えやすい。`ix-manual` はコマンドリファレンスマニュアルの該当項目（入力形式・パラメータ・実行モード・ユーザー権限・ノート）を返す。ノートに書かれた制約（再起動要否、併用不可など）は投入前にユーザーへ伝えること。

**IX-R/IX-V に投入するときは、設定行を `ix-manual` の `diff.tsv`（無印 → IX-R の対応表）で照合する。** 無印の設定を移す場合、無印で覚えたコマンドをそのまま書いた場合に効く。当たった行の `kind` で扱いが変わる（`source` が `ch8` / `derived` の行が対象。`setdiff` は後述）。

| `kind` | 設定行の扱い |
|---|---|
| `removed` / `limit` | **案から外す。** `ix-r` 列か `note` に統一先があれば（`restart` → `reload`、`show config` → `show startup-config`）それを示す。無ければ代替をユーザーと相談する |
| `renamed` | `ix-r` 列の綴りに**置き換えて残す**（`logging buffered` → `syslog enable`） |
| `moved` | コマンドは同じだが**登録するモードが変わる。** インタフェースコンフィグへ移すものは `interface` ブロックの中に書き直す（`http-server ip enable` はグローバルではなく該当インタフェース配下）。IX-R 機能説明書 8.3.3 のリスト 8.3.1 / 8.3.2 に移し方の前後が載っている |
| `range` / `changed` | **行は残し、値とオプションを `note` と突き合わせる。** 範囲外の値・廃止されたオプション（`ip max-route unlimited`、`ipsec policy` の `in` / `out`）は直してから提示する |

- `source` が `setdiff`（索引の集合差）の行に当たったら**警告として添える**だけでよい。本当に無いかは `commands.tsv` で確かめる。
- 照合は手段であって、投入の可否を決めるのは次項のユーザー確認である（`diff.tsv` に無いから安全、とはしない。8 章は使用頻度の高い差分だけを載せている）。

### 2. ユーザーへの確認提示

投入する設定行を一覧表示し、**必ず確認を取る**。以下のフォーマットで提示する:

```
対象機器: <機器名>（<user>@<host>） ← --list の表示と一致すること
系列    : IX-R/IX-V（model IX-R2530）← 何から決めたかを添える（model / 会話 / show version）
投入する設定:
  ip route default GigaEthernet1.0
  logging buffered 100
diff.tsv 照合: 該当なし（または「logging buffered → syslog enable に置き換えた (renamed, ch8)」「logging packet は廃止 (removed, ch8) → 案から外した」）
永続化: write memory を実行する（--save）

この設定を投入しますか？
```

### 3. 投入実行

確認が取れたら実行する:

```bash
ix-ssh -d <機器名> --config "ip route default GigaEthernet1.0" --config "logging buffered 100" --save
```

行数が多い場合はファイルから:

```bash
ix-ssh -d <機器名> --config-file changes.ix --save
```

（`--config-file` は 1 行 1 コマンド、`#` 始まりはコメント）

### 4. 結果確認

- 標準エラーの `# target: ...` 行が**確認を取った機器と一致している**ことを最初に確かめる。
- 出力にエラー（`% ...`）が無いか確認する。
- `% You must restart the router ...` が出た場合は、その変更の反映に**再起動が必要**な旨をユーザーに伝える（勝手に reload しない）。
- `ix-show` で設定が反映されたことを確認する（例: `show running-config`）。同じ機器名を渡すこと。
- `--save` を付けなかった場合は、永続化のため `ix-save` をユーザーに提案する。

## 注意事項

- `no` を伴う削除は特に慎重に。対象を必ず提示してから実行する。
- 投入前に `ix-backup` で running-config を退避しておくと安全。
- 初回の本番投入時は、まず 1 行だけの低リスクな変更で挙動を確認することを推奨。
- 複数機器へ同じ設定を流すときも、1 機器ずつ確認・実行する（一括ループにしない）。
