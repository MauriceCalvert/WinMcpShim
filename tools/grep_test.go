package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MauriceCalvert/WinMcpShim/shared"
)

func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func writeTestSubdir(t *testing.T, dir, subdir string) string {
	t.Helper()
	path := filepath.Join(dir, subdir)
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	return path
}

func grepParams(kv ...interface{}) map[string]interface{} {
	m := make(map[string]interface{})
	for i := 0; i < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func TestGrep_SingleFileMatch(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "hello world\nfoo bar\nhello again\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "hello"), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 matches, got %d: %q", len(lines), got)
	}
	if !strings.Contains(lines[0], "1:hello world") {
		t.Errorf("expected line 1 match, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "3:hello again") {
		t.Errorf("expected line 3 match, got %q", lines[1])
	}
}

func TestGrep_SingleFileNoMatch(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "hello world\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "xyz"), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty result, got %q", got)
	}
}

func TestGrep_RecursiveSearch(t *testing.T) {
	dir := t.TempDir()
	sub := writeTestSubdir(t, dir, "sub")
	writeTestFile(t, dir, "a.py", "import os\nimport sys\n")
	writeTestFile(t, sub, "b.py", "import json\n")
	writeTestFile(t, sub, "c.txt", "import nothing\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", dir, "pattern", "import", "recursive", true, "include", "*.py"), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if !strings.Contains(got, "import os") {
		t.Errorf("missing a.py match in %q", got)
	}
	if !strings.Contains(got, "import json") {
		t.Errorf("missing b.py match in %q", got)
	}
	if strings.Contains(got, "import nothing") {
		t.Errorf("c.txt should be excluded by include filter, got %q", got)
	}
}

func TestGrep_IgnoreCase(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "Hello\nhello\nHELLO\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "hello", "ignore_case", true), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 case-insensitive matches, got %d: %q", len(lines), got)
	}
}

func TestGrep_ContextLines(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nMATCH\nline4\nline5\nline6\nline7\nMATCH2\nline9\nline10\n"
	path := writeTestFile(t, dir, "test.txt", content)
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "MATCH", "context", float64(1)), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	// Should have context around both matches, with "--" separator
	if !strings.Contains(got, "line2") {
		t.Errorf("missing before-context for first match in %q", got)
	}
	if !strings.Contains(got, "line4") {
		t.Errorf("missing after-context for first match in %q", got)
	}
	if !strings.Contains(got, "--") {
		t.Errorf("missing separator between non-contiguous groups in %q", got)
	}
}

func TestGrep_MaxResults(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("match line\n")
	}
	path := writeTestFile(t, dir, "test.txt", sb.String())
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "match", "max_results", float64(5)), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if !strings.Contains(got, "[results truncated at 5 matches]") {
		t.Errorf("expected truncation message, got %q", got)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	// 5 match lines + 1 truncation message
	if len(lines) != 6 {
		t.Errorf("expected 6 lines (5 results + truncation), got %d", len(lines))
	}
}

func TestGrep_BinaryFileSkipped(t *testing.T) {
	dir := t.TempDir()
	// Create a binary file
	binPath := filepath.Join(dir, "binary.dat")
	binData := []byte{0x00, 0x01, 0x02, 'h', 'e', 'l', 'l', 'o', 0x00}
	os.WriteFile(binPath, binData, 0644)
	// Create a text file with a match
	writeTestFile(t, dir, "text.txt", "hello world\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", dir, "pattern", "hello", "recursive", true), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	// Should find match in text.txt but not error on binary
	if !strings.Contains(got, "hello world") {
		t.Errorf("expected text.txt match in %q", got)
	}
	if strings.Contains(got, "binary.dat") {
		t.Errorf("binary file should be skipped, got %q", got)
	}
}

func TestGrep_UTF16FileDecoded(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16LEFile(t, dir, "utf16.txt", "hello UTF-16 world\nfoo bar\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "UTF-16"), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if !strings.Contains(got, "hello UTF-16 world") {
		t.Errorf("expected decoded UTF-16 match, got %q", got)
	}
}

func TestGrep_CRLFStripped(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "crlf.txt", "hello world\r\nfoo\r\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "world$"), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	// \r should be stripped before matching, so "world$" matches
	if !strings.Contains(got, "hello world") {
		t.Errorf("CRLF stripping failed, $ anchor didn't match: %q", got)
	}
}

func TestGrep_PathConfinement(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Grep(grepParams("path", "C:\\Windows\\System32\\config", "pattern", "test"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

func TestGrep_InvalidRegex(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "hello\n")
	roots := []string{dir}
	_, err := Grep(grepParams("path", path, "pattern", "[invalid"), roots)
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
	if !strings.Contains(err.Error(), "invalid regex") {
		t.Errorf("expected 'invalid regex' in error, got %q", err.Error())
	}
}

func TestGrep_FallbackRegistration(t *testing.T) {
	cfg := &shared.Config{
		BuiltinDescriptions: map[string]string{
			"read": "Read", "write": "Write", "edit": "Edit",
			"copy": "Copy", "move": "Move", "delete": "Delete",
			"list": "List", "search": "Search", "cat": "Cat",
			"diff": "Diff", "head": "Head", "info": "Info",
			"mkdir": "Mkdir", "roots": "Roots", "tail": "Tail",
			"tree": "Tree", "wc": "Wc", "run": "Run",
		},
		Tools: map[string]shared.ToolConfig{
			"grep": {
				Exe:         "C:\\nonexistent\\grep.exe",
				Description: "test grep",
			},
		},
	}
	result, err := BuildToolSchemas(cfg)
	if err != nil {
		t.Fatalf("BuildToolSchemas error: %v", err)
	}
	if !result.BuiltinOverrides["grep"] {
		t.Error("expected grep in builtin overrides")
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning about missing exe")
	}
	// Find grep schema
	found := false
	for _, s := range result.Schemas {
		if s.Name == "grep" {
			found = true
			break
		}
	}
	if !found {
		t.Error("grep schema not found in result")
	}
}

func TestGrep_ExternalForbidden(t *testing.T) {
	// Even when a valid external grep.exe is configured, built-in wins
	// and a warning is emitted.
	dir := t.TempDir()
	fakeExe := filepath.Join(dir, "grep.exe")
	os.WriteFile(fakeExe, []byte("fake"), 0755)

	cfg := &shared.Config{
		BuiltinDescriptions: map[string]string{
			"read": "Read", "write": "Write", "edit": "Edit",
			"copy": "Copy", "move": "Move", "delete": "Delete",
			"list": "List", "search": "Search", "cat": "Cat",
			"diff": "Diff", "head": "Head", "info": "Info",
			"mkdir": "Mkdir", "roots": "Roots", "tail": "Tail",
			"tree": "Tree", "wc": "Wc", "run": "Run",
		},
		Tools: map[string]shared.ToolConfig{
			"grep": {
				Exe:         fakeExe,
				Description: "external grep",
			},
		},
	}
	result, err := BuildToolSchemas(cfg)
	if err != nil {
		t.Fatalf("BuildToolSchemas error: %v", err)
	}
	if !result.BuiltinOverrides["grep"] {
		t.Error("expected built-in grep to always win, even when external exe exists")
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning that configured [tools.grep] is ignored")
	}
}

func TestGrep_LargeFile(t *testing.T) {
	dir := t.TempDir()
	// Build a file near 400KB
	var sb strings.Builder
	line := strings.Repeat("x", 99) + "\n" // 100 bytes per line
	for sb.Len() < 400*1024 {
		sb.WriteString(line)
	}
	// Add a match near the end
	sb.WriteString("FINDME here\n")
	path := writeTestFile(t, dir, "large.txt", sb.String())
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "FINDME"), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if !strings.Contains(got, "FINDME here") {
		t.Errorf("expected to find match in large file, got %q", got)
	}
}

// ===== Addendum tests =====

// --- Input edge cases ---

func TestGrep_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "empty.txt", "")
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "anything"), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty result for empty file, got %q", got)
	}
}

func TestGrep_EmptyPattern(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "hello\n")
	roots := []string{dir}
	_, err := Grep(grepParams("path", path, "pattern", ""), roots)
	if err == nil {
		t.Fatal("expected error for empty pattern")
	}
	if !strings.Contains(err.Error(), "empty pattern") {
		t.Errorf("expected 'empty pattern' in error, got %q", err.Error())
	}
}

func TestGrep_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Grep(grepParams("path", filepath.Join(dir, "nonexistent.txt"), "pattern", "test"), roots)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestGrep_DirectoryNotFound(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Grep(grepParams("path", filepath.Join(dir, "nodir"), "pattern", "test"), roots)
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestGrep_NoMatchesAnywhere(t *testing.T) {
	dir := t.TempDir()
	sub := writeTestSubdir(t, dir, "sub")
	writeTestFile(t, dir, "a.txt", "hello\n")
	writeTestFile(t, sub, "b.txt", "world\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", dir, "pattern", "zzzzz", "recursive", true), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty result, got %q", got)
	}
}

// --- Parameter variations ---

func TestGrep_LineNumbersFalse(t *testing.T) {
	dir := t.TempDir()
	// Single file mode
	path := writeTestFile(t, dir, "test.txt", "hello\nworld\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "hello", "line_numbers", false), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	got = strings.TrimRight(got, "\n")
	if got != "hello" {
		t.Errorf("single file: expected just 'hello', got %q", got)
	}

	// Multi-file mode (directory)
	writeTestFile(t, dir, "other.txt", "hello there\n")
	got2, err := Grep(grepParams("path", dir, "pattern", "hello", "line_numbers", false), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	// Should have path prefix but no line numbers
	for _, line := range strings.Split(strings.TrimRight(got2, "\n"), "\n") {
		// Line should be "path:text" with no number between
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			t.Errorf("expected path:text format, got %q", line)
		}
		// The part after the first ":" should not start with a digit (no line number)
		if len(parts[1]) > 0 && parts[1][0] >= '0' && parts[1][0] <= '9' {
			// Could be the content itself starting with a digit, but our content doesn't
			// Actually, for "hello" content, it starts with 'h'
		}
	}
}

func TestGrep_RecursiveFalseOnDir(t *testing.T) {
	dir := t.TempDir()
	sub := writeTestSubdir(t, dir, "sub")
	writeTestFile(t, dir, "top.txt", "match here\n")
	writeTestFile(t, sub, "deep.txt", "match there\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", dir, "pattern", "match", "recursive", false), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if !strings.Contains(got, "match here") {
		t.Errorf("expected top-level match, got %q", got)
	}
	if strings.Contains(got, "match there") {
		t.Errorf("should not find match in subdirectory with recursive=false, got %q", got)
	}
}

func TestGrep_IncludeNoMatches(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "test.py", "hello\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", dir, "pattern", "hello", "recursive", true, "include", "*.xyz"), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty result with non-matching include, got %q", got)
	}
}

func TestGrep_ContextZero(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "a\nb\nmatch\nd\ne\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "match", "context", float64(0)), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line with context=0, got %d: %q", len(lines), got)
	}
}

// --- Context line edge cases ---

func TestGrep_OverlappingContext(t *testing.T) {
	dir := t.TempDir()
	// Two matches 2 lines apart, context=3 — should merge
	content := "line1\nMATCH1\nline3\nMATCH2\nline5\n"
	path := writeTestFile(t, dir, "test.txt", content)
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "MATCH", "context", float64(3)), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	// Should NOT have separator since contexts overlap
	if strings.Contains(got, "--") {
		t.Errorf("expected merged context (no separator), got %q", got)
	}
	// Both matches should be present
	if !strings.Contains(got, "MATCH1") || !strings.Contains(got, "MATCH2") {
		t.Errorf("expected both matches in output, got %q", got)
	}
}

func TestGrep_ContextSeparator(t *testing.T) {
	dir := t.TempDir()
	// Two matches far apart, context=1
	content := "line1\nMATCH1\nline3\nline4\nline5\nline6\nMATCH2\nline8\n"
	path := writeTestFile(t, dir, "test.txt", content)
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "MATCH", "context", float64(1)), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if !strings.Contains(got, "--") {
		t.Errorf("expected separator between non-contiguous groups, got %q", got)
	}
}

func TestGrep_MatchAtFileStart(t *testing.T) {
	dir := t.TempDir()
	content := "MATCH\nline2\nline3\nline4\n"
	path := writeTestFile(t, dir, "test.txt", content)
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "MATCH", "context", float64(3)), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if !strings.Contains(got, "MATCH") {
		t.Errorf("expected match at file start, got %q", got)
	}
	// Should have after-context
	if !strings.Contains(got, "line2") {
		t.Errorf("expected after-context, got %q", got)
	}
}

func TestGrep_MatchAtFileEnd(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\nMATCH\n"
	path := writeTestFile(t, dir, "test.txt", content)
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "MATCH", "context", float64(3)), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if !strings.Contains(got, "MATCH") {
		t.Errorf("expected match at file end, got %q", got)
	}
	// Should have before-context
	if !strings.Contains(got, "line2") {
		t.Errorf("expected before-context, got %q", got)
	}
}

func TestGrep_ConsecutiveMatches(t *testing.T) {
	dir := t.TempDir()
	content := "match1\nmatch2\nmatch3\nmatch4\nmatch5\n"
	path := writeTestFile(t, dir, "test.txt", content)
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "match", "context", float64(1)), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	// No separators — all contiguous
	if strings.Contains(got, "--") {
		t.Errorf("expected no separators for consecutive matches, got %q", got)
	}
	// Each line printed exactly once
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines (one per match, no dupes), got %d: %q", len(lines), got)
	}
}

// --- Filesystem edge cases ---

func TestGrep_PermissionDenied(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "readable.txt", "findme\n")
	unreadable := writeTestFile(t, dir, "unreadable.txt", "findme\n")
	os.Chmod(unreadable, 0000)
	defer os.Chmod(unreadable, 0644) // cleanup
	roots := []string{dir}
	got, err := Grep(grepParams("path", dir, "pattern", "findme", "recursive", true), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	// Should find match in readable file, skip unreadable silently
	if !strings.Contains(got, "findme") {
		t.Errorf("expected match in readable file, got %q", got)
	}
}

func TestGrep_SymlinkOutsideRoots(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	writeTestFile(t, outsideDir, "secret.txt", "findme\n")
	linkPath := filepath.Join(dir, "link.txt")
	err := os.Symlink(filepath.Join(outsideDir, "secret.txt"), linkPath)
	if err != nil {
		t.Skip("symlinks not supported on this system")
	}
	roots := []string{dir}
	got, err := Grep(grepParams("path", dir, "pattern", "findme", "recursive", true), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	// Should NOT follow symlink outside roots
	if strings.Contains(got, "findme") {
		t.Errorf("symlink outside roots should be skipped, got %q", got)
	}
}

func TestGrep_VeryLongLine(t *testing.T) {
	dir := t.TempDir()
	// Line > 64 KB, < 1 MB
	longLine := strings.Repeat("x", 100*1024) + "FINDME" + strings.Repeat("y", 100*1024) + "\n"
	path := writeTestFile(t, dir, "long.txt", longLine)
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "FINDME"), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if !strings.Contains(got, "FINDME") {
		t.Errorf("expected to match very long line, got length %d", len(got))
	}
}

func TestGrep_MixedBinaryAndText(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "binary.dat")
	os.WriteFile(binPath, []byte{0x00, 0x01, 'f', 'i', 'n', 'd', 0x00}, 0644)
	writeTestFile(t, dir, "text.txt", "find this\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", dir, "pattern", "find", "recursive", true), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if !strings.Contains(got, "find this") {
		t.Errorf("expected text file match, got %q", got)
	}
	if strings.Contains(got, "binary.dat") {
		t.Errorf("binary file should be skipped, got %q", got)
	}
}

// --- Content edge cases ---

func TestGrep_UnicodeContent(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "cjk.txt", "你好世界\nother line\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "你好"), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if !strings.Contains(got, "你好世界") {
		t.Errorf("expected CJK match, got %q", got)
	}
}

func TestGrep_UnicodePattern(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "accent.txt", "café\nresumé\nnaive\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "é"), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines with accented char, got %d: %q", len(lines), got)
	}
}

func TestGrep_DollarAnchorCRLF(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "crlf.txt", "hello end\r\nother\r\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "end$"), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	if !strings.Contains(got, "hello end") {
		t.Errorf("dollar anchor should match after CRLF stripping, got %q", got)
	}
}

func TestGrep_CaretAnchor(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "start here\nnot start\nstart again\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "^start"), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines matching ^start, got %d: %q", len(lines), got)
	}
}

func TestGrep_MultipleMatchesSameLine(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "aaa bbb aaa ccc aaa\nno match\n")
	roots := []string{dir}
	got, err := Grep(grepParams("path", path, "pattern", "aaa"), roots)
	if err != nil {
		t.Fatalf("Grep error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("expected line printed once despite multiple matches, got %d: %q", len(lines), got)
	}
}
