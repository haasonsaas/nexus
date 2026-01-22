package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BackupConfig writes a timestamped copy of the config file and returns its path.
func BackupConfig(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("config path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	backupPath := fmt.Sprintf("%s.bak-%s", path, time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(backupPath, data, info.Mode().Perm()); err != nil {
		return "", err
	}
	return backupPath, nil
}
