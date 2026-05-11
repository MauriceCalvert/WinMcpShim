//go:build !windows

package shared

import "fmt"

// AugmentAllowedCommandsWithGit is a no-op on non-Windows builds.
func AugmentAllowedCommandsWithGit(existing []string) []string {
	return existing
}

// FindGitForWindows returns an error on non-Windows builds. Provided so
// cross-package callers compile on every platform; only the Windows build
// performs real discovery.
func FindGitForWindows() (string, error) {
	return "", fmt.Errorf("Git for Windows discovery is Windows-only")
}
