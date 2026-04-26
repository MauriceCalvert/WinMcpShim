//go:build windows

package shared

import (
	"os"
	"syscall"
	"time"
)

// FileCreationTime extracts the creation time from a FileInfo on Windows (§5.10).
func FileCreationTime(info os.FileInfo) string {
	sys := info.Sys()
	if sys == nil {
		return info.ModTime().Format(time.RFC3339)
	}
	if attr, ok := sys.(*syscall.Win32FileAttributeData); ok {
		created := time.Unix(0, attr.CreationTime.Nanoseconds())
		return created.Format(time.RFC3339)
	}
	return info.ModTime().Format(time.RFC3339)
}
