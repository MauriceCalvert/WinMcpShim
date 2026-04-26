//go:build windows

package shared

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// VerifyPathByHandle opens the path, calls GetFinalPathNameByHandle to resolve
// all symlinks/junctions, and verifies the resolved path is within allowed roots (§8.1).
func VerifyPathByHandle(path string, allowedRoots []string) error {
	if len(allowedRoots) == 0 {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		if IsNotExist(err) {
			return nil // Pre-check passed; downstream code handles not-found.
		}
		return err
	}
	defer f.Close()
	h := windows.Handle(f.Fd())
	buf := make([]uint16, 1024)
	// VOLUME_NAME_DOS = 0x0
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return fmt.Errorf("GetFinalPathNameByHandle: %w", err)
	}
	realPath := windows.UTF16ToString(buf[:n])
	realPath = strings.TrimPrefix(realPath, `\\?\`)
	return CheckResolvedPathConfinement(realPath, allowedRoots)
}

// VerifyCommandByHandle checks that an absolute command path resolves (after symlinks)
// to a location within allowed roots (§8.1).
func VerifyCommandByHandle(path string, allowedRoots []string) error {
	return VerifyPathByHandle(path, allowedRoots)
}

// ToLongPath expands any 8.3 short-name components in path to their long form.
// CI runners set TEMP to C:\Users\RUNNER~1\... while GetFinalPathNameByHandle
// returns the long form (C:\Users\runneradmin\...), so confinement comparisons
// must normalise both sides to the same form.
//
// GetLongPathName only works for existing paths, so for paths whose leaf
// components do not yet exist we walk up to the deepest existing ancestor,
// normalise that, and re-append the remaining suffix.
func ToLongPath(path string) string {
	if path == "" {
		return path
	}
	if long, ok := tryGetLongPath(path); ok {
		return long
	}
	dir := path
	var suffix string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		base := filepath.Base(dir)
		if suffix == "" {
			suffix = base
		} else {
			suffix = filepath.Join(base, suffix)
		}
		dir = parent
		if long, ok := tryGetLongPath(dir); ok {
			return filepath.Join(long, suffix)
		}
	}
}

func tryGetLongPath(path string) (string, bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, 1024)
	n, err := windows.GetLongPathName(p, &buf[0], uint32(len(buf)))
	if err != nil {
		return "", false
	}
	if int(n) > len(buf) {
		buf = make([]uint16, n)
		n, err = windows.GetLongPathName(p, &buf[0], uint32(len(buf)))
		if err != nil {
			return "", false
		}
	}
	return windows.UTF16ToString(buf[:n]), true
}
