package application

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/yuu61/ix-toolkit/internal/manualbook/domain"
	"github.com/yuu61/ix-toolkit/internal/manualbook/infrastructure"
)

// Diff は両系列の変換結果から diff.tsv を書く。行の出どころ (ch8 / derived / setdiff)
// と列の意味は domain/comparison.go に書いてある。
//
// out が空なら <ix-r>/diff.tsv、derived が空ならカレント → 実行ファイルの隣の順に探す。
func Diff(ixDir, ixrDir, out, derived string) error {
	if out == "" {
		out = filepath.Join(ixrDir, "diff.tsv")
	}

	ixCmds, err := infrastructure.ReadCommandIndex(infrastructure.CommandIndexPath(ixDir))
	if err != nil {
		return err
	}
	ixrCmds, err := infrastructure.ReadCommandIndex(infrastructure.CommandIndexPath(ixrDir))
	if err != nil {
		return err
	}

	ch8, err := infrastructure.ReadCh8(infrastructure.FDDir(ixrDir), ixCmds)
	if err != nil {
		return err
	}
	fmt.Printf("ch8:     %d 行 (IX-R 機能説明書「IXシリーズとの差分」)\n", len(ch8))

	var hand []domain.DiffRow
	path := derived
	if path == "" {
		path = infrastructure.FindDerived()
	}
	if path != "" {
		if hand, err = infrastructure.ReadDerived(path); err != nil {
			return err
		}
		fmt.Printf("derived: %d 行 (%s)\n", len(hand), path)
	} else {
		// 黙って 0 行にすると、できた diff.tsv は欠けているのに完全に見える。
		fmt.Fprintln(os.Stderr, "⚠ derived: profiles/ix-r-derived-diff.tsv が見つからない (カレントにも実行ファイルの隣にも無い)。")
		fmt.Fprintln(os.Stderr, "  リポジトリの外から流すときは -derived <リポジトリ>/profiles/ix-r-derived-diff.tsv を付ける。")
		fmt.Println("derived: 0 行")
	}

	known := append(append([]domain.DiffRow{}, ch8...), hand...)
	sd := domain.SetDiff(ixCmds, ixrCmds, known)
	fmt.Printf("setdiff: %d 族 (無印にあって IX-R の索引に無いコマンド群)\n", len(sd))

	rows := append(known, sd...)
	if err := infrastructure.WriteDiffTSV(out, rows); err != nil {
		return err
	}
	fmt.Printf("出力しました: %s (%d 行)\n", out, len(rows))
	return nil
}
