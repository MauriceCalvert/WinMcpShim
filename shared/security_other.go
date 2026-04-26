//go:build !windows

package shared

// VerifyPathByHandle is a no-op on non-Windows platforms.
func VerifyPathByHandle(path string, allowedRoots []string) error {
	return nil
}

// VerifyCommandByHandle is a no-op on non-Windows platforms.
func VerifyCommandByHandle(path string, allowedRoots []string) error {
	return nil
}

// ToLongPath is a no-op on non-Windows platforms (no 8.3 aliasing).
func ToLongPath(path string) string {
	return path
}
