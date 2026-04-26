package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var exePath string

// TestMain builds strpatch.exe into a temp directory before running tests.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "strpatch-test-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	goExe, err := exec.LookPath("go")
	if err != nil {
		fmt.Fprintf(os.Stderr, "go not found on PATH: %v\n", err)
		os.Exit(1)
	}
	exePath = filepath.Join(dir, "strpatch.exe")
	build := exec.Command(goExe, "build", "-o", exePath, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build strpatch.exe: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// strpatchExe returns the path to the built strpatch.exe.
func strpatchExe(t *testing.T) string {
	t.Helper()
	return exePath
}

// runStrpatch runs strpatch.exe with the given JSON input and returns stdout, stderr, and exit code.
func runStrpatch(t *testing.T, input string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(strpatchExe(t))
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run strpatch: %v", err)
		}
	}
	return stdout.String(), stderr.String(), code
}

// makeJSON creates a JSON string from the given fields.
func makeJSON(path, oldText, newText string) string {
	req := PatchRequest{Path: path, OldText: oldText, NewText: newText}
	data, _ := json.Marshal(req)
	return string(data)
}

// writeTemp creates a temporary file with the given content and returns its path.
func writeTemp(t *testing.T, dir, content string) string {
	t.Helper()
	f, err := os.CreateTemp(dir, "strpatch-test-*")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		t.Fatalf("write temp file: %v", err)
	}
	name := f.Name()
	f.Close()
	return name
}

// §10.1 Happy Path Tests

func TestHappyPath_LF(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "hello\nworld\n")
	input := makeJSON(path, "hello", "goodbye")
	stdout, stderr, code := runStrpatch(t, input)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Replaced 1 occurrence") {
		t.Errorf("unexpected stdout: %s", stdout)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "goodbye\nworld\n" {
		t.Errorf("file content = %q, want %q", string(data), "goodbye\nworld\n")
	}
}

func TestHappyPath_CRLF_LFSearch(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "hello\r\nworld\r\n")
	// Claude sends LF-only search, should match CRLF file.
	input := makeJSON(path, "hello\nworld", "goodbye\nplanet")
	stdout, stderr, code := runStrpatch(t, input)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Replaced 1 occurrence") {
		t.Errorf("unexpected stdout: %s", stdout)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "goodbye\r\nplanet\r\n" {
		t.Errorf("file content = %q, want %q", string(data), "goodbye\r\nplanet\r\n")
	}
}

func TestHappyPath_CRLF_CRLFSearch(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "hello\r\nworld\r\n")
	// Search already has CRLF — should not double-convert.
	input := makeJSON(path, "hello\r\nworld", "goodbye\r\nplanet")
	stdout, stderr, code := runStrpatch(t, input)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Replaced 1 occurrence") {
		t.Errorf("unexpected stdout: %s", stdout)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "goodbye\r\nplanet\r\n" {
		t.Errorf("file content = %q, want %q", string(data), "goodbye\r\nplanet\r\n")
	}
}

func TestHappyPath_UTF8BOM(t *testing.T) {
	dir := t.TempDir()
	bom := "\xEF\xBB\xBFhello world"
	path := writeTemp(t, dir, bom)
	input := makeJSON(path, "hello", "goodbye")
	_, stderr, code := runStrpatch(t, input)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "\xEF\xBB\xBFgoodbye world" {
		t.Errorf("BOM not preserved: %q", string(data))
	}
}

func TestHappyPath_EmptyReplacement(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "abc def ghi")
	input := makeJSON(path, " def", "")
	_, stderr, code := runStrpatch(t, input)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "abc ghi" {
		t.Errorf("file = %q, want %q", string(data), "abc ghi")
	}
}

func TestHappyPath_ReplacementContainsSearch(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "foo bar baz")
	input := makeJSON(path, "foo", "foo bar foo")
	_, stderr, code := runStrpatch(t, input)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "foo bar foo bar baz" {
		t.Errorf("file = %q", string(data))
	}
}

func TestHappyPath_MultiLine(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "line1\nline2\nline3\n")
	input := makeJSON(path, "line1\nline2", "replaced1\nreplaced2")
	_, stderr, code := runStrpatch(t, input)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "replaced1\nreplaced2\nline3\n" {
		t.Errorf("file = %q", string(data))
	}
}

func TestHappyPath_Tabs(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "func() {\n\treturn 1\n}\n")
	input := makeJSON(path, "\treturn 1", "\treturn 2")
	_, stderr, code := runStrpatch(t, input)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "func() {\n\treturn 2\n}\n" {
		t.Errorf("file = %q", string(data))
	}
}

func TestHappyPath_Backslashes(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, `path = "C:\Users\test"`)
	input := makeJSON(path, `C:\Users\test`, `D:\projects\new`)
	_, stderr, code := runStrpatch(t, input)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, _ := os.ReadFile(path)
	if string(data) != `path = "D:\projects\new"` {
		t.Errorf("file = %q", string(data))
	}
}

func TestHappyPath_Quotes(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, `say "hello" to the world`)
	input := makeJSON(path, `"hello"`, `"goodbye"`)
	_, stderr, code := runStrpatch(t, input)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, _ := os.ReadFile(path)
	if string(data) != `say "goodbye" to the world` {
		t.Errorf("file = %q", string(data))
	}
}

func TestHappyPath_Unicode(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "hello café world")
	input := makeJSON(path, "café", "naïve")
	_, stderr, code := runStrpatch(t, input)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hello naïve world" {
		t.Errorf("file = %q", string(data))
	}
}

func TestHappyPath_LargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file test in short mode")
	}
	dir := t.TempDir()
	// Create a 5 MB file with a unique marker near the end.
	content := strings.Repeat("abcdefghij", 500000) + "UNIQUE_MARKER" + strings.Repeat("klmnopqrst", 10)
	path := writeTemp(t, dir, content)
	input := makeJSON(path, "UNIQUE_MARKER", "REPLACED_MARKER")
	_, stderr, code := runStrpatch(t, input)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "REPLACED_MARKER") {
		t.Error("replacement not found in large file")
	}
	if strings.Contains(string(data), "UNIQUE_MARKER") {
		t.Error("original marker still present in large file")
	}
}

// §10.2 Refusal Cases

func TestRefusal_BinaryFile(t *testing.T) {
	dir := t.TempDir()
	content := "hello\x00world"
	path := writeTemp(t, dir, content)
	input := makeJSON(path, "hello", "goodbye")
	_, stderr, code := runStrpatch(t, input)
	if code != ExitFileError {
		t.Errorf("exit = %d, want %d", code, ExitFileError)
	}
	if !strings.Contains(stderr, "binary") {
		t.Errorf("stderr = %q, want mention of binary", stderr)
	}
}

func TestRefusal_UTF16(t *testing.T) {
	dir := t.TempDir()
	content := "\xFF\xFEhello"
	path := writeTemp(t, dir, content)
	input := makeJSON(path, "hello", "goodbye")
	_, stderr, code := runStrpatch(t, input)
	if code != ExitFileError {
		t.Errorf("exit = %d, want %d", code, ExitFileError)
	}
	if !strings.Contains(stderr, "UTF-16") {
		t.Errorf("stderr = %q, want mention of UTF-16", stderr)
	}
}

func TestRefusal_UTF16BE(t *testing.T) {
	dir := t.TempDir()
	content := "\xFE\xFFhello"
	path := writeTemp(t, dir, content)
	input := makeJSON(path, "hello", "goodbye")
	_, stderr, code := runStrpatch(t, input)
	if code != ExitFileError {
		t.Errorf("exit = %d, want %d", code, ExitFileError)
	}
	if !strings.Contains(stderr, "UTF-16") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRefusal_TooLarge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file test in short mode")
	}
	dir := t.TempDir()
	content := strings.Repeat("a", MaxFileSize+1)
	path := writeTemp(t, dir, content)
	input := makeJSON(path, "a", "b")
	_, stderr, code := runStrpatch(t, input)
	if code != ExitFileError {
		t.Errorf("exit = %d, want %d", code, ExitFileError)
	}
	if !strings.Contains(stderr, "10 MB") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRefusal_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "hello world")
	input := makeJSON(path, "nonexistent", "replacement")
	_, stderr, code := runStrpatch(t, input)
	if code != ExitNotFound {
		t.Errorf("exit = %d, want %d", code, ExitNotFound)
	}
	if !strings.Contains(stderr, "not found") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRefusal_NotUnique(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "hello hello hello")
	input := makeJSON(path, "hello", "goodbye")
	_, stderr, code := runStrpatch(t, input)
	if code != ExitNotUnique {
		t.Errorf("exit = %d, want %d", code, ExitNotUnique)
	}
	if !strings.Contains(stderr, "not unique") {
		t.Errorf("stderr = %q", stderr)
	}
	if !strings.Contains(stderr, "3") {
		t.Errorf("stderr should contain count 3: %q", stderr)
	}
}

func TestRefusal_EmptySearch(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "hello")
	jsonStr := fmt.Sprintf(`{"path":%q,"old_text":"","new_text":"x"}`, path)
	_, stderr, code := runStrpatch(t, jsonStr)
	if code != ExitInputError {
		t.Errorf("exit = %d, want %d", code, ExitInputError)
	}
	if !strings.Contains(stderr, "empty") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRefusal_FileNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.txt")
	input := makeJSON(path, "hello", "goodbye")
	_, stderr, code := runStrpatch(t, input)
	if code != ExitFileError {
		t.Errorf("exit = %d, want %d", code, ExitFileError)
	}
	if !strings.Contains(stderr, "not found") || !strings.Contains(stderr, "File") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRefusal_ReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "hello world")
	os.Chmod(path, 0444)
	defer os.Chmod(path, 0644)
	input := makeJSON(path, "hello", "goodbye")
	_, stderr, code := runStrpatch(t, input)
	if code != ExitFileError {
		t.Errorf("exit = %d, want %d", code, ExitFileError)
	}
	if !strings.Contains(stderr, "read-only") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRefusal_InvalidJSON(t *testing.T) {
	_, stderr, code := runStrpatch(t, "not json at all")
	if code != ExitInputError {
		t.Errorf("exit = %d, want %d", code, ExitInputError)
	}
	if !strings.Contains(stderr, "Invalid JSON") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRefusal_MissingField(t *testing.T) {
	_, stderr, code := runStrpatch(t, `{"path":"x.txt","new_text":"y"}`)
	if code != ExitInputError {
		t.Errorf("exit = %d, want %d", code, ExitInputError)
	}
	if !strings.Contains(stderr, "empty") || !strings.Contains(stderr, "Search") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRefusal_EmptyStdin(t *testing.T) {
	_, stderr, code := runStrpatch(t, "")
	if code != ExitInputError {
		t.Errorf("exit = %d, want %d", code, ExitInputError)
	}
	if !strings.Contains(stderr, "No input") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRefusal_TruncatedJSON(t *testing.T) {
	_, stderr, code := runStrpatch(t, `{"path":"x.txt","old_text":"a"`)
	if code != ExitInputError {
		t.Errorf("exit = %d, want %d", code, ExitInputError)
	}
	if !strings.Contains(stderr, "Invalid JSON") {
		t.Errorf("stderr = %q", stderr)
	}
}

// §10.3 Atomicity — original file intact on failure

func TestAtomicity_OriginalIntactOnNotFound(t *testing.T) {
	dir := t.TempDir()
	original := "original content"
	path := writeTemp(t, dir, original)
	input := makeJSON(path, "nonexistent", "replacement")
	runStrpatch(t, input)
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Errorf("original file modified: %q", string(data))
	}
}

func TestAtomicity_OriginalIntactOnNotUnique(t *testing.T) {
	dir := t.TempDir()
	original := "hello hello"
	path := writeTemp(t, dir, original)
	input := makeJSON(path, "hello", "goodbye")
	runStrpatch(t, input)
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Errorf("original file modified: %q", string(data))
	}
}


// §4.9 File locked by another process
func TestRefusal_FileLocked(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("file locking test unreliable in CI")
	}
	dir := t.TempDir()
	path := writeTemp(t, dir, "hello world")
	f := lockFileExclusive(t, path)
	defer f.Close()
	input := makeJSON(path, "hello", "goodbye")
	_, stderr, code := runStrpatch(t, input)
	if code != ExitFileError {
		t.Errorf("exit = %d, want %d", code, ExitFileError)
	}
	if !strings.Contains(stderr, "locked") {
		t.Errorf("stderr = %q, want mention of locked", stderr)
	}
	// Verify original file intact after lock released.
	f.Close()
	data, _ := os.ReadFile(path)
	if string(data) != "hello world" {
		t.Errorf("original modified: %q", string(data))
	}
}

// §7.5 File without final newline
func TestHappyPath_NoFinalNewline(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "first line\nsecond line")
	input := makeJSON(path, "first", "1st")
	stdout, stderr, code := runStrpatch(t, input)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Replaced 1 occurrence") {
		t.Errorf("unexpected stdout: %s", stdout)
	}
	data, _ := os.ReadFile(path)
	want := "1st line\nsecond line"
	if string(data) != want {
		t.Errorf("file = %q, want %q", string(data), want)
	}
}

// §4.10 / §5.5.5 Write failed (ExitWriteError = 4)
// ACCEPT: Triggering atomicWrite failure requires OS-level ACL manipulation
// (icacls /deny (W) is insufficient — Windows needs finer-grained denial
// to block CreateTemp). The code path is a simple error return from
// os.CreateTemp/os.Rename wrapped as "Write failed: ...".
// Same class of untestability as crash-atomicity (§10.3.3, §10.3.4).
