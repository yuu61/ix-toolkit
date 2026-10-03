# ix-toolkit

NEC IX を Claude Code 等から運用するための skill 一式と、その参照マニュアルを作る PDF / Web → Markdown 変換ツールです。
IX2000/IX3000（無印）と IX-R/IX-V の 2 系列を扱います。skill は SKILL.md 形式なので、Codexなど同じ形式を読むエージェントでもそのまま動きます。

---

## 1. 運用スキル (`ix-ssh` / skills)

LLM が IX ルータの状態確認や設定変更を行うためのツール群です。

| skill | 用途 | 種別 |
|---|---|---|
| `ix-show` | show コマンドで状態確認 | 読み取り専用 |
| `ix-manual` | コマンドリファレンス・機能説明書などを検索 | 読み取り専用・機器接続なし |
| `ix-backup` | running-config をファイルに退避 | 読み取り専用 |
| `ix-configure` | 設定を投入 | **破壊的**・実行前確認必須 |
| `ix-save` | 設定を保存 (`write memory`) | **破壊的** |

### インストール

```console
git clone https://github.com/yuu61/ix-toolkit "$HOME/.agents/skills/ix-toolkit"
uv tool install -e "$HOME/.agents/skills/ix-toolkit"
```

全プロジェクトで使え、スキルごとのコピーや登録は不要です。

インストール後はエージェントを再起動してください。`ix-ssh` が見つからない場合は
`uv tool update-shell` を実行し、ターミナルとエージェントを開き直します。

更新は `git -C "$HOME/.agents/skills/ix-toolkit" pull` で本体・スキルの両方に反映されます。
依存パッケージの変更時は `uv tool install --reinstall -e "$HOME/.agents/skills/ix-toolkit"` を再実行します。

### 接続先 (インベントリ)

`~/.ix-toolkit/devices.json` を作成して対象機器を定義します。

```json
{
  "devices": {
    "home": {
      "host": "192.0.2.1",
      "username": "admin",
      "password": "...",
      "model": "IX2215",
      "note": "Core Router"
    }
  }
}
```

- `host` は IP アドレスや `~/.ssh/config` のエイリアスが使用可能です（`ProxyJump` も自動で辿ります）。対象または踏み台の有効な `ProxyCommand` は未対応として接続前にエラーにし、`--list` では未解決の経路と表示します。
- 意図しない機器への設定投入を防ぐため、既定の機器は設定できません。エージェントが会話から判断するかユーザーに尋ねます。
- 動作確認は手動で `ix-ssh --list` を実行してください。

### コンフィグモードについて

通常は `enable-config` で入ります。他のユーザーが使用中の場合はエラーになります。
強制取得を明示する場合は `--force-config` を付けて実行します。skill は、ユーザーから「強制的に設定して」など明示された場合のみこのオプションを付与します。

設定応答は行ごとに表示し、パスワード・認証鍵・PIN 等のエコーと診断中の値を `[REDACTED]` に伏せます。
対象は IX CRM / FD 10.11-1.1 と IX-R CRM / FD 1.5a の調査に基づき、機種情報がなくても両系列を保護します。
省略形・曖昧な構文・任意文字列への埋め込みでは引数列を広く伏せる場合があります。`--raw` でも保護します。
再起動要求やパスワード強度評価、未分類の通知は秘密を除いて表示し、後続行の失敗でも先行する通知を残します。
既知の英語診断に加え、送信した入力行に対する `% 入力行 -- 診断文` を拒否として判定します。
診断文が未分類でもこの形式なら行番号を示し、後続の設定・保存を止めます。
完了行数は応答を受信し、既知の拒否を検出しなかった数です。動作確認は別の show、永続化は `--save` / `ix-save` で行います。
対話式の鍵・証明書入力は未対応のため接続前に拒否し、端末に返る秘密鍵の出力は全体を伏せます。
show の本文や機器が書き出すファイルはこの設定応答マスクの対象外です。

---

## 2. マニュアル変換ツール (`manualbook`)

NEC の公式マニュアル（PDF / Web）を取得し、`ix-manual` skill が読める Markdown と索引に変換する Go 製のツールです。

### 使い方

```console
go build -ldflags="-s -w" ./cmd/manualbook
./manualbook build
```

※ Windows では `manualbook.exe` ができるので、PowerShell では `.\manualbook.exe build` と実行します。
※ Windows Defender の誤検知を避けるため `-ldflags="-s -w"` を推奨します。

- `manifest.json` の定義に従い、取得 → 変換 → 差分表の作成までを1回で作ります。
- マニュアルの変換結果は `~/.ix-toolkit/manuals/` 以下に出力されます。
- PDF 版マニュアル（無印の設定事例集など）が手元にある場合は `pdf/` ディレクトリに配置しておくと、ダウンロードをスキップして変換します。
- 2回目以降の実行では、取得済みの資料は再取得せずに変換のみを行います。
- GET の一時的な通信失敗と HTTP 429 / 500 / 502 / 503 / 504 は最大 3 回試します。再試行待ちは通常 2 秒・4 秒で、429 / 503 の `Retry-After` と取得間隔を守ります。`Retry-After` が 30 秒を超える場合は失敗にします。
- `fetch` / `build` は Ctrl-C で HTTP と待機を中断し、起動済みの取得処理と一時ファイルの後始末を終えてから終了します。再取得に失敗した Web キャッシュは、以前の本文を保ち、未完了として次回取り直します。
- PDF のページ画像は、原本の SHA-256・DPI・全画像の存在を確認して再利用します。古い形式の生成済み印は初回に作り直します。

### 変換結果の構成

出力されたデータは以下の構造で配置され、エージェントが自己解決のために参照します。

- **`ix/`** (IX2000/IX3000系): `crm` (コマンドリファレンス), `fd` (機能説明書), `ex` (設定事例集)
- **`ix-r/`** (IX-R/IX-V系): `crm`, `fd`, `ex`, `slog` (syslog リファレンス)
- **`ix-r/diff.tsv`**: 無印と IX-R のコマンド対応表

PDF由来の図表はテキスト構造として復元されるか、必要に応じてページごと画像（PNG）として保存され、Markdown内に埋め込まれます。

---

## 開発

開発環境、検証手順、静的検査の方針、マニュアル変換の個別実行は [開発ガイド](docs/develop.md) を参照してください。

## ライセンス

MIT。バイナリに含まれる [PDFium](https://pdfium.googlesource.com/pdfium/) は Apache-2.0。
