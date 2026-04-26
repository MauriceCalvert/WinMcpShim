package shared

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ConfinementError is returned by path confinement checks (§9.15.1).
// IsCritical is true only for post-check failures (symlink/junction escape).
type ConfinementError struct {
	Message    string
	IsCritical bool
}

func (e *ConfinementError) Error() string {
	return e.Message
}

// CheckPathConfinement performs the pre-check: verify that the cleaned absolute
// path falls within an allowed root (§8.1 step 1).
func CheckPathConfinement(path string, allowedRoots []string) error {
	if len(allowedRoots) == 0 {
		return nil
	}
	return CheckResolvedPathConfinement(path, allowedRoots)
}

// CheckPathConfinementFull performs both pre-check and post-check (§8.1, §9.15.1).
// Pre-check failures return a normal error. Post-check failures (symlink/junction escape)
// return a ConfinementError with IsCritical=true.
func CheckPathConfinementFull(path string, allowedRoots []string) error {
	if err := CheckPathConfinement(path, allowedRoots); err != nil {
		return err
	}
	if err := VerifyPathByHandle(path, allowedRoots); err != nil {
		return &ConfinementError{
			Message:    "path confinement breach: " + path + " escapes allowed roots via symlink/junction",
			IsCritical: true,
		}
	}
	return nil
}

// CheckResolvedPathConfinement checks whether a resolved path is within allowed roots.
// Both the path and each root are normalised to long form on Windows so 8.3
// aliases (e.g. C:\Users\RUNNER~1) compare equal to their long names.
func CheckResolvedPathConfinement(path string, allowedRoots []string) error {
	canonical, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path %s: %w", path, err)
	}
	canonical = filepath.Clean(ToLongPath(canonical))
	canonicalLower := strings.ToLower(canonical)
	for _, root := range allowedRoots {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rootAbs = filepath.Clean(ToLongPath(rootAbs))
		rootLower := strings.ToLower(rootAbs)
		if !strings.HasSuffix(rootLower, string(filepath.Separator)) {
			rootLower += string(filepath.Separator)
		}
		if strings.HasPrefix(canonicalLower, rootLower) || canonicalLower == strings.TrimSuffix(rootLower, string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("Path not within allowed directories: %s\nAllowed roots: %s",
		path, strings.Join(allowedRoots, ", "))
}

// CheckCommandConfinement verifies a command is within allowed roots or on PATH (§8.1).
func CheckCommandConfinement(command string, allowedRoots []string) error {
	if len(allowedRoots) == 0 {
		return nil
	}
	if filepath.IsAbs(command) {
		if err := CheckPathConfinement(command, allowedRoots); err != nil {
			return err
		}
		return VerifyCommandByHandle(command, allowedRoots)
	}
	// Unqualified name resolved via PATH is allowed regardless of location
	// (spec §8.1: system executables live outside allowed roots; the user
	// already controls PATH).
	if _, err := exec.LookPath(command); err == nil {
		return nil
	}
	return fmt.Errorf("Path not within allowed directories: %s\nAllowed roots: %s",
		command, strings.Join(allowedRoots, ", "))
}

// hasControlChar reports whether s contains any C0 control byte (NUL, BEL, BS,
// HT, LF, VT, FF, CR, etc.). Such bytes never appear in legitimate Windows
// paths and often indicate parser corruption or NUL-truncation attacks.
func hasControlChar(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// looksLikeUnsubstitutedPlaceholder catches a literal "${user_config.X}" or
// similar reaching the shim — which means the host failed to substitute the
// value. filepath.Abs would silently rebase such a string against cwd, so we
// reject it as a configuration error instead of granting cwd-rooted access.
func looksLikeUnsubstitutedPlaceholder(s string) bool {
	return strings.Contains(s, "${")
}

// SanitizeAllowedRoots normalises and validates the allowed_roots list before
// it reaches the runtime confinement checks. It is the single chokepoint for
// values arriving from any source (shim.toml, WINMCPSHIM_ALLOWED_ROOTS env
// var, mcpb args). For each entry it: trims whitespace; rejects empties,
// unsubstituted ${user_config.X} placeholders, control characters, and
// non-absolute paths (which filepath.Abs would otherwise silently rebase
// against cwd, granting access the user did not authorise); resolves
// surviving entries to absolute long-form paths; deduplicates case-
// insensitively while preserving order. Drops are surfaced as warnings so
// silent rejection is visible at startup.
func SanitizeAllowedRoots(roots []string) ([]string, []string) {
	cleaned := make([]string, 0, len(roots))
	var warnings []string
	seen := make(map[string]bool)
	for _, raw := range roots {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			warnings = append(warnings, "allowed_roots: dropping empty/whitespace entry")
			continue
		}
		if looksLikeUnsubstitutedPlaceholder(entry) {
			warnings = append(warnings, fmt.Sprintf("allowed_roots: dropping %q (unsubstituted placeholder; check Claude Desktop user_config or shim.toml)", raw))
			continue
		}
		if hasControlChar(entry) {
			warnings = append(warnings, "allowed_roots: dropping entry containing control characters")
			continue
		}
		if !filepath.IsAbs(entry) {
			warnings = append(warnings, fmt.Sprintf("allowed_roots: dropping %q (must be absolute path; relative paths would silently rebase against cwd)", raw))
			continue
		}
		abs, err := filepath.Abs(entry)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("allowed_roots: dropping %q (%v)", raw, err))
			continue
		}
		abs = filepath.Clean(ToLongPath(abs))
		key := strings.ToLower(abs)
		if seen[key] {
			continue
		}
		seen[key] = true
		cleaned = append(cleaned, abs)
	}
	return cleaned, warnings
}

// ResolveCommandPath returns the absolute, normalised path of a command for
// permission checks and allowlist matching. The input is accepted in two forms
// only: a bare basename (no path separator), which is resolved via PATH; or
// an absolute path, which must point to a regular file that currently exists.
// Relative paths with separators (e.g. "bin\foo.exe") are rejected because
// filepath.Abs would silently rebase them against cwd. Control characters and
// unsubstituted placeholders are also rejected.
func ResolveCommandPath(command string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("empty command")
	}
	if looksLikeUnsubstitutedPlaceholder(command) {
		return "", fmt.Errorf("contains unsubstituted placeholder: %s", command)
	}
	if hasControlChar(command) {
		return "", fmt.Errorf("contains control characters")
	}
	candidate := command
	bare := !filepath.IsAbs(candidate) && !strings.ContainsAny(candidate, `/\`)
	if bare {
		resolved, err := exec.LookPath(candidate)
		if err != nil {
			return "", fmt.Errorf("not on PATH: %s", candidate)
		}
		candidate = resolved
	} else if !filepath.IsAbs(candidate) {
		return "", fmt.Errorf("must be absolute path or bare name (relative paths with separators are rejected): %s", command)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", command, err)
	}
	abs = filepath.Clean(ToLongPath(abs))
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("not found: %s", command)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file: %s", command)
	}
	return abs, nil
}

// ResolveAllowedCommands turns the user-supplied run allowlist into a list of
// fully-qualified executable paths suitable for exact matching. Bare names
// (e.g. "powershell") are resolved on PATH; entries already containing a path
// separator are normalised but otherwise trusted. Entries that fail to resolve
// are dropped and surfaced via the returned warnings so the operator sees the
// silent rejection at startup. The output preserves input order minus drops.
func ResolveAllowedCommands(entries []string) ([]string, []string) {
	resolved := make([]string, 0, len(entries))
	var warnings []string
	seen := make(map[string]bool)
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		path, err := ResolveCommandPath(entry)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("run allowlist: dropping %q (%v)", raw, err))
			continue
		}
		key := strings.ToLower(path)
		if seen[key] {
			continue
		}
		seen[key] = true
		resolved = append(resolved, path)
	}
	return resolved, warnings
}

// CheckRunPermission gates the run tool when an allowlist is configured.
// The command is permitted if it resolves either to a path inside allowedRoots
// (so the user's declared roots double as execute permission) or to one of the
// pre-resolved allowedCommandPaths (exact match, case-insensitive on Windows).
// Callers MUST pass a list already produced by ResolveAllowedCommands; bare
// names will not match here. An empty allowlist falls back to legacy directory
// confinement via CheckCommandConfinement and is not handled by this function.
func CheckRunPermission(command string, allowedRoots []string, allowedCommandPaths []string) error {
	resolved, err := ResolveCommandPath(command)
	if err != nil {
		return fmt.Errorf("Command %q not permitted: %v", command, err)
	}
	if len(allowedRoots) > 0 {
		if err := CheckResolvedPathConfinement(resolved, allowedRoots); err == nil {
			return nil
		}
	}
	resolvedLower := strings.ToLower(resolved)
	for _, allowed := range allowedCommandPaths {
		if strings.ToLower(allowed) == resolvedLower {
			return nil
		}
	}
	return fmt.Errorf("Command %q not permitted: not inside allowed_roots and not on the run allowlist (%s)",
		command, strings.Join(allowedCommandPaths, ", "))
}
