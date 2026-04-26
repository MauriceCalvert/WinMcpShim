//go:build windows

package shared

import (
	"errors"
	"syscall"
)

// IsSharingViolation returns true if the error is a Windows sharing violation.
func IsSharingViolation(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == 32 // ERROR_SHARING_VIOLATION
	}
	return false
}
