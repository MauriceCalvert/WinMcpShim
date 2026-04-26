//go:build windows

package main

import (
	"errors"
	"syscall"
)

// isSharingViolation returns true if the error is a Windows sharing violation.
func isSharingViolation(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == 32 // ERROR_SHARING_VIOLATION
	}
	return false
}
