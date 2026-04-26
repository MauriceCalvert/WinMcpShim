package installer

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-70: WriteAtomic creates a new file.
func TestWriteAtomic_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	data := []byte("hello world")
	if err := WriteAtomic(path, data); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("content = %q, want %q", got, data)
	}
}

// T-71: WriteAtomic overwrites an existing file.
func TestWriteAtomic_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exist.txt")
	os.WriteFile(path, []byte("old"), 0644)
	data := []byte("new content")
	if err := WriteAtomic(path, data); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, data) {
		t.Errorf("content = %q, want %q", got, data)
	}
}

// T-72: WriteAtomic does not leave .tmp file on success.
func TestWriteAtomic_NoLeftoverTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.txt")
	WriteAtomic(path, []byte("data"))
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".install-tmp-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

// T-73: WriteAtomic original file is intact if .tmp write fails (INV-02).
func TestWriteAtomic_OriginalIntactOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "original.txt")
	original := []byte("precious content")
	os.WriteFile(path, original, 0644)
	// Attempt WriteAtomic to a path whose directory is read-only,
	// so CreateTemp fails. The original file should be untouched.
	badDir := filepath.Join(dir, "readonly")
	os.MkdirAll(badDir, 0755)
	badPath := filepath.Join(badDir, "file.txt")
	os.WriteFile(badPath, []byte("also precious"), 0644)
	os.Chmod(badDir, 0555)
	defer os.Chmod(badDir, 0755)
	err := WriteAtomic(badPath, []byte("new content"))
	if err == nil {
		// On Windows, Chmod(0555) may not prevent writes. Skip if so.
		t.Skip("OS did not enforce read-only directory")
	}
	got, _ := os.ReadFile(badPath)
	if !bytes.Equal(got, []byte("also precious")) {
		t.Errorf("original file modified after failed WriteAtomic: %q", got)
	}
}

// T-74: WriteAtomic handles path with spaces (INV-03).
func TestWriteAtomic_PathWithSpaces(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "path with spaces")
	os.MkdirAll(subdir, 0755)
	path := filepath.Join(subdir, "file.txt")
	data := []byte("spaces ok")
	if err := WriteAtomic(path, data); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, data) {
		t.Errorf("content = %q, want %q", got, data)
	}
}

// T-75: BackupFile creates timestamped copy (INS-16).
func TestBackupFile_CreatesTimestampedCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte(`{"test":true}`), 0644)
	bakPath, err := BackupFile(path)
	if err != nil {
		t.Fatalf("BackupFile: %v", err)
	}
	if !strings.Contains(bakPath, ".bak.") {
		t.Errorf("backup path %q does not contain .bak.", bakPath)
	}
	if _, err := os.Stat(bakPath); err != nil {
		t.Errorf("backup file does not exist: %v", err)
	}
}

// T-76: BackupFile content matches original byte-for-byte.
func TestBackupFile_ContentMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	original := []byte{0x00, 0xFF, 0x42, 0x0A, 0x0D}
	os.WriteFile(path, original, 0644)
	bakPath, _ := BackupFile(path)
	got, _ := os.ReadFile(bakPath)
	if !bytes.Equal(got, original) {
		t.Errorf("backup content differs from original")
	}
}

// T-77: BackupFile does not overwrite existing backup (appends suffix).
func TestBackupFile_AppendsSuffix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	os.WriteFile(path, []byte("v1"), 0644)
	bak1, _ := BackupFile(path)
	// Create a collision by writing same-second backup
	os.WriteFile(path, []byte("v2"), 0644)
	// Force collision: create the expected next backup path
	os.WriteFile(bak1, []byte("occupied"), 0644)
	bak2, err := BackupFile(path)
	if err != nil {
		t.Fatalf("BackupFile second call: %v", err)
	}
	if bak1 == bak2 {
		t.Errorf("second backup overwrote first: both are %s", bak1)
	}
}
