//go:build !windows

package main

// isSharingViolation always returns false on non-Windows platforms.
func isSharingViolation(err error) bool {
	return false
}
