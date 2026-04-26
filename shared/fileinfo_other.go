//go:build !windows

package shared

import (
	"os"
	"time"
)

// FileCreationTime returns modification time on non-Windows platforms.
func FileCreationTime(info os.FileInfo) string {
	return info.ModTime().Format(time.RFC3339)
}
