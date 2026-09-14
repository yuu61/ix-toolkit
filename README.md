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

[uv](https://docs.astral.sh/uv/) を使用して、リポジトリ内の `ix-ssh` コマンドを PATH にインストールします。

**Claude Code の場合:**
```console
$ git clone https://github.com/yuu61/ix-toolkit $HOME/.claude/skills/ix-toolkit
$ uv tool install -e $HOME/.claude/skills/ix-toolkit
```
※ **`gh skill install` は使わないでください**（ソースコードが含まれないため動作しません）。
※ アップデートは `git pull` のみで反映されます。

**Codex などの他のエージェントの場合:**
クローン先を対象エージェントの skill ディレクトリ（例: `~/.codex/skills/ix-toolkit`）に変更し、同様に `uv tool install -e $HOME/.codex/skills/ix-toolkit` を実行してください。

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
$ go build -ldflags="-s -w" -o manualbook ./cmd/manualbook
$ ./manualbook build
```
※ Windows Defender の誤検知を避けるため `-ldflags="-s -w"` を推奨します。

- `manifest.json` の定義に従い、取得 → 変換 → 差分表の作成までを1回で作ります。
- マニュアルの変換結果は `~/.ix-toolkit/manuals/` 以下に出力されます。
- PDF 版マニュアル（無印の設定事例集など）が手元にある場合は `pdf/` ディレクトリに配置しておくと、ダウンロードをスキップして変換します。
- 2回目以降の実行では、取得済みの資料は再取得せずに変換のみを行います。

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

## ライセンス

MIT。バイナリに含まれる [PDFium](https://pdfium.googlesource.com/pdfium/) は Apache-2.0。
