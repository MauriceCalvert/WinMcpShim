package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WriteAtomic writes data to path via a temporary file and atomic rename (INV-02).
func WriteAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".install-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp to %s: %w", path, err)
	}
	tmpName = "" // prevent deferred removal
	return nil
}

// BackupFile copies path to path.bak.<timestamp> and returns the backup path (INS-16, UNS-05).
func BackupFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("backup read %s: %w", path, err)
	}
	stamp := time.Now().Format("20060102T150405")
	bakPath := path + ".bak." + stamp
	for i := 0; ; i++ {
		if i >= 1000 {
			return "", fmt.Errorf("backup exhausted 1000 candidates for %s", path)
		}
		candidate := bakPath
		if i > 0 {
			candidate = fmt.Sprintf("%s.%d", bakPath, i)
		}
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			bakPath = candidate
			break
		}
	}
	if err := os.WriteFile(bakPath, data, 0644); err != nil {
		return "", fmt.Errorf("backup write %s: %w", bakPath, err)
	}
	return bakPath, nil
}
