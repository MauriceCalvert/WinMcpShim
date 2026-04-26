//go:build !windows

package main

import (
	"fmt"
	"os"
)

// doLock is not supported on non-Windows platforms.
func doLock(path string) {
	fmt.Fprintln(os.Stderr, "lock mode requires Windows")
	os.Exit(1)
}
