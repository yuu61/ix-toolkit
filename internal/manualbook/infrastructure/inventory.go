package infrastructure

import (
	"fmt"
	"os"
	"path/filepath"
)

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
		if err := os.WriteFile(invPath, []byte("{\n  \"devices\": {}\n}\n"), 0o644); err != nil {
			return false, "", fmt.Errorf("インベントリファイルを作れません: %v", err)
		}
		return true, invPath, nil
	}
	return false, invPath, nil
}
