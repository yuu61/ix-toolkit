# 開発ガイド

コマンドはリポジトリのルートで実行します。

## 開発環境

GNU Make、`uv`、`go.mod` に対応する Go、`golangci-lint` が PATH に必要です。
Windows の開発環境は MSYS2 と PowerShell 7 を前提とします。

各 OS で同じ Makefile を使い、Python 3.10 / 3.14 を既定で検証します。対象は `PYTHON_VERSIONS` で指定できます。
Makefile に OS ごとのシェル選択や Python バージョンの自動分岐を追加しないでください。

## 検証

検証は `make check` でまとめて実行します。

```console
make check
```

| コマンド | 内容 |
| --- | --- |
| `make check` | 静的検査、Python のバージョン別テスト、Go テスト・ビルド、新規インストールの統合テスト |
| `make lint` | Ruff の check / format 検査と golangci-lint |
| `make test` | Python のバージョン別テストと Go テスト |
| `make test-python PYTHON=3.10` | 指定バージョンの Python テスト |
| `make test-python-matrix PYTHON_VERSIONS="3.10 3.14"` | 指定した複数の Python バージョンを検証 |
| `make test-go` / `make build` | Go テスト / ビルド（Windows 向けは `-ldflags="-s -w"` を付与） |
| `make test-fresh` | lock を使わない新規の editable tool install で偽の IX を操作 |

Python は `.make/python-<version>/` に lock を使った環境を作り、通常の `.venv` と分けます。
新規インストールは `.make/` 内の一時ディレクトリに tool 環境と実行ファイルを作り、終了時に削除するため、普段使う tool 環境を変更しません。
`UV`、`GO`、`GOLANGCI_LINT`、`PYTHON`、`PYTHON_VERSIONS`、`FRESH_PYTHON` は make の引数で変更できます。
SSH 統合テストと Windows の DACL テストの必須部分は skip を成功扱いにしません。NEC の資料は取得せず、合成 PDF とローカル HTTP を使います。

### 個別の検証・整形

通常の `.venv` を使って個別に確認する場合は、次のコマンドを実行します。

```console
uv sync --extra dev
uv run ruff check src/ tests/
uv run ruff format --check src/ tests/
uv run python -m unittest
golangci-lint run ./...
go test ./...
```

整形を適用するときは `uv run ruff format src/ tests/` を実行します。

### 静的検査の方針

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

## マニュアル変換の確認

```console
make build
./manualbook build
```

`make build` は Go の対象 OS で判定し、Windows 向けでは Defender の誤検知を避けるために必須の `-ldflags="-s -w"` を付けます。他の OS 向けでは通常の `go build` を実行します。
Windows では `manualbook.exe` ができるので、PowerShell 7 では `.\manualbook.exe build` と実行します。
`-o manualbook` は Windows で `.exe` が付かないため指定しません。

変換処理の一部だけをやり直す場合は、次のサブコマンドを使用できます。

```text
manualbook fetch   資料をまとめて取得する (PDF / Web)
manualbook probe   段組み等の自動較正・プロファイル作成 (PDF)
manualbook md      取得済みデータを Markdown に変換
manualbook diff    無印と IX-R のコマンド対応表を作成
manualbook figures PDF のページを PNG に画像化する
manualbook scan    見開き画像を1ページずつに分割
```

実際の NEC マニュアルを使う変換の確認は、資料を持つ開発者の手元で実施します。
マニュアル本文・変換結果・ページ画像・取得キャッシュはリポジトリに入れません。
