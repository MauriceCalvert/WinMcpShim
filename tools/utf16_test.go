package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeUTF16LEFile writes a UTF-16 LE file with BOM.
func writeUTF16LEFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	var data []byte
	data = append(data, 0xFF, 0xFE) // BOM
	for _, r := range content {
		data = append(data, byte(r&0xFF), byte(r>>8))
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// writeUTF16BEFile writes a UTF-16 BE file with BOM.
func writeUTF16BEFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	var data []byte
	data = append(data, 0xFE, 0xFF) // BOM
	for _, r := range content {
		data = append(data, byte(r>>8), byte(r&0xFF))
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// writeUTF8File writes a plain UTF-8 file.
func writeUTF8File(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func params(kv ...interface{}) map[string]interface{} {
	m := make(map[string]interface{})
	for i := 0; i < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func TestRead_UTF16LEFile(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16LEFile(t, dir, "le.txt", "Hello World\n")
	roots := []string{dir}
	got, err := Read(params("path", path), roots)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if !strings.Contains(got, "Hello World") {
		t.Errorf("expected decoded content, got %q", got)
	}
}

func TestRead_UTF16BEFile(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16BEFile(t, dir, "be.txt", "Hello World\n")
	roots := []string{dir}
	got, err := Read(params("path", path), roots)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if !strings.Contains(got, "Hello World") {
		t.Errorf("expected decoded content, got %q", got)
	}
}

func TestRead_UTF16OffsetLimit(t *testing.T) {
	dir := t.TempDir()
	// "ABCD" in UTF-16 LE = BOM(2) + A(2) + B(2) + C(2) + D(2) = 10 bytes
	path := writeUTF16LEFile(t, dir, "offset.txt", "ABCD")
	roots := []string{dir}
	// offset=2 skips first code unit (A), limit=4 reads 2 code units (B, C)
	got, err := Read(params("path", path, "offset", float64(2), "limit", float64(4)), roots)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if got != "BC" {
		t.Errorf("expected %q, got %q", "BC", got)
	}
}

func TestRead_UTF16OddOffset(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16LEFile(t, dir, "odd.txt", "ABCD")
	roots := []string{dir}
	_, err := Read(params("path", path, "offset", float64(1)), roots)
	if err == nil {
		t.Fatal("expected error for odd offset")
	}
	if !strings.Contains(err.Error(), "even") {
		t.Errorf("expected 'even' in error, got %q", err.Error())
	}
}

func TestHead_UTF16File(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16LEFile(t, dir, "head.txt", "line1\nline2\nline3\nline4\n")
	roots := []string{dir}
	got, err := Head(params("path", path, "lines", float64(2)), roots)
	if err != nil {
		t.Fatalf("Head error: %v", err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %q", len(lines), got)
	}
	if lines[0] != "line1" {
		t.Errorf("expected first line 'line1', got %q", lines[0])
	}
}

func TestTail_UTF16File(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16LEFile(t, dir, "tail.txt", "line1\nline2\nline3\nline4\n")
	roots := []string{dir}
	got, err := Tail(params("path", path, "lines", float64(2)), roots)
	if err != nil {
		t.Fatalf("Tail error: %v", err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %q", len(lines), got)
	}
	if lines[0] != "line3" {
		t.Errorf("expected 'line3', got %q", lines[0])
	}
	if lines[1] != "line4" {
		t.Errorf("expected 'line4', got %q", lines[1])
	}
}

func TestCat_UTF16File(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16LEFile(t, dir, "cat.txt", "Hello UTF-16")
	roots := []string{dir}
	got, err := Cat(params("paths", path), roots)
	if err != nil {
		t.Fatalf("Cat error: %v", err)
	}
	if !strings.Contains(got, "Hello UTF-16") {
		t.Errorf("expected decoded content, got %q", got)
	}
}

func TestCat_MixedEncodings(t *testing.T) {
	dir := t.TempDir()
	p1 := writeUTF8File(t, dir, "utf8.txt", "ASCII content")
	p2 := writeUTF16LEFile(t, dir, "utf16.txt", "UTF-16 content")
	roots := []string{dir}
	paths := p1 + " " + p2
	got, err := Cat(params("paths", paths), roots)
	if err != nil {
		t.Fatalf("Cat error: %v", err)
	}
	if !strings.Contains(got, "ASCII content") {
		t.Errorf("missing UTF-8 content in %q", got)
	}
	if !strings.Contains(got, "UTF-16 content") {
		t.Errorf("missing UTF-16 content in %q", got)
	}
}

func TestWc_UTF16File(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16LEFile(t, dir, "wc.txt", "one two three\nfour five\n")
	roots := []string{dir}
	got, err := Wc(params("path", path), roots)
	if err != nil {
		t.Fatalf("Wc error: %v", err)
	}
	if !strings.Contains(got, "lines: 2") {
		t.Errorf("expected 2 lines in %q", got)
	}
	if !strings.Contains(got, "words: 5") {
		t.Errorf("expected 5 words in %q", got)
	}
}

func TestDiff_UTF16Files(t *testing.T) {
	dir := t.TempDir()
	p1 := writeUTF16LEFile(t, dir, "a.txt", "line1\nline2\n")
	p2 := writeUTF16LEFile(t, dir, "b.txt", "line1\nchanged\n")
	roots := []string{dir}
	got, err := Diff(params("file1", p1, "file2", p2), roots)
	if err != nil {
		t.Fatalf("Diff error: %v", err)
	}
	if !strings.Contains(got, "-line2") || !strings.Contains(got, "+changed") {
		t.Errorf("expected diff output showing change, got %q", got)
	}
}

func TestDiff_MixedEncodings(t *testing.T) {
	dir := t.TempDir()
	p1 := writeUTF8File(t, dir, "utf8.txt", "line1\nline2\n")
	p2 := writeUTF16LEFile(t, dir, "utf16.txt", "line1\nline2\n")
	roots := []string{dir}
	got, err := Diff(params("file1", p1, "file2", p2), roots)
	if err != nil {
		t.Fatalf("Diff error: %v", err)
	}
	// Same content, different encoding — should produce no diff (or minimal)
	if strings.Contains(got, "-line") || strings.Contains(got, "+line") {
		t.Errorf("expected no meaningful diff for same content, got %q", got)
	}
}
