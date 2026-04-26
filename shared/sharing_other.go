//go:build !windows

package shared

// IsSharingViolation always returns false on non-Windows platforms.
func IsSharingViolation(err error) bool {
	return false
}
