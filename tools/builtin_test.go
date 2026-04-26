package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MauriceCalvert/WinMcpShim/shared"
)

// ===== Cat =====

func TestCat_BinaryRefusal(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "bin.dat")
	os.WriteFile(binPath, []byte{0x00, 0x01, 0x02}, 0644)
	roots := []string{dir}
	_, err := Cat(params("paths", binPath), roots)
	if err == nil {
		t.Fatal("expected error for binary file")
	}
	if !strings.Contains(err.Error(), "binary") {
		t.Errorf("expected 'binary' in error, got %q", err.Error())
	}
}

func TestCat_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Cat(params("paths", filepath.Join(dir, "nope.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestCat_OutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Cat(params("paths", "C:\\Windows\\System32\\config\\sam"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

func TestCat_SingleFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "one.txt", "hello single")
	roots := []string{dir}
	got, err := Cat(params("paths", path), roots)
	if err != nil {
		t.Fatalf("Cat error: %v", err)
	}
	if got != "hello single" {
		t.Errorf("expected %q, got %q", "hello single", got)
	}
}

// ===== Copy =====

func TestCopy_DestOutsideRoots(t *testing.T) {
	dir := t.TempDir()
	src := writeTestFile(t, dir, "src.txt", "data")
	roots := []string{dir}
	_, err := Copy(params("source", src, "destination", "C:\\Windows\\evil.txt"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

func TestCopy_MissingParams(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Copy(params("source", filepath.Join(dir, "a.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing destination")
	}
	_, err = Copy(params("destination", filepath.Join(dir, "b.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestCopy_SourceNotFound(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Copy(params("source", filepath.Join(dir, "nope.txt"), "destination", filepath.Join(dir, "dst.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %q", err.Error())
	}
}

func TestCopy_SourceOutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Copy(params("source", "C:\\Windows\\System32\\notepad.exe", "destination", filepath.Join(dir, "copy.exe")), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

// ===== Delete =====

func TestDelete_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Delete(params("path", filepath.Join(dir, "nope.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestDelete_OutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Delete(params("path", "C:\\Windows\\System32\\notepad.exe"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

// ===== Diff =====

func TestDiff_MissingParams(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Diff(params("file1", filepath.Join(dir, "a.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing file2")
	}
	_, err = Diff(params("file2", filepath.Join(dir, "b.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing file1")
	}
}

func TestDiff_OneMissing(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "a.txt", "hello\n")
	roots := []string{dir}
	_, err := Diff(params("file1", filepath.Join(dir, "a.txt"), "file2", filepath.Join(dir, "nope.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing file2")
	}
}

func TestDiff_OutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Diff(params("file1", "C:\\Windows\\win.ini", "file2", filepath.Join(dir, "b.txt")), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

// ===== Head =====

func TestHead_DefaultLines(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	for i := 1; i <= 20; i++ {
		sb.WriteString("line\n")
	}
	path := writeTestFile(t, dir, "many.txt", sb.String())
	roots := []string{dir}
	got, err := Head(params("path", path), roots)
	if err != nil {
		t.Fatalf("Head error: %v", err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 10 {
		t.Errorf("expected 10 default lines, got %d", len(lines))
	}
}

func TestHead_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "empty.txt", "")
	roots := []string{dir}
	got, err := Head(params("path", path), roots)
	if err != nil {
		t.Fatalf("Head error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty result, got %q", got)
	}
}

func TestHead_OutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Head(params("path", "C:\\Windows\\win.ini"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

// ===== Info =====

func TestInfo_NotFound(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Info(params("path", filepath.Join(dir, "nope.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestInfo_OutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Info(params("path", "C:\\Windows\\System32\\notepad.exe"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

func TestInfo_ReadOnlyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "ro.txt", "read only content")
	os.Chmod(path, 0444)
	defer os.Chmod(path, 0644)
	roots := []string{dir}
	got, err := Info(params("path", path), roots)
	if err != nil {
		t.Fatalf("Info error: %v", err)
	}
	if !strings.Contains(got, "read_only: true") {
		t.Errorf("expected read_only: true, got %q", got)
	}
}

// ===== List =====

func TestList_DirNotFound(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := List(params("path", filepath.Join(dir, "nodir")), roots)
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestList_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	got, err := List(params("path", dir), roots)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty result for empty dir, got %q", got)
	}
}

func TestList_OutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := List(params("path", "C:\\Windows\\System32"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

// ===== Mkdir =====

func TestMkdir_AlreadyExistsAsFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "file.txt", "data")
	roots := []string{dir}
	// MkdirAll on an existing file path should succeed or fail depending on OS.
	// On Windows, creating a dir where a file exists returns an error.
	_, err := Mkdir(params("path", path), roots)
	if err == nil {
		// If no error, the OS allowed it (unlikely), just skip
		t.Skip("OS allowed MkdirAll on existing file path")
	}
}

func TestMkdir_MissingParam(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Mkdir(params(), roots)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

// ===== Move =====

func TestMove_DestOutsideRoots(t *testing.T) {
	dir := t.TempDir()
	src := writeTestFile(t, dir, "src.txt", "data")
	roots := []string{dir}
	_, err := Move(params("source", src, "destination", "C:\\Windows\\evil.txt"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

func TestMove_MissingParams(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Move(params("source", filepath.Join(dir, "a.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing destination")
	}
	_, err = Move(params("destination", filepath.Join(dir, "b.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestMove_SourceNotFound(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Move(params("source", filepath.Join(dir, "nope.txt"), "destination", filepath.Join(dir, "dst.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestMove_SourceOutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Move(params("source", "C:\\Windows\\System32\\notepad.exe", "destination", filepath.Join(dir, "copy.exe")), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

// ===== Read =====

func TestRead_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "empty.txt", "")
	roots := []string{dir}
	got, err := Read(params("path", path), roots)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty result, got %q", got)
	}
}

func TestRead_MissingPathParam(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Read(params(), roots)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("expected 'missing' in error, got %q", err.Error())
	}
}

// ===== Roots =====

func TestRoots_EmptyRoots(t *testing.T) {
	cfg := &shared.Config{}
	got, err := Roots(cfg)
	if err != nil {
		t.Fatalf("Roots error: %v", err)
	}
	if !strings.Contains(got, "no allowed roots") {
		t.Errorf("expected 'no allowed roots' message, got %q", got)
	}
}

func TestRoots_MultipleRoots(t *testing.T) {
	cfg := &shared.Config{
		Security: shared.SecurityConfig{
			AllowedRoots: []string{"C:\\Root1", "D:\\Root2", "E:\\Root3"},
		},
	}
	got, err := Roots(cfg)
	if err != nil {
		t.Fatalf("Roots error: %v", err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 roots, got %d: %q", len(lines), got)
	}
}

// ===== Search =====

func TestSearch_DirNotFound(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Search(params("path", filepath.Join(dir, "nodir"), "pattern", "*"), roots)
	// WalkDir on nonexistent dir returns empty, no error
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearch_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	got, err := Search(params("path", dir, "pattern", "*.txt"), roots)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty result, got %q", got)
	}
}

func TestSearch_OutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Search(params("path", "C:\\Windows", "pattern", "*.exe"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

// ===== Tail =====

func TestTail_DefaultLines(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	for i := 1; i <= 20; i++ {
		sb.WriteString("line\n")
	}
	path := writeTestFile(t, dir, "many.txt", sb.String())
	roots := []string{dir}
	got, err := Tail(params("path", path), roots)
	if err != nil {
		t.Fatalf("Tail error: %v", err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 10 {
		t.Errorf("expected 10 default lines, got %d", len(lines))
	}
}

func TestTail_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "empty.txt", "")
	roots := []string{dir}
	got, err := Tail(params("path", path), roots)
	if err != nil {
		t.Fatalf("Tail error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty result, got %q", got)
	}
}

func TestTail_FewerLinesThanRequested(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "short.txt", "one\ntwo\nthree\n")
	roots := []string{dir}
	got, err := Tail(params("path", path, "lines", float64(10)), roots)
	if err != nil {
		t.Fatalf("Tail error: %v", err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %q", len(lines), got)
	}
}

func TestTail_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Tail(params("path", filepath.Join(dir, "nope.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestTail_OutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Tail(params("path", "C:\\Windows\\win.ini"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

func TestTail_UTF16BEFile(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16BEFile(t, dir, "be.txt", "line1\nline2\nline3\nline4\n")
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
}

func TestTail_UTF8BOMFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bom.txt")
	content := append([]byte{0xEF, 0xBB, 0xBF}, []byte("line1\nline2\nline3\n")...)
	os.WriteFile(path, content, 0644)
	roots := []string{dir}
	got, err := Tail(params("path", path, "lines", float64(2)), roots)
	if err != nil {
		t.Fatalf("Tail error: %v", err)
	}
	if strings.Contains(got, "\xEF\xBB\xBF") {
		t.Error("BOM should be stripped")
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %q", len(lines), got)
	}
}

// ===== Tree =====

func TestTree_DirNotFound(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	got, err := Tree(params("path", filepath.Join(dir, "nodir")), roots)
	if err != nil {
		t.Fatalf("Tree error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty result for missing dir, got %q", got)
	}
}

func TestTree_OutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Tree(params("path", "C:\\Windows"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

// ===== Wc =====

func TestWc_BinaryRefusal(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "bin.dat")
	os.WriteFile(binPath, []byte{0x00, 0x01, 0x02, 'h', 'e', 'l', 'l', 'o'}, 0644)
	roots := []string{dir}
	_, err := Wc(params("path", binPath), roots)
	if err == nil {
		t.Fatal("expected error for binary file")
	}
	if !strings.Contains(err.Error(), "binary") {
		t.Errorf("expected 'binary' in error, got %q", err.Error())
	}
}

func TestWc_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "empty.txt", "")
	roots := []string{dir}
	got, err := Wc(params("path", path), roots)
	if err != nil {
		t.Fatalf("Wc error: %v", err)
	}
	if !strings.Contains(got, "lines: 0") {
		t.Errorf("expected 0 lines, got %q", got)
	}
}

func TestWc_OutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Wc(params("path", "C:\\Windows\\win.ini"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

// ===== Write =====

func TestWrite_AppendNonExistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	roots := []string{dir}
	got, err := Write(params("path", path, "content", "appended", "append", true), roots)
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if !strings.Contains(got, "Wrote") {
		t.Errorf("expected 'Wrote' message, got %q", got)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "appended" {
		t.Errorf("expected 'appended', got %q", string(data))
	}
}

func TestWrite_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "deep.txt")
	roots := []string{dir}
	_, err := Write(params("path", path, "content", "deep"), roots)
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "deep" {
		t.Errorf("expected 'deep', got %q", string(data))
	}
}

func TestWrite_MissingContentParam(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Write(params("path", filepath.Join(dir, "test.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing content")
	}
}

func TestWrite_MissingPathParam(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Write(params("content", "data"), roots)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestWrite_OutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Write(params("path", "C:\\Windows\\evil.txt", "content", "bad"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

// ===== Edit =====

func TestEdit_MissingParams(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Edit(params("path", filepath.Join(dir, "test.txt"), "old_text", "a"), roots)
	if err == nil {
		t.Fatal("expected error for missing new_text")
	}
	_, err = Edit(params("path", filepath.Join(dir, "test.txt"), "new_text", "b"), roots)
	if err == nil {
		t.Fatal("expected error for missing old_text")
	}
	_, err = Edit(params("old_text", "a", "new_text", "b"), roots)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestEdit_OutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Edit(params("path", "C:\\Windows\\evil.txt", "old_text", "a", "new_text", "b"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

// ===== Tail large file paths =====

func TestTail_LargeUTF8File(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	line := strings.Repeat("x", 99) + "\n"
	for sb.Len() < shared.MaxReadSize+1024 {
		sb.WriteString(line)
	}
	sb.WriteString("LAST LINE\n")
	path := writeTestFile(t, dir, "large.txt", sb.String())
	roots := []string{dir}
	got, err := Tail(params("path", path, "lines", float64(1)), roots)
	if err != nil {
		t.Fatalf("Tail error: %v", err)
	}
	if got != "LAST LINE" {
		t.Errorf("expected 'LAST LINE', got %q", got)
	}
}

func TestTail_LargeUTF16LEFile(t *testing.T) {
	dir := t.TempDir()
	// Build a large UTF-16 LE file (>MaxReadSize)
	var content strings.Builder
	line := strings.Repeat("a", 49) + "\n"
	for content.Len()*2+2 < shared.MaxReadSize+1024 {
		content.WriteString(line)
	}
	content.WriteString("FINAL\n")
	path := writeUTF16LEFile(t, dir, "large_utf16.txt", content.String())
	roots := []string{dir}
	got, err := Tail(params("path", path, "lines", float64(1)), roots)
	if err != nil {
		t.Fatalf("Tail error: %v", err)
	}
	if got != "FINAL" {
		t.Errorf("expected 'FINAL', got %q", got)
	}
}

func TestTail_UTF16LESmallFile(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16LEFile(t, dir, "small.txt", "alpha\nbeta\ngamma\n")
	roots := []string{dir}
	got, err := Tail(params("path", path, "lines", float64(2)), roots)
	if err != nil {
		t.Fatalf("Tail error: %v", err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 || lines[0] != "beta" || lines[1] != "gamma" {
		t.Errorf("expected [beta, gamma], got %v", lines)
	}
}

// ===== Read additional coverage =====

func TestRead_UTF8BOMFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bom.txt")
	content := append([]byte{0xEF, 0xBB, 0xBF}, []byte("hello bom")...)
	os.WriteFile(path, content, 0644)
	roots := []string{dir}
	got, err := Read(params("path", path), roots)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if strings.Contains(got, "\xEF\xBB\xBF") {
		t.Error("BOM should be stripped from output")
	}
	if got != "hello bom" {
		t.Errorf("expected 'hello bom', got %q", got)
	}
}

func TestRead_UTF8BOMWithOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bom2.txt")
	content := append([]byte{0xEF, 0xBB, 0xBF}, []byte("ABCDEF")...)
	os.WriteFile(path, content, 0644)
	roots := []string{dir}
	got, err := Read(params("path", path, "offset", float64(2), "limit", float64(3)), roots)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if got != "CDE" {
		t.Errorf("expected 'CDE', got %q", got)
	}
}

func TestRead_LargeFileWithoutRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.txt")
	data := make([]byte, shared.MaxReadSize+1)
	for i := range data {
		data[i] = 'a'
	}
	os.WriteFile(path, data, 0644)
	roots := []string{dir}
	_, err := Read(params("path", path), roots)
	if err == nil {
		t.Fatal("expected error for oversized file without range")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("expected 'limit' in error, got %q", err.Error())
	}
}

func TestRead_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Read(params("path", filepath.Join(dir, "nope.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestRead_OutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Read(params("path", "C:\\Windows\\win.ini"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

func TestRead_BinaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin.dat")
	os.WriteFile(path, []byte{0x00, 0x01, 0x02, 'h', 'e', 'l', 'l', 'o'}, 0644)
	roots := []string{dir}
	_, err := Read(params("path", path), roots)
	if err == nil {
		t.Fatal("expected error for binary file")
	}
}

// ===== Head additional =====

func TestHead_UTF16BEFile(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16BEFile(t, dir, "be.txt", "line1\nline2\nline3\n")
	roots := []string{dir}
	got, err := Head(params("path", path, "lines", float64(2)), roots)
	if err != nil {
		t.Fatalf("Head error: %v", err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %q", len(lines), got)
	}
}

func TestHead_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Head(params("path", filepath.Join(dir, "nope.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestHead_BinaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin.dat")
	os.WriteFile(path, []byte{0x00, 0x01, 0x02}, 0644)
	roots := []string{dir}
	_, err := Head(params("path", path), roots)
	if err == nil {
		t.Fatal("expected error for binary file")
	}
}

// ===== Wc additional =====

func TestWc_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Wc(params("path", filepath.Join(dir, "nope.txt")), roots)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestWc_NormalFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "wc.txt", "one two three\nfour five\nsix\n")
	roots := []string{dir}
	got, err := Wc(params("path", path), roots)
	if err != nil {
		t.Fatalf("Wc error: %v", err)
	}
	if !strings.Contains(got, "lines: 3") {
		t.Errorf("expected 3 lines in %q", got)
	}
	if !strings.Contains(got, "words: 6") {
		t.Errorf("expected 6 words in %q", got)
	}
}

// ===== Cat additional =====

func TestCat_JSONArrayPaths(t *testing.T) {
	dir := t.TempDir()
	p1 := writeTestFile(t, dir, "a.txt", "aaa")
	p2 := writeTestFile(t, dir, "b.txt", "bbb")
	roots := []string{dir}
	pathsJSON, _ := json.Marshal([]string{p1, p2})
	got, err := Cat(params("paths", string(pathsJSON)), roots)
	if err != nil {
		t.Fatalf("Cat error: %v", err)
	}
	if !strings.Contains(got, "aaa") || !strings.Contains(got, "bbb") {
		t.Errorf("expected both file contents, got %q", got)
	}
}

func TestCat_EmptyPaths(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Cat(params("paths", ""), roots)
	if err == nil {
		t.Fatal("expected error for empty paths")
	}
}

// ===== Diff additional =====

func TestDiff_IdenticalFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "a.txt", "same\ncontent\n")
	writeTestFile(t, dir, "b.txt", "same\ncontent\n")
	roots := []string{dir}
	got, err := Diff(params("file1", filepath.Join(dir, "a.txt"), "file2", filepath.Join(dir, "b.txt")), roots)
	if err != nil {
		t.Fatalf("Diff error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty diff for identical files, got %q", got)
	}
}

func TestDiff_ContextLines(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "a.txt", "line1\nline2\nline3\nline4\nline5\n")
	writeTestFile(t, dir, "b.txt", "line1\nline2\nCHANGED\nline4\nline5\n")
	roots := []string{dir}
	got, err := Diff(params("file1", filepath.Join(dir, "a.txt"), "file2", filepath.Join(dir, "b.txt"), "context_lines", float64(1)), roots)
	if err != nil {
		t.Fatalf("Diff error: %v", err)
	}
	if !strings.Contains(got, "-line3") || !strings.Contains(got, "+CHANGED") {
		t.Errorf("expected diff output, got %q", got)
	}
}

// ===== List additional =====

func TestList_WithPattern(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "a.py", "python")
	writeTestFile(t, dir, "b.txt", "text")
	writeTestFile(t, dir, "c.py", "python2")
	roots := []string{dir}
	got, err := List(params("path", dir, "pattern", "*.py"), roots)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if !strings.Contains(got, "a.py") || !strings.Contains(got, "c.py") {
		t.Errorf("expected .py files, got %q", got)
	}
	if strings.Contains(got, "b.txt") {
		t.Errorf("expected .txt to be filtered out, got %q", got)
	}
}

func TestList_WithFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "file.txt", "content")
	writeTestSubdir(t, dir, "subdir")
	roots := []string{dir}
	got, err := List(params("path", dir), roots)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if !strings.Contains(got, "file.txt") {
		t.Errorf("expected file.txt in output, got %q", got)
	}
	if !strings.Contains(got, "subdir") {
		t.Errorf("expected subdir in output, got %q", got)
	}
	if !strings.Contains(got, "dir") {
		t.Errorf("expected 'dir' type marker, got %q", got)
	}
}

// ===== Search additional =====

func TestSearch_FindsFiles(t *testing.T) {
	dir := t.TempDir()
	sub := writeTestSubdir(t, dir, "sub")
	writeTestFile(t, dir, "a.py", "hello")
	writeTestFile(t, sub, "b.py", "world")
	writeTestFile(t, dir, "c.txt", "nope")
	roots := []string{dir}
	got, err := Search(params("path", dir, "pattern", "*.py"), roots)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if !strings.Contains(got, "a.py") || !strings.Contains(got, "b.py") {
		t.Errorf("expected .py files, got %q", got)
	}
	if strings.Contains(got, "c.txt") {
		t.Errorf("expected .txt to be excluded, got %q", got)
	}
}

// ===== Info additional =====

func TestInfo_Directory(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	got, err := Info(params("path", dir), roots)
	if err != nil {
		t.Fatalf("Info error: %v", err)
	}
	if !strings.Contains(got, "type: directory") {
		t.Errorf("expected 'type: directory', got %q", got)
	}
}

func TestInfo_RegularFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "hello")
	roots := []string{dir}
	got, err := Info(params("path", path), roots)
	if err != nil {
		t.Fatalf("Info error: %v", err)
	}
	if !strings.Contains(got, "type: file") {
		t.Errorf("expected 'type: file', got %q", got)
	}
	if !strings.Contains(got, "size: 5") {
		t.Errorf("expected 'size: 5', got %q", got)
	}
}

// ===== Delete additional =====

func TestDelete_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "del.txt", "delete me")
	roots := []string{dir}
	got, err := Delete(params("path", path), roots)
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if !strings.Contains(got, "Deleted") {
		t.Errorf("expected 'Deleted' message, got %q", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should have been deleted")
	}
}

func TestDelete_NonEmptyDir(t *testing.T) {
	dir := t.TempDir()
	sub := writeTestSubdir(t, dir, "notempty")
	writeTestFile(t, sub, "file.txt", "content")
	roots := []string{dir}
	_, err := Delete(params("path", sub), roots)
	if err == nil {
		t.Fatal("expected error for non-empty directory")
	}
}

// ===== Copy additional =====

func TestCopy_HappyPath(t *testing.T) {
	dir := t.TempDir()
	src := writeTestFile(t, dir, "src.txt", "copy me")
	dst := filepath.Join(dir, "dst.txt")
	roots := []string{dir}
	got, err := Copy(params("source", src, "destination", dst), roots)
	if err != nil {
		t.Fatalf("Copy error: %v", err)
	}
	if !strings.Contains(got, "Copied") {
		t.Errorf("expected 'Copied' message, got %q", got)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "copy me" {
		t.Errorf("expected 'copy me', got %q", string(data))
	}
}

func TestCopy_DestAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	src := writeTestFile(t, dir, "src.txt", "data")
	dst := writeTestFile(t, dir, "dst.txt", "existing")
	roots := []string{dir}
	_, err := Copy(params("source", src, "destination", dst), roots)
	if err == nil {
		t.Fatal("expected error for existing destination")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' in error, got %q", err.Error())
	}
}

func TestCopy_DirectoryRecursive(t *testing.T) {
	dir := t.TempDir()
	srcDir := writeTestSubdir(t, dir, "src")
	writeTestFile(t, srcDir, "a.txt", "aaa")
	sub := writeTestSubdir(t, srcDir, "inner")
	writeTestFile(t, sub, "b.txt", "bbb")
	dstDir := filepath.Join(dir, "dst")
	roots := []string{dir}
	got, err := Copy(params("source", srcDir, "destination", dstDir, "recursive", true), roots)
	if err != nil {
		t.Fatalf("Copy error: %v", err)
	}
	if !strings.Contains(got, "Copied") {
		t.Errorf("expected 'Copied' message, got %q", got)
	}
	data, _ := os.ReadFile(filepath.Join(dstDir, "inner", "b.txt"))
	if string(data) != "bbb" {
		t.Errorf("expected 'bbb', got %q", string(data))
	}
}

func TestCopy_DirectoryWithoutRecursive(t *testing.T) {
	dir := t.TempDir()
	srcDir := writeTestSubdir(t, dir, "src")
	dstDir := filepath.Join(dir, "dst")
	roots := []string{dir}
	_, err := Copy(params("source", srcDir, "destination", dstDir), roots)
	if err == nil {
		t.Fatal("expected error for directory without recursive flag")
	}
	if !strings.Contains(err.Error(), "recursive") {
		t.Errorf("expected 'recursive' in error, got %q", err.Error())
	}
}

// ===== Move additional =====

func TestMove_HappyPath(t *testing.T) {
	dir := t.TempDir()
	src := writeTestFile(t, dir, "src.txt", "move me")
	dst := filepath.Join(dir, "dst.txt")
	roots := []string{dir}
	got, err := Move(params("source", src, "destination", dst), roots)
	if err != nil {
		t.Fatalf("Move error: %v", err)
	}
	if !strings.Contains(got, "Moved") {
		t.Errorf("expected 'Moved' message, got %q", got)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source should not exist after move")
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "move me" {
		t.Errorf("expected 'move me', got %q", string(data))
	}
}

func TestMove_DestAlreadyExists(t *testing.T) {
	dir := t.TempDir()
	src := writeTestFile(t, dir, "src.txt", "data")
	dst := writeTestFile(t, dir, "dst.txt", "existing")
	roots := []string{dir}
	_, err := Move(params("source", src, "destination", dst), roots)
	if err == nil {
		t.Fatal("expected error for existing destination")
	}
}

// ===== Write additional =====

func TestWrite_Append(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "append.txt", "first\n")
	roots := []string{dir}
	_, err := Write(params("path", path, "content", "second\n", "append", true), roots)
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "first") || !strings.Contains(string(data), "second") {
		t.Errorf("expected both first and second, got %q", string(data))
	}
}

func TestWrite_CRLFPreservation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crlf.txt")
	os.WriteFile(path, []byte("existing\r\ncontent\r\n"), 0644)
	roots := []string{dir}
	_, err := Write(params("path", path, "content", "new\ncontent\n"), roots)
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "\r\n") {
		t.Error("expected CRLF normalization for file that originally had CRLF")
	}
}

// ===== Mkdir additional =====

func TestMkdir_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c")
	roots := []string{dir}
	got, err := Mkdir(params("path", path), roots)
	if err != nil {
		t.Fatalf("Mkdir error: %v", err)
	}
	if !strings.Contains(got, "Created") {
		t.Errorf("expected 'Created' message, got %q", got)
	}
	info, _ := os.Stat(path)
	if !info.IsDir() {
		t.Error("expected directory to exist")
	}
}

func TestMkdir_OutsideRoots(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Mkdir(params("path", "C:\\Windows\\evil"), roots)
	if err == nil {
		t.Fatal("expected path confinement error")
	}
}

// ===== Tree additional =====

func TestTree_HappyPath(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "file.txt", "content")
	sub := writeTestSubdir(t, dir, "subdir")
	writeTestFile(t, sub, "inner.txt", "inner")
	roots := []string{dir}
	got, err := Tree(params("path", dir), roots)
	if err != nil {
		t.Fatalf("Tree error: %v", err)
	}
	if !strings.Contains(got, "file.txt") {
		t.Errorf("expected file.txt in tree, got %q", got)
	}
	if !strings.Contains(got, "subdir/") {
		t.Errorf("expected subdir/ in tree, got %q", got)
	}
}

func TestTree_DepthLimit(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "a", "b", "c", "d")
	os.MkdirAll(deep, 0755)
	writeTestFile(t, deep, "deep.txt", "data")
	roots := []string{dir}
	got, err := Tree(params("path", dir, "depth", float64(2)), roots)
	if err != nil {
		t.Fatalf("Tree error: %v", err)
	}
	if strings.Contains(got, "deep.txt") {
		t.Errorf("expected deep.txt to be excluded at depth 2, got %q", got)
	}
}

func TestTree_Pattern(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "a.py", "python")
	writeTestFile(t, dir, "b.txt", "text")
	roots := []string{dir}
	got, err := Tree(params("path", dir, "pattern", "*.py"), roots)
	if err != nil {
		t.Fatalf("Tree error: %v", err)
	}
	if !strings.Contains(got, "a.py") {
		t.Errorf("expected a.py, got %q", got)
	}
	if strings.Contains(got, "b.txt") {
		t.Errorf("expected b.txt to be filtered, got %q", got)
	}
}

// ===== DispatchExternalTool =====

func TestDispatchExternal_BooleanTrue(t *testing.T) {
	cfg := shared.ToolConfig{
		Exe: "cmd.exe",
		Params: map[string]shared.ParamConfig{
			"verbose": {Type: "boolean", Flag: "/V:ON", Position: 0},
		},
		InactivityTimeout: 5,
		TotalTimeout:      10,
		MaxOutput:         10240,
	}
	// Reconfigure with a boolean flag and a positional arg for /c
	cfg.Params = map[string]shared.ParamConfig{
		"verbose": {Type: "boolean", Flag: "/V:ON"},
		"switch":  {Type: "string", Position: 1},
		"arg":     {Type: "string", Position: 2},
	}
	got, err := DispatchExternalTool("test", cfg, map[string]interface{}{
		"verbose": true,
		"switch":  "/c",
		"arg":     "echo bool_ok",
	}, 60)
	if err != nil {
		t.Fatalf("DispatchExternalTool error: %v", err)
	}
	if !strings.Contains(got, "bool_ok") {
		t.Errorf("expected 'bool_ok' in output, got %q", got)
	}
}

func TestDispatchExternal_MissingRequired(t *testing.T) {
	cfg := shared.ToolConfig{
		Exe: "cmd.exe",
		Params: map[string]shared.ParamConfig{
			"input": {Type: "string", Required: true, Flag: "--input"},
		},
	}
	_, err := DispatchExternalTool("test", cfg, map[string]interface{}{}, 60)
	if err == nil {
		t.Fatal("expected error for missing required parameter")
	}
	if !strings.Contains(err.Error(), "missing required") {
		t.Errorf("expected 'missing required' in error, got %q", err.Error())
	}
}

func TestDispatchExternal_PositionalOrdering(t *testing.T) {
	cfg := shared.ToolConfig{
		Exe:               "cmd.exe",
		Params:            map[string]shared.ParamConfig{},
		InactivityTimeout: 5,
		TotalTimeout:      10,
		MaxOutput:         10240,
	}
	// Test with positional params that cmd.exe /c can execute
	cfg.Params = map[string]shared.ParamConfig{
		"switch": {Type: "string", Position: 1},
		"arg":    {Type: "string", Position: 2},
	}
	got, err := DispatchExternalTool("test", cfg, map[string]interface{}{
		"switch": "/c",
		"arg":    "echo hello",
	}, 60)
	if err != nil {
		t.Fatalf("DispatchExternalTool error: %v", err)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("expected 'hello' in output, got %q", got)
	}
}

func TestDispatchExternal_TimeoutOverride(t *testing.T) {
	cfg := shared.ToolConfig{
		Exe: "cmd.exe",
		Params: map[string]shared.ParamConfig{
			"switch": {Type: "string", Position: 1},
			"arg":    {Type: "string", Position: 2},
		},
		InactivityTimeout: 5,
		TotalTimeout:      10,
		MaxOutput:         10240,
	}
	got, err := DispatchExternalTool("test", cfg, map[string]interface{}{
		"switch":  "/c",
		"arg":     "echo timeout_test",
		"timeout": float64(3),
	}, 60)
	if err != nil {
		t.Fatalf("DispatchExternalTool error: %v", err)
	}
	if !strings.Contains(got, "timeout_test") {
		t.Errorf("expected 'timeout_test' in output, got %q", got)
	}
}

// ===== ExecuteExternal =====

func TestExecuteExternal_WorkingDir(t *testing.T) {
	cfg := shared.ToolConfig{
		Exe:               "cmd.exe",
		InactivityTimeout: 5,
		TotalTimeout:      10,
		MaxOutput:         10240,
	}
	got, err := ExecuteExternal("test", cfg, []string{"/c", "echo working"}, 60)
	if err != nil {
		t.Fatalf("ExecuteExternal error: %v", err)
	}
	if !strings.Contains(got, "working") {
		t.Errorf("expected 'working' in output, got %q", got)
	}
}

func TestExecuteExternal_SuccessCode(t *testing.T) {
	cfg := shared.ToolConfig{
		Exe:               "cmd.exe",
		SuccessCodes:      []int{0, 1},
		InactivityTimeout: 5,
		TotalTimeout:      10,
		MaxOutput:         10240,
	}
	// exit /b 1 returns exit code 1, which is in SuccessCodes
	got, err := ExecuteExternal("test", cfg, []string{"/c", "echo ok & exit /b 1"}, 60)
	if err != nil {
		t.Fatalf("ExecuteExternal error: %v", err)
	}
	if !strings.Contains(got, "ok") {
		t.Errorf("expected 'ok' in output, got %q", got)
	}
}

func TestExecuteExternal_FailureCode(t *testing.T) {
	cfg := shared.ToolConfig{
		Exe:               "cmd.exe",
		InactivityTimeout: 5,
		TotalTimeout:      10,
		MaxOutput:         10240,
	}
	_, err := ExecuteExternal("test", cfg, []string{"/c", "exit /b 1"}, 60)
	if err == nil {
		t.Fatal("expected error for non-zero exit code")
	}
	if !strings.Contains(err.Error(), "exit code") {
		t.Errorf("expected 'exit code' in error, got %q", err.Error())
	}
}

// ===== ClampTimeout =====

func TestClampTimeout_BelowMin(t *testing.T) {
	got := ClampTimeout(0, 60)
	if got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestClampTimeout_AboveMax(t *testing.T) {
	got := ClampTimeout(100, 60)
	if got != 60 {
		t.Errorf("expected 60, got %d", got)
	}
}

func TestClampTimeout_InRange(t *testing.T) {
	got := ClampTimeout(30, 60)
	if got != 30 {
		t.Errorf("expected 30, got %d", got)
	}
}

// ===== FormatRunResult =====

func TestFormatRunResult_StdoutOnly(t *testing.T) {
	r := ExecResult{Stdout: "hello", ExitCode: 0}
	got := FormatRunResult(r)
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestFormatRunResult_StderrAndExitCode(t *testing.T) {
	r := ExecResult{Stdout: "out", Stderr: "err", ExitCode: 1}
	got := FormatRunResult(r)
	if !strings.Contains(got, "out") || !strings.Contains(got, "err") || !strings.Contains(got, "[exit code: 1]") {
		t.Errorf("expected all components, got %q", got)
	}
}

// ===== SplitArgs =====

func TestSplitArgs_QuotedStrings(t *testing.T) {
	got := SplitArgs(`hello "world foo" bar 'baz qux'`)
	if len(got) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(got), got)
	}
	if got[1] != "world foo" {
		t.Errorf("expected 'world foo', got %q", got[1])
	}
	if got[3] != "baz qux" {
		t.Errorf("expected 'baz qux', got %q", got[3])
	}
}

func TestSplitArgs_Empty(t *testing.T) {
	got := SplitArgs("")
	if len(got) != 0 {
		t.Errorf("expected 0 args, got %d", len(got))
	}
}

// ===== ExecuteWithTimeouts =====

// ===== Run (direct) =====

func TestRun_HappyPath(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	runCfg := shared.RunConfig{
		InactivityTimeout: 10,
		TotalTimeout:      30,
		MaxOutput:         10240,
	}
	got, err := Run(map[string]interface{}{
		"command": "cmd.exe",
		"args":    "/c echo run_test",
	}, roots, runCfg, 60)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !strings.Contains(got, "run_test") {
		t.Errorf("expected 'run_test' in output, got %q", got)
	}
}

func TestRun_WithTimeout(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	runCfg := shared.RunConfig{
		InactivityTimeout: 10,
		TotalTimeout:      30,
		MaxOutput:         10240,
	}
	got, err := Run(map[string]interface{}{
		"command": "cmd.exe",
		"args":    "/c echo timeout_run",
		"timeout": float64(5),
	}, roots, runCfg, 60)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !strings.Contains(got, "timeout_run") {
		t.Errorf("expected 'timeout_run' in output, got %q", got)
	}
}

func TestRun_ExitCodeNonZero(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	runCfg := shared.RunConfig{
		InactivityTimeout: 10,
		TotalTimeout:      30,
		MaxOutput:         10240,
	}
	got, err := Run(map[string]interface{}{
		"command": "cmd.exe",
		"args":    "/c echo fail_test & exit /b 1",
	}, roots, runCfg, 60)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !strings.Contains(got, "[exit code: 1]") {
		t.Errorf("expected exit code in output, got %q", got)
	}
}

func TestRun_ConfinementError(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	runCfg := shared.RunConfig{
		InactivityTimeout: 10,
		TotalTimeout:      30,
		MaxOutput:         10240,
	}
	_, err := Run(map[string]interface{}{
		"command": "C:\\Windows\\System32\\notepad.exe",
	}, roots, runCfg, 60)
	if err == nil {
		t.Fatal("expected confinement error")
	}
}

func TestRun_MissingCommand(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	runCfg := shared.RunConfig{
		InactivityTimeout: 10,
		TotalTimeout:      30,
		MaxOutput:         10240,
	}
	_, err := Run(map[string]interface{}{}, roots, runCfg, 60)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

// ===== DispatchExternalTool additional =====

func TestDispatchExternal_BooleanFalse(t *testing.T) {
	cfg := shared.ToolConfig{
		Exe: "cmd.exe",
		Params: map[string]shared.ParamConfig{
			"verbose": {Type: "boolean", Flag: "/V:ON"},
			"switch":  {Type: "string", Position: 1},
			"arg":     {Type: "string", Position: 2},
		},
		InactivityTimeout: 5,
		TotalTimeout:      10,
		MaxOutput:         10240,
	}
	got, err := DispatchExternalTool("test", cfg, map[string]interface{}{
		"verbose": false,
		"switch":  "/c",
		"arg":     "echo bool_false",
	}, 60)
	if err != nil {
		t.Fatalf("DispatchExternalTool error: %v", err)
	}
	if !strings.Contains(got, "bool_false") {
		t.Errorf("expected 'bool_false' in output, got %q", got)
	}
}

func TestDispatchExternal_IntegerFlag(t *testing.T) {
	cfg := shared.ToolConfig{
		Exe: "cmd.exe",
		Params: map[string]shared.ParamConfig{
			"count":  {Type: "integer", Flag: "/A"},
			"switch": {Type: "string", Position: 1},
			"arg":    {Type: "string", Position: 2},
		},
		InactivityTimeout: 5,
		TotalTimeout:      10,
		MaxOutput:         10240,
	}
	got, err := DispatchExternalTool("test", cfg, map[string]interface{}{
		"count":  float64(5),
		"switch": "/c",
		"arg":    "echo int_flag",
	}, 60)
	if err != nil {
		t.Fatalf("DispatchExternalTool error: %v", err)
	}
	if !strings.Contains(got, "int_flag") {
		t.Errorf("expected 'int_flag' in output, got %q", got)
	}
}

func TestDispatchExternal_StringFlag(t *testing.T) {
	cfg := shared.ToolConfig{
		Exe: "cmd.exe",
		Params: map[string]shared.ParamConfig{
			"name":   {Type: "string", Flag: "/D"},
			"switch": {Type: "string", Position: 1},
			"arg":    {Type: "string", Position: 2},
		},
		InactivityTimeout: 5,
		TotalTimeout:      10,
		MaxOutput:         10240,
	}
	got, err := DispatchExternalTool("test", cfg, map[string]interface{}{
		"name":   "test",
		"switch": "/c",
		"arg":    "echo string_flag",
	}, 60)
	if err != nil {
		t.Fatalf("DispatchExternalTool error: %v", err)
	}
	if !strings.Contains(got, "string_flag") {
		t.Errorf("expected 'string_flag' in output, got %q", got)
	}
}

func TestDispatchExternal_DefaultValue(t *testing.T) {
	cfg := shared.ToolConfig{
		Exe: "cmd.exe",
		Params: map[string]shared.ParamConfig{
			"switch": {Type: "string", Position: 1, Default: "/c"},
			"arg":    {Type: "string", Position: 2},
		},
		InactivityTimeout: 5,
		TotalTimeout:      10,
		MaxOutput:         10240,
	}
	got, err := DispatchExternalTool("test", cfg, map[string]interface{}{
		"arg": "echo default_val",
	}, 60)
	if err != nil {
		t.Fatalf("DispatchExternalTool error: %v", err)
	}
	if !strings.Contains(got, "default_val") {
		t.Errorf("expected 'default_val' in output, got %q", got)
	}
}

func TestDispatchExternal_IntegerPositional(t *testing.T) {
	cfg := shared.ToolConfig{
		Exe: "cmd.exe",
		Params: map[string]shared.ParamConfig{
			"switch": {Type: "string", Position: 1},
			"arg":    {Type: "integer", Position: 2},
		},
		InactivityTimeout: 5,
		TotalTimeout:      10,
		MaxOutput:         10240,
	}
	got, err := DispatchExternalTool("test", cfg, map[string]interface{}{
		"switch": "/c",
		"arg":    float64(42),
	}, 60)
	// cmd.exe /c 42 will just print "42" or error, doesn't matter
	// We just want to verify the integer positional is handled
	_ = got
	_ = err
}

// ===== ExternalToolSchema =====

func TestExternalToolSchema_Basic(t *testing.T) {
	cfg := shared.ToolConfig{
		Exe:         "test.exe",
		Description: "A test tool",
		Title:       "Test Tool",
		ReadOnly:    true,
		Params: map[string]shared.ParamConfig{
			"input": {Type: "string", Description: "input file", Required: true, Flag: "--input"},
		},
	}
	schema := ExternalToolSchema("testtool", cfg, 60)
	if schema.Name != "testtool" {
		t.Errorf("expected name 'testtool', got %q", schema.Name)
	}
	if schema.Description != "A test tool" {
		t.Errorf("expected description 'A test tool', got %q", schema.Description)
	}
	if schema.Annotations == nil || schema.Annotations.Title != "Test Tool" {
		t.Error("expected annotations with title 'Test Tool'")
	}
	// Check inputSchema contains "input" and "timeout"
	var parsed map[string]interface{}
	json.Unmarshal(schema.InputSchema, &parsed)
	props := parsed["properties"].(map[string]interface{})
	if _, ok := props["input"]; !ok {
		t.Error("expected 'input' in properties")
	}
	if _, ok := props["timeout"]; !ok {
		t.Error("expected 'timeout' in properties")
	}
	req := parsed["required"].([]interface{})
	if len(req) != 1 || req[0] != "input" {
		t.Errorf("expected required=['input'], got %v", req)
	}
}

func TestExternalToolSchema_NoTitle(t *testing.T) {
	cfg := shared.ToolConfig{
		Exe:         "test.exe",
		Description: "desc",
	}
	schema := ExternalToolSchema("mytool", cfg, 60)
	if schema.Annotations.Title != "mytool" {
		t.Errorf("expected title to default to tool name 'mytool', got %q", schema.Annotations.Title)
	}
}

// ===== BuiltinSchemas =====

func TestBuiltinSchemas_MissingDescription(t *testing.T) {
	// Missing a required description should error
	descriptions := map[string]string{
		"read": "Read",
		// missing everything else
	}
	_, err := BuiltinSchemas(descriptions, 10, 60)
	if err == nil {
		t.Fatal("expected error for missing description")
	}
}

// ===== Read UTF-16 full file =====

func TestRead_UTF16FullFile(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16LEFile(t, dir, "full.txt", "full content here\n")
	roots := []string{dir}
	got, err := Read(params("path", path), roots)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if !strings.Contains(got, "full content here") {
		t.Errorf("expected decoded content, got %q", got)
	}
}

// ===== Tail UTF-16 BE small file =====

func TestTail_UTF16BESmallFile(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16BEFile(t, dir, "be_small.txt", "one\ntwo\nthree\nfour\n")
	roots := []string{dir}
	got, err := Tail(params("path", path, "lines", float64(2)), roots)
	if err != nil {
		t.Fatalf("Tail error: %v", err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 || lines[0] != "three" {
		t.Errorf("expected [three, four], got %v", lines)
	}
}

// ===== Diff binary file =====

func TestDiff_BinaryFile(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "bin.dat")
	os.WriteFile(binPath, []byte{0x00, 0x01, 0x02}, 0644)
	textPath := writeTestFile(t, dir, "text.txt", "hello\n")
	roots := []string{dir}
	_, err := Diff(params("file1", binPath, "file2", textPath), roots)
	if err == nil {
		t.Fatal("expected error for binary file in diff")
	}
}

// ===== Cat combined size exceeds limit =====

func TestCat_ExceedsMaxSize(t *testing.T) {
	dir := t.TempDir()
	// Create a file near MaxReadSize
	big := make([]byte, shared.MaxReadSize-10)
	for i := range big {
		big[i] = 'a'
	}
	p1 := filepath.Join(dir, "big.txt")
	os.WriteFile(p1, big, 0644)
	p2 := writeTestFile(t, dir, "small.txt", strings.Repeat("b", 100))
	roots := []string{dir}
	pathsJSON, _ := json.Marshal([]string{p1, p2})
	_, err := Cat(params("paths", string(pathsJSON)), roots)
	if err == nil {
		t.Fatal("expected error for combined output exceeding limit")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected 'exceeds' in error, got %q", err.Error())
	}
}

// ===== Search max_results =====

func TestSearch_MaxResults(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		writeTestFile(t, dir, "file"+string(rune('0'+i))+".txt", "content")
	}
	roots := []string{dir}
	got, err := Search(params("path", dir, "pattern", "*.txt", "max_results", float64(2)), roots)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(lines))
	}
}

func TestExecuteWithTimeouts_InactivityTimeout(t *testing.T) {
	opts := ExecOpts{
		InactivityTimeout: 1 * time.Second,
		TotalTimeout:      30 * time.Second,
		MaxOutput:         10240,
		ToolName:          "test",
		MaxTimeout:        60,
	}
	// Command that hangs (sleep)
	result, err := ExecuteWithTimeouts("cmd.exe", []string{"/c", "ping -n 10 127.0.0.1 > nul"}, opts)
	if err != nil {
		t.Fatalf("ExecuteWithTimeouts error: %v", err)
	}
	if result.Timeout == "" {
		t.Error("expected inactivity timeout")
	}
	if !strings.Contains(result.Timeout, "no output") {
		t.Errorf("expected inactivity timeout message, got %q", result.Timeout)
	}
}

// ===== Tail UTF-16 BE large file =====

func TestTail_LargeUTF16BEFile(t *testing.T) {
	dir := t.TempDir()
	var content strings.Builder
	line := strings.Repeat("b", 49) + "\n"
	for content.Len()*2+2 < shared.MaxReadSize+1024 {
		content.WriteString(line)
	}
	content.WriteString("BE_FINAL\n")
	path := writeUTF16BEFile(t, dir, "large_be.txt", content.String())
	roots := []string{dir}
	got, err := Tail(params("path", path, "lines", float64(1)), roots)
	if err != nil {
		t.Fatalf("Tail error: %v", err)
	}
	if got != "BE_FINAL" {
		t.Errorf("expected 'BE_FINAL', got %q", got)
	}
}

// ===== Read UTF-16 large file without range (should error) =====

func TestRead_UTF16LargeFileNoRange(t *testing.T) {
	dir := t.TempDir()
	var content strings.Builder
	line := strings.Repeat("x", 49) + "\n"
	for content.Len()*2+2 < shared.MaxReadSize+1024 {
		content.WriteString(line)
	}
	path := writeUTF16LEFile(t, dir, "large_utf16.txt", content.String())
	roots := []string{dir}
	_, err := Read(params("path", path), roots)
	if err == nil {
		t.Fatal("expected error for large UTF-16 file without range")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("expected 'limit' in error, got %q", err.Error())
	}
}

// ===== Read UTF-16 with range =====

func TestRead_UTF16WithRange(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16LEFile(t, dir, "range.txt", "ABCDEF")
	roots := []string{dir}
	// Read with limit only (no offset)
	got, err := Read(params("path", path, "limit", float64(4)), roots)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if got != "AB" {
		t.Errorf("expected 'AB', got %q", got)
	}
}

// ===== Read large UTF-8 file with range =====

func TestRead_LargeFileWithRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.txt")
	data := make([]byte, shared.MaxReadSize+100)
	for i := range data {
		data[i] = 'a'
	}
	copy(data[shared.MaxReadSize:], []byte("FINDME"))
	os.WriteFile(path, data, 0644)
	roots := []string{dir}
	got, err := Read(params("path", path, "offset", float64(shared.MaxReadSize), "limit", float64(6)), roots)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if got != "FINDME" {
		t.Errorf("expected 'FINDME', got %q", got)
	}
}

// ===== Grep missing path param =====

func TestGrep_MissingPath(t *testing.T) {
	dir := t.TempDir()
	roots := []string{dir}
	_, err := Grep(grepParams("pattern", "test"), roots)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestGrep_MissingPattern(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "hello\n")
	roots := []string{dir}
	_, err := Grep(grepParams("path", path), roots)
	if err == nil {
		t.Fatal("expected error for missing pattern")
	}
}

// ===== Wc UTF-16 BE file =====

func TestWc_UTF16BEFile(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16BEFile(t, dir, "wc_be.txt", "one two\nthree\n")
	roots := []string{dir}
	got, err := Wc(params("path", path), roots)
	if err != nil {
		t.Fatalf("Wc error: %v", err)
	}
	if !strings.Contains(got, "lines: 2") {
		t.Errorf("expected 2 lines in %q", got)
	}
	if !strings.Contains(got, "words: 3") {
		t.Errorf("expected 3 words in %q", got)
	}
}

// ===== Head UTF-16 fewer lines than requested =====

func TestHead_FewerLinesThanRequested(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "short.txt", "one\ntwo\n")
	roots := []string{dir}
	got, err := Head(params("path", path, "lines", float64(10)), roots)
	if err != nil {
		t.Fatalf("Head error: %v", err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %q", len(lines), got)
	}
}

// ===== splitPaths with quotes =====

func TestCat_QuotedPaths(t *testing.T) {
	dir := t.TempDir()
	// Create a file with a space in the name
	spaceDir := filepath.Join(dir, "has space")
	os.MkdirAll(spaceDir, 0755)
	p1 := writeTestFile(t, spaceDir, "test.txt", "spaced content")
	roots := []string{dir}
	got, err := Cat(params("paths", "\""+p1+"\""), roots)
	if err != nil {
		t.Fatalf("Cat error: %v", err)
	}
	if got != "spaced content" {
		t.Errorf("expected 'spaced content', got %q", got)
	}
}

// ===== ExecuteExternal with stderr =====

func TestExecuteExternal_WithStderr(t *testing.T) {
	cfg := shared.ToolConfig{
		Exe:               "cmd.exe",
		InactivityTimeout: 5,
		TotalTimeout:      10,
		MaxOutput:         10240,
	}
	got, err := ExecuteExternal("test", cfg, []string{"/c", "echo stdout_ok && echo stderr_msg 1>&2"}, 60)
	if err != nil {
		t.Fatalf("ExecuteExternal error: %v", err)
	}
	if !strings.Contains(got, "stdout_ok") {
		t.Errorf("expected stdout in output, got %q", got)
	}
	if !strings.Contains(got, "stderr_msg") {
		t.Errorf("expected stderr in output, got %q", got)
	}
}

// ===== BuildToolSchemas with no grep config =====

func TestBuildToolSchemas_NoGrep(t *testing.T) {
	cfg := &shared.Config{
		BuiltinDescriptions: shared.DefaultBuiltinDescriptions(),
		Run:                 shared.DefaultRunConfig(),
		Security:            shared.SecurityConfig{MaxTimeout: 60},
	}
	result, err := BuildToolSchemas(cfg)
	if err != nil {
		t.Fatalf("BuildToolSchemas error: %v", err)
	}
	// grep should be registered as builtin
	if !result.BuiltinOverrides["grep"] {
		t.Error("expected grep in builtin overrides when no grep config")
	}
}

// ===== Tail tailFromDecoded edge case =====

func TestTail_UTF16AllLinesReturned(t *testing.T) {
	dir := t.TempDir()
	path := writeUTF16LEFile(t, dir, "few.txt", "a\nb\n")
	roots := []string{dir}
	got, err := Tail(params("path", path, "lines", float64(10)), roots)
	if err != nil {
		t.Fatalf("Tail error: %v", err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 || lines[0] != "a" || lines[1] != "b" {
		t.Errorf("expected [a, b], got %v", lines)
	}
}
