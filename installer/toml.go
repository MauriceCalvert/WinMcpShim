package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// allowedRootsRe matches the entire allowed_roots = [ ... ] block including newlines.
var allowedRootsRe = regexp.MustCompile(`(?ms)^(allowed_roots\s*=\s*)\[.*?\]`)

// allowedCommandsRe matches the entire allowed_commands = [ ... ] block including newlines.
var allowedCommandsRe = regexp.MustCompile(`(?ms)^(allowed_commands\s*=\s*)\[.*?\]`)

// runSectionRe matches the [run] section header.
var runSectionRe = regexp.MustCompile(`(?m)^\[run\]\s*$`)

// GetTomlState classifies the state of shim.toml (INS-08, INS-08a, INS-08b).
func GetTomlState(path string) TomlState {
	data, err := os.ReadFile(path)
	if err != nil {
		return TomlMissing
	}
	if strings.Contains(string(data), "CHANGE_ME") {
		return TomlUnconfigured
	}
	return TomlConfigured
}

// SetAllowedRoots replaces the allowed_roots block in content with the given roots (INS-10).
// Returns the modified content. The caller handles file I/O.
func SetAllowedRoots(content string, roots []string) (string, error) {
	if !allowedRootsRe.MatchString(content) {
		return "", fmt.Errorf("allowed_roots block not found in shim.toml")
	}
	replacement := "allowed_roots = " + FormatTomlRoots(roots)
	result := allowedRootsRe.ReplaceAllString(content, replacement)
	return result, nil
}

// SetAllowedCommands replaces (or inserts) the allowed_commands block in the [run]
// section. If the block exists it is replaced; otherwise it is appended inside
// the [run] section. If [run] itself is missing an error is returned.
func SetAllowedCommands(content string, cmds []string) (string, error) {
	replacement := "allowed_commands = " + FormatTomlStrings(cmds)
	if allowedCommandsRe.MatchString(content) {
		return allowedCommandsRe.ReplaceAllString(content, replacement), nil
	}
	loc := runSectionRe.FindStringIndex(content)
	if loc == nil {
		return "", fmt.Errorf("[run] section not found in shim.toml")
	}
	// Insert immediately after the [run] header line.
	headerEnd := loc[1]
	// Advance past the newline that follows [run] (handle both LF and CRLF).
	if headerEnd < len(content) && content[headerEnd] == '\r' {
		headerEnd++
	}
	if headerEnd < len(content) && content[headerEnd] == '\n' {
		headerEnd++
	}
	return content[:headerEnd] + replacement + "\n" + content[headerEnd:], nil
}

// FormatTomlStrings formats a list of basenames as a TOML string array.
func FormatTomlStrings(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteString("[")
	for i, s := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", s)
	}
	b.WriteString("]")
	return b.String()
}

// SetGitPaths replaces the default Git usr\bin path with the discovered path (INS-11, INS-12).
// Handles both TOML-escaped (exe = "...") and scan_dirs forms.
func SetGitPaths(content string, gitUsrBin string) (string, error) {
	oldEscaped := `C:\\Program Files\\Git\\usr\\bin`
	newEscaped := strings.ReplaceAll(gitUsrBin, `\`, `\\`)
	result := strings.ReplaceAll(content, oldEscaped, newEscaped)
	return result, nil
}

// ValidateToml parses content as TOML and returns any syntax error (section 6.4).
func ValidateToml(content string) error {
	var dummy map[string]interface{}
	_, err := toml.Decode(content, &dummy)
	if err != nil {
		return fmt.Errorf("TOML validation failed: %w", err)
	}
	return nil
}

// ValidateRoot normalises and validates a single allowed-root path (INS-09).
// Returns (normalised, error). Normalisation: trim whitespace, remove trailing
// backslash, resolve to absolute, confirm directory exists.
func ValidateRoot(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	path = strings.TrimRight(path, `\`)
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("cannot resolve %q to absolute path: %w", path, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("path %q does not exist: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path %q is not a directory", abs)
	}
	return abs, nil
}

// ValidateRoots validates and deduplicates a list of paths (INS-09, INS-09a, INS-09b).
// Returns (valid, rejected). Deduplication is case-insensitive.
func ValidateRoots(paths []string) ([]string, []string) {
	var valid []string
	var rejected []string
	seen := make(map[string]bool)
	for _, p := range paths {
		norm, err := ValidateRoot(p)
		if err != nil {
			rejected = append(rejected, fmt.Sprintf("%s: %v", p, err))
			continue
		}
		key := strings.ToLower(norm)
		if seen[key] {
			continue // silently deduplicate
		}
		seen[key] = true
		valid = append(valid, norm)
	}
	return valid, rejected
}

// FormatTomlRoots formats roots as a TOML string array with double-backslash escaping (INS-10).
func FormatTomlRoots(roots []string) string {
	var b strings.Builder
	b.WriteString("[\n")
	for _, r := range roots {
		b.WriteString(fmt.Sprintf("    %q,\n", r))
	}
	b.WriteString("]")
	return b.String()
}
