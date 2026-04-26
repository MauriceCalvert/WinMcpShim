//go:build !windows

package main

import (
	"os"
	"testing"
)

// lockFileExclusive is a no-op stub on non-Windows platforms.
func lockFileExclusive(t *testing.T, path string) *os.File {
	t.Skip("exclusive file locking test is Windows-only")
	return nil
}
