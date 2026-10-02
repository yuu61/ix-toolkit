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

- `host` は IP アドレスや `~/.ssh/config` のエイリアスが使用可能です（`ProxyJump` 等も自動で辿ります）。
- 意図しない機器への設定投入を防ぐため、既定の機器は設定できません。エージェントが会話から判断するかユーザーに尋ねます。
- 動作確認は手動で `ix-ssh --list` を実行してください。

### コンフィグモードについて

通常は `enable-config` で入ります。他のユーザーが使用中の場合はエラーになります。
強制取得を明示する場合は `--force-config` を付けて実行します。skill は、ユーザーから「強制的に設定して」など明示された場合のみこのオプションを付与します。

---

## 2. マニュアル変換ツール (`manualbook`)

NEC の公式マニュアル（PDF / Web）を取得し、`ix-manual` skill が読める Markdown と索引に変換する Go 製のツールです。

### 使い方

```console
go build -ldflags="-s -w" ./cmd/manualbook
./manualbook build
```

※ Windows では `manualbook.exe` ができるので、PowerShell では `.\manualbook.exe build` と実行します（`-o manualbook` を付けると `.exe` が付かず、実行できません）。
※ Windows Defender の誤検知を避けるため `-ldflags="-s -w"` を推奨します。

- `manifest.json` の定義に従い、取得 → 変換 → 差分表の作成までを1回で作ります。
- マニュアルの変換結果は `~/.ix-toolkit/manuals/` 以下に出力されます。
- PDF 版マニュアル（無印の設定事例集など）が手元にある場合は `pdf/` ディレクトリに配置しておくと、ダウンロードをスキップして変換します。
- 2回目以降の実行では、取得済みの資料は再取得せずに変換のみを行います。
- PDF のページ画像は、原本の SHA-256・DPI・全画像の存在を確認して再利用します。古い形式の生成済み印は初回に作り直します。

### 変換結果の構成

出力されたデータは以下の構造で配置され、エージェントが自己解決のために参照します。

- **`ix/`** (IX2000/IX3000系): `crm` (コマンドリファレンス), `fd` (機能説明書), `ex` (設定事例集)
- **`ix-r/`** (IX-R/IX-V系): `crm`, `fd`, `ex`, `slog` (syslog リファレンス)
- **`ix-r/diff.tsv`**: 無印と IX-R のコマンド対応表

PDF由来の図表はテキスト構造として復元されるか、必要に応じてページごと画像（PNG）として保存され、Markdown内に埋め込まれます。

### 個別実行 (開発・デバッグ用)
変換処理の一部だけをやり直す場合は、以下のサブコマンドを使用できます。
```
manualbook fetch   資料をまとめて取得する (PDF / Web)
manualbook probe   段組み等の自動較正・プロファイル作成 (PDF)
manualbook md      取得済みデータを Markdown に変換
manualbook diff    無印と IX-R のコマンド対応表を作成
manualbook figures PDF のページを PNG に画像化する
manualbook scan    見開き画像を1ページずつに分割
```

---

## 開発時の検証

```console
uv sync --extra dev
uv run ruff check src/ tests/
uv run ruff format --check src/ tests/
uv run python -m unittest
golangci-lint run ./...
go test ./...
```

Ruff は `pyproject.toml` で検査対象を明示し、0.16.7 以上・0.17 未満で実行します。プレビューの検査は個別に選び、Python 3.10 に対応する構文を基準にします。[公式ルール一覧](https://docs.astral.sh/ruff/rules/)を参照して追加・更新します。

| 主なルール | 検出する問題 |
| --- | --- |
| `B` / `PL` / `RUF` / `C90` | 可変の既定引数、ループ変数の上書き、反復中の変更、複雑な処理 |
| `S` / `BLE` / `TRY` | 秘密の直書き、危険な実行、広すぎる例外捕捉、例外処理の誤り |
| `DTZ` / `PTH` / `PLW1514` | タイムゾーン・パス操作・`open()` の文字コード指定漏れ |
| `I` / `UP` / `PERF` / `ARG` | import の整理、対応構文、非効率な書き方、未使用引数 |
| `TID253` / `PGH` / `RUF100` | SSH ライブラリの早期 import、検査の一括抑制、不要な抑制 |

unittest を pytest に変えるルール、型注釈・docstring の網羅、フォーマッターと競合するルールは選びません。偽の IX の資格情報・コールバック、依存注入の引数、Include の glob、既存のホスト鍵自動受け入れは、設定または該当箇所に理由を付けて例外にしています。新しい例外はルールと範囲を限定します。

`pathlib` とテキストの `subprocess` にも文字コードを指定する規則は unittest で検査します。Go は出力ファイルの `Close()` を含め、無視したエラーを検出します。権限と画像キャッシュの正しさは回帰テストで確認します。

## ライセンス

MIT。バイナリに含まれる [PDFium](https://pdfium.googlesource.com/pdfium/) は Apache-2.0。
