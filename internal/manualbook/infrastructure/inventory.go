package infrastructure

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		if err := os.WriteFile(invPath, []byte("{\n  \"devices\": {}\n}\n"), 0o600); err != nil {
			return false, "", fmt.Errorf("インベントリファイルを作れません: %v", err)
		}
		if runtime.GOOS == "windows" {
			if username := os.Getenv("USERNAME"); username != "" {
				exec.Command("icacls", invPath, "/inheritance:r", "/grant:r", username+":F").Run()
			}
		}
		return true, invPath, nil
	}
	return false, invPath, nil
}
