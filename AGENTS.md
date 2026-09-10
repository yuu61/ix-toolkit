# AGENTS.md

このリポジトリで作業するときの前提。使い方は README.md を見る。

## 構成

| ディレクトリ | 中身 | 言語 |
|---|---|---|
| `skills/` | SKILL.md 形式の skill 5 つ | Markdown |
| `src/ix_ssh/` | skill が呼ぶ `ix-ssh` コマンド | Python (netmiko / paramiko) |
| `cmd/manualbook/` | PDF / Web マニュアル → Markdown 変換、系列間差分 | Go (PDFium, x/net/html) |
| `profiles/` | manualbook の変換プロファイルと、手で導いた系列間差分 (`ix-r-derived-diff.tsv`) | JSON / TSV |
| `manifest.json` | 取得する資料の一覧 (系列・種別・版・URL) | JSON |

```console
$ go build -ldflags="-s -w" -o manualbook ./cmd/manualbook   # -s -w は Defender の誤検知回避で必須
$ ruff check src/ && ruff format src/
```

## skill を書き換えるときの決まり

skill は Claude Code を主に、Codex など SKILL.md を読む他のエージェントでも同じように動く形を保つ。
本文に**特定のエージェントの記法を書かない**。

- `$ARGUMENTS` を使わない。置換するのは Claude Code だけで、Codex は置換しない。対象は「ユーザーの
  依頼から読み取る」と書く。
- 他の skill は `/ix-show` ではなく `ix-show` と名前で参照する（`/` は Claude Code、`$` は Codex の
  呼び出し記法）。
- ツール名を単独で要求しない。`Read` / `Grep` / `Glob` を例として書くのは可。ただし**やること**が
  本体で、シェルでも同じことができると分かるように書く。
- frontmatter は `name` と `description` が必須。`argument-hint` / `allowed-tools` /
  `compatibility` / `license` は Claude Code 向けで、読まないエージェントは黙って無視する
  （Codex は `name` / `description` / `metadata.short-description` だけを読む）。
- パスをエージェントの home に置かない。インベントリは `~/.ix-toolkit/devices.json`（場所は
  `ix-ssh --list` が表示する）、マニュアルは `$IX_MANUALS` → `~/.ix-toolkit/manuals/` →
  `~/.claude/ix-manuals/` の順に探し、その下が `<系列>/<冊子>/`（`ix/crm`, `ix-r/fd` …）。
  系列間の対応表は `<manuals>/ix-r/diff.tsv`。
- 系列は 2 つ（`ix` = IX2000/IX3000、`ix-r` = IX-R/IX-V）でコマンドが違う。skill は系列を
  インベントリの `model` → 会話 → ユーザーに聞く、の順で決める。`model` はヒントであって
  ゲートではない（無くても止まらない）。系列で変わるコマンド名（`show logging` / `show syslog` 等）を
  skill に書くときは両方を併記し、`ix-manual` の `diff.tsv` の項と食い違わせない。

## リポジトリに入れないもの

- 機器の資格情報とインベントリ（`~/.ix-toolkit/devices.json`）。
- マニュアル本文・変換結果・ページ画像・Web の取得キャッシュ（`pdf/`）。NEC の著作物で、各自の
  手元で取得・変換する。定期的に取りに行く仕組みも作らない（各自が最初に 1 回取り、NEC の
  更新情報を見て自分で `manifest.json` を直して取り直す）。
