//go:build windows

package shared

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// AugmentAllowedCommandsWithGit appends <gitRoot>\cmd\git.exe to existing if
// Git for Windows is discoverable on the system and the path is not already
// present (case-insensitive). Idempotent and additive only: any failure
// (Git not installed, git.exe missing, already on the list) returns existing
// unchanged. Called from shim startup so the run tool can invoke `git` by
// bare name without the user having to add Git to their MCPB allowed-roots
// or allowed-commands.
func AugmentAllowedCommandsWithGit(existing []string) []string {
	gitRoot, err := FindGitForWindows()
	if err != nil {
		return existing
	}
	gitExe := filepath.Join(gitRoot, "cmd", "git.exe")
	if _, err := os.Stat(gitExe); err != nil {
		return existing
	}
	gitExeLower := strings.ToLower(gitExe)
	for _, e := range existing {
		if strings.ToLower(e) == gitExeLower {
			return existing
		}
	}
	return append(existing, gitExe)
}

// FindGitForWindows locates the Git for Windows installation.
// Discovery order: registry, where.exe grep, common install paths.
// Returns the install root (e.g. C:\Program Files\Git) on success.
func FindGitForWindows() (string, error) {
	// Step 1: HKLM\SOFTWARE\GitForWindows\InstallPath.
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\GitForWindows`, registry.QUERY_VALUE)
	if err == nil {
		defer key.Close()
		val, _, err := key.GetStringValue("InstallPath")
		if err == nil && gitDirExists(filepath.Join(val, "usr", "bin")) {
			return val, nil
		}
	}
	// Step 2: where.exe grep — derive Git root as great-grandparent of the
	// usr\bin\grep.exe entry it returns.
	out, err := exec.Command("where.exe", "grep").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			lower := strings.ToLower(line)
			if strings.Contains(lower, `\usr\bin\grep.exe`) {
				gitRoot := filepath.Dir(filepath.Dir(filepath.Dir(line)))
				if gitDirExists(filepath.Join(gitRoot, "usr", "bin")) {
					return gitRoot, nil
				}
			}
		}
	}
	// Step 3: Common install paths.
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Git"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Git"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git"),
		`C:\Git`,
	}
	for _, c := range candidates {
		if gitDirExists(filepath.Join(c, "usr", "bin")) {
			return c, nil
		}
	}
	return "", fmt.Errorf("Git for Windows not found")
}

// gitDirExists returns true if path exists and is a directory.
// Named distinctly from any installer-package helper to avoid confusion.
func gitDirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
