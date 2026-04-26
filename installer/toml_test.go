package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-20: GetTomlState returns TomlMissing for non-existent path (INS-08).
func TestGetTomlState_Missing(t *testing.T) {
	got := GetTomlState(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if got != TomlMissing {
		t.Errorf("GetTomlState = %d, want TomlMissing", got)
	}
}

// T-21: GetTomlState returns TomlUnconfigured when file contains CHANGE_ME (INS-08a).
func TestGetTomlState_Unconfigured(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shim.toml")
	os.WriteFile(path, []byte(`allowed_roots = ["CHANGE_ME"]`), 0644)
	got := GetTomlState(path)
	if got != TomlUnconfigured {
		t.Errorf("GetTomlState = %d, want TomlUnconfigured", got)
	}
}

// T-22: GetTomlState returns TomlConfigured when file has real paths (INS-08b).
func TestGetTomlState_Configured(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shim.toml")
	os.WriteFile(path, []byte(`allowed_roots = ["D:\\projects"]`), 0644)
	got := GetTomlState(path)
	if got != TomlConfigured {
		t.Errorf("GetTomlState = %d, want TomlConfigured", got)
	}
}

// T-23: ValidateRoot accepts an existing directory (INS-09).
func TestValidateRoot_ExistingDir(t *testing.T) {
	dir := t.TempDir()
	got, err := ValidateRoot(dir)
	if err != nil {
		t.Fatalf("ValidateRoot: %v", err)
	}
	if got == "" {
		t.Error("ValidateRoot returned empty string")
	}
}

// T-24: ValidateRoot rejects a non-existent path (INS-09).
func TestValidateRoot_NonExistent(t *testing.T) {
	_, err := ValidateRoot(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Error("expected error for non-existent path")
	}
}

// T-25: ValidateRoot rejects a path that is a file (INS-09).
func TestValidateRoot_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	os.WriteFile(path, []byte("hi"), 0644)
	_, err := ValidateRoot(path)
	if err == nil {
		t.Error("expected error for file path")
	}
}

// T-26: ValidateRoot trims whitespace (INS-09).
func TestValidateRoot_TrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	got, err := ValidateRoot("  " + dir + "  ")
	if err != nil {
		t.Fatalf("ValidateRoot: %v", err)
	}
	if got != dir {
		t.Errorf("got %q, want %q", got, dir)
	}
}

// T-27: ValidateRoot removes trailing backslash (INS-09).
func TestValidateRoot_RemovesTrailingBackslash(t *testing.T) {
	dir := t.TempDir()
	got, err := ValidateRoot(dir + `\`)
	if err != nil {
		t.Fatalf("ValidateRoot: %v", err)
	}
	if strings.HasSuffix(got, `\`) {
		t.Errorf("got %q still has trailing backslash", got)
	}
}

// T-28: ValidateRoot resolves a relative path to absolute (INS-09).
func TestValidateRoot_ResolvesRelative(t *testing.T) {
	got, err := ValidateRoot(".")
	if err != nil {
		t.Fatalf("ValidateRoot: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("got %q is not absolute", got)
	}
}

// T-29: ValidateRoots deduplicates case-insensitive paths (INS-09b).
func TestValidateRoots_DeduplicatesCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	upper := strings.ToUpper(dir)
	valid, _ := ValidateRoots([]string{dir, upper})
	if len(valid) != 1 {
		t.Errorf("got %d valid paths, want 1 (dedup)", len(valid))
	}
}

// T-30: ValidateRoots deduplicates paths differing only by trailing backslash (INS-09b).
func TestValidateRoots_DeduplicatesTrailingBackslash(t *testing.T) {
	dir := t.TempDir()
	valid, _ := ValidateRoots([]string{dir, dir + `\`})
	if len(valid) != 1 {
		t.Errorf("got %d valid paths, want 1 (dedup trailing backslash)", len(valid))
	}
}

// T-31: ValidateRoots returns rejected paths separately (INS-09a).
func TestValidateRoots_ReturnsRejected(t *testing.T) {
	dir := t.TempDir()
	valid, rejected := ValidateRoots([]string{dir, filepath.Join(dir, "nope")})
	if len(valid) != 1 {
		t.Errorf("got %d valid, want 1", len(valid))
	}
	if len(rejected) != 1 {
		t.Errorf("got %d rejected, want 1", len(rejected))
	}
}

const testTomlTemplate = `[security]
allowed_roots = [
    "CHANGE_ME",
]
max_timeout = 60

[scan_dirs]
paths = [
    "C:\\Program Files\\Git\\usr\\bin",
]

[tools.grep]
exe = "C:\\Program Files\\Git\\usr\\bin\\grep.exe"
`

// T-32: SetAllowedRoots replaces CHANGE_ME block with single root (INS-10).
func TestSetAllowedRoots_SingleRoot(t *testing.T) {
	got, err := SetAllowedRoots(testTomlTemplate, []string{`D:\projects`})
	if err != nil {
		t.Fatalf("SetAllowedRoots: %v", err)
	}
	if strings.Contains(got, "CHANGE_ME") {
		t.Error("output still contains CHANGE_ME")
	}
	if !strings.Contains(got, `"D:\\projects"`) {
		t.Error("output does not contain escaped path")
	}
}

// T-33: SetAllowedRoots replaces CHANGE_ME block with multiple roots (INS-10).
func TestSetAllowedRoots_MultipleRoots(t *testing.T) {
	roots := []string{`D:\projects`, `C:\Users\Momo\Documents`}
	got, err := SetAllowedRoots(testTomlTemplate, roots)
	if err != nil {
		t.Fatalf("SetAllowedRoots: %v", err)
	}
	if !strings.Contains(got, `"D:\\projects"`) {
		t.Error("missing first root")
	}
	if !strings.Contains(got, `"C:\\Users\\Momo\\Documents"`) {
		t.Error("missing second root")
	}
}

// T-34: SetAllowedRoots on already-configured content replaces existing roots (INS-10).
func TestSetAllowedRoots_ReplacesExisting(t *testing.T) {
	configured := strings.Replace(testTomlTemplate, `"CHANGE_ME"`, `"D:\\old"`, 1)
	got, err := SetAllowedRoots(configured, []string{`D:\new`})
	if err != nil {
		t.Fatalf("SetAllowedRoots: %v", err)
	}
	if strings.Contains(got, `"D:\\old"`) {
		t.Error("old root still present")
	}
	if !strings.Contains(got, `"D:\\new"`) {
		t.Error("new root missing")
	}
}

// T-35: SetAllowedRoots output has correct TOML double-backslash escaping (INS-10).
func TestSetAllowedRoots_BackslashEscaping(t *testing.T) {
	got, _ := SetAllowedRoots(testTomlTemplate, []string{`C:\Users\Momo\Projects`})
	if !strings.Contains(got, `"C:\\Users\\Momo\\Projects"`) {
		t.Errorf("backslash escaping incorrect in: %s", got)
	}
}

// T-36: SetAllowedRoots preserves all content outside the allowed_roots block (INS-10).
func TestSetAllowedRoots_PreservesOtherContent(t *testing.T) {
	got, _ := SetAllowedRoots(testTomlTemplate, []string{`D:\x`})
	if !strings.Contains(got, "max_timeout = 60") {
		t.Error("max_timeout missing from output")
	}
	if !strings.Contains(got, `[tools.grep]`) {
		t.Error("[tools.grep] section missing from output")
	}
	if !strings.Contains(got, `[scan_dirs]`) {
		t.Error("[scan_dirs] section missing from output")
	}
}

// T-37: SetGitPaths replaces default Git path with discovered path (INS-11).
func TestSetGitPaths_ReplacesDefault(t *testing.T) {
	got, err := SetGitPaths(testTomlTemplate, `D:\Git\usr\bin`)
	if err != nil {
		t.Fatalf("SetGitPaths: %v", err)
	}
	if strings.Contains(got, `C:\\Program Files\\Git\\usr\\bin`) {
		t.Error("default path still present")
	}
	if !strings.Contains(got, `D:\\Git\\usr\\bin`) {
		t.Error("discovered path not present")
	}
}

// T-38: SetGitPaths handles Git installed at path with spaces (INS-11, INV-03).
func TestSetGitPaths_PathWithSpaces(t *testing.T) {
	got, _ := SetGitPaths(testTomlTemplate, `D:\My Programs\Git\usr\bin`)
	if !strings.Contains(got, `D:\\My Programs\\Git\\usr\\bin`) {
		t.Error("path with spaces not correctly escaped")
	}
}

// T-39: SetGitPaths handles Git installed on non-C drive (INS-11).
func TestSetGitPaths_NonCDrive(t *testing.T) {
	got, _ := SetGitPaths(testTomlTemplate, `E:\tools\Git\usr\bin`)
	if !strings.Contains(got, `E:\\tools\\Git\\usr\\bin`) {
		t.Error("non-C drive path not present")
	}
}

// T-40: SetGitPaths updates scan_dirs paths (INS-12).
func TestSetGitPaths_UpdatesScanDirs(t *testing.T) {
	got, _ := SetGitPaths(testTomlTemplate, `D:\Git\usr\bin`)
	// scan_dirs line should have the new path
	if !strings.Contains(got, `"D:\\Git\\usr\\bin"`) {
		t.Error("scan_dirs path not updated")
	}
}

// T-41: SetGitPaths does not modify tar.exe path (System32) (INS-11).
func TestSetGitPaths_PreservesTarPath(t *testing.T) {
	content := testTomlTemplate + "\n" + `exe = "C:\\Windows\\System32\\tar.exe"` + "\n"
	got, _ := SetGitPaths(content, `D:\Git\usr\bin`)
	if !strings.Contains(got, `C:\\Windows\\System32\\tar.exe`) {
		t.Error("tar.exe path was incorrectly modified")
	}
}

// T-42: FormatTomlRoots with one path produces correct TOML (INS-10).
func TestFormatTomlRoots_OnePath(t *testing.T) {
	got := FormatTomlRoots([]string{`D:\projects`})
	expected := "[\n    \"D:\\\\projects\",\n]"
	if got != expected {
		t.Errorf("got:\n%s\nwant:\n%s", got, expected)
	}
}

// T-43: FormatTomlRoots with three paths produces correct TOML (INS-10).
func TestFormatTomlRoots_ThreePaths(t *testing.T) {
	got := FormatTomlRoots([]string{`D:\a`, `D:\b`, `D:\c`})
	lines := strings.Split(got, "\n")
	if len(lines) != 5 { // [ + 3 entries + ]
		t.Errorf("got %d lines, want 5", len(lines))
	}
}

// T-44: FormatTomlRoots escapes backslashes in paths (INS-10).
func TestFormatTomlRoots_EscapesBackslashes(t *testing.T) {
	got := FormatTomlRoots([]string{`C:\Users\Momo\Documents`})
	if !strings.Contains(got, `C:\\Users\\Momo\\Documents`) {
		t.Errorf("backslashes not escaped in: %s", got)
	}
}

// T-45: ValidateToml accepts output of SetAllowedRoots applied to shim.toml.example (section 6.4).
func TestValidateToml_AcceptsSetAllowedRootsOutput(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "shim.toml.example"))
	if err != nil {
		t.Skip("shim.toml.example not found (run from project root)")
	}
	modified, err := SetAllowedRoots(string(data), []string{`D:\projects`})
	if err != nil {
		t.Fatalf("SetAllowedRoots: %v", err)
	}
	if err := ValidateToml(modified); err != nil {
		t.Fatalf("ValidateToml: %v", err)
	}
}

// T-46: ValidateToml rejects a string with unclosed bracket (section 6.4).
func TestValidateToml_RejectsUnclosedBracket(t *testing.T) {
	if err := ValidateToml(`[security`); err == nil {
		t.Error("expected error for unclosed bracket")
	}
}

// T-47: SetGitPaths + ValidateToml round-trip: modified toml is still parseable (INS-11).
func TestSetGitPaths_ValidateToml_RoundTrip(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "shim.toml.example"))
	if err != nil {
		t.Skip("shim.toml.example not found")
	}
	modified, err := SetGitPaths(string(data), `D:\MyGit\usr\bin`)
	if err != nil {
		t.Fatalf("SetGitPaths: %v", err)
	}
	if err := ValidateToml(modified); err != nil {
		t.Fatalf("ValidateToml after SetGitPaths: %v", err)
	}
}

// T-48: Full pipeline: copy shim.toml.example, apply SetAllowedRoots + SetGitPaths, validate (INS-10, INS-11).
func TestFullPipeline_TomlModification(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "shim.toml.example"))
	if err != nil {
		t.Skip("shim.toml.example not found")
	}
	content := string(data)
	content, err = SetAllowedRoots(content, []string{`D:\projects`, `C:\Users\Momo\Documents`})
	if err != nil {
		t.Fatalf("SetAllowedRoots: %v", err)
	}
	content, err = SetGitPaths(content, `D:\Git\usr\bin`)
	if err != nil {
		t.Fatalf("SetGitPaths: %v", err)
	}
	if err := ValidateToml(content); err != nil {
		t.Fatalf("ValidateToml: %v", err)
	}
	if strings.Contains(content, "CHANGE_ME") {
		t.Error("CHANGE_ME still present after full pipeline")
	}
	if strings.Contains(content, `C:\\Program Files\\Git\\usr\\bin`) {
		t.Error("default Git path still present after full pipeline")
	}
}
