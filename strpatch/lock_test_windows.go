//go:build windows

package main

import (
	"os"
	"syscall"
	"testing"
)

// lockFileExclusive opens a file with no sharing, preventing other processes from reading.
func lockFileExclusive(t *testing.T, path string) *os.File {
	t.Helper()
	pathp, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("utf16 path: %v", err)
	}
	h, err := syscall.CreateFile(
		pathp,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		0, // dwShareMode = 0 → exclusive
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatalf("exclusive open %s: %v", path, err)
	}
	return os.NewFile(uintptr(h), path)
}
