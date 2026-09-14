package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"time"
)

// icaclsTimeout はホームがネットワーク上にあって icacls が固まった場合の上限。
const icaclsTimeout = 10 * time.Second

// InitInventory は ~/.ix-toolkit/devices.json が存在しない場合に初期の空ファイルを作成する。
func InitInventory() (bool, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, "", nil
	}
	toolkitDir := filepath.Join(home, ".ix-toolkit")
	if err := os.MkdirAll(toolkitDir, 0o755); err != nil {
		return false, "", fmt.Errorf("インベントリのディレクトリを作れません: %v", err)
	}
	invPath := filepath.Join(toolkitDir, "devices.json")
	if _, err := os.Stat(invPath); os.IsNotExist(err) {
		if err := os.WriteFile(invPath, []byte("{\n  \"devices\": {}\n}\n"), 0o600); err != nil {
			return false, "", fmt.Errorf("インベントリファイルを作れません: %v", err)
		}
		// 0o600 は Windows では読み取り専用属性にしか効かないので DACL で本人のみに絞る。
		// ファイル自体はできているので、失敗しても作成済み (true, path) として返す。
		if runtime.GOOS == "windows" {
			if err := restrictToOwner(invPath); err != nil {
				return true, invPath, fmt.Errorf("インベントリファイル %s を作りましたが、本人のみに制限できません: %w", invPath, err)
			}
		}
		return true, invPath, nil
	}
	return false, invPath, nil
}

// restrictToOwner は継承 ACE を外し、実行中のユーザーだけにフルアクセスを与える。
// 環境変数 USERNAME ではなくトークンの SID (*S-1-5-…) を渡すので名前解決に依存しない
// (空の名前を渡すと icacls は BUILTIN に与えて本人を締め出す)。
func restrictToOwner(path string) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	grant := "*" + u.Uid + ":F"
	ctx, cancel := context.WithTimeout(context.Background(), icaclsTimeout)
	defer cancel()
	//nolint:gosec // G204: 引数は自前で組み立てたパスと SID のみで、外部入力は含まない
	out, err := exec.CommandContext(ctx, "icacls", path, "/inheritance:r", "/grant:r", grant).CombinedOutput()
	if err != nil {
		return fmt.Errorf("icacls: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}
