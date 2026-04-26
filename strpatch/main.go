// strpatch.exe — find-and-replace on a single text file.
// Reads a JSON object from stdin, performs exact byte matching,
// writes atomically via temp file + rename.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	// MaxFileSize is the maximum file size strpatch will process.
	MaxFileSize = 10 * 1024 * 1024 // 10 MB
	// BinaryScanSize is the number of bytes checked for null bytes.
	BinaryScanSize = 8 * 1024 // 8 KB
	// MaxRetries for file locking.
	MaxRetries = 3
)

// Backoff durations for file lock retries.
var retryBackoffs = [MaxRetries]time.Duration{
	50 * time.Millisecond,
	200 * time.Millisecond,
	500 * time.Millisecond,
}

// Exit codes per spec §5.5.
const (
	ExitSuccess       = 0
	ExitNotFound      = 1
	ExitNotUnique     = 2
	ExitFileError     = 3
	ExitWriteError    = 4
	ExitInputError    = 5
)

// PatchRequest is the JSON input schema.
type PatchRequest struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

func main() {
	os.Exit(run())
}

func run() int {
	req, err := readInput()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return ExitInputError
	}
	code, err := patch(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return code
	}
	return ExitSuccess
}

// readInput reads and validates the JSON request from stdin.
func readInput() (*PatchRequest, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("Failed to read stdin: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("No input received on stdin")
	}
	var req PatchRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("Invalid JSON input on stdin")
	}
	if req.Path == "" {
		return nil, fmt.Errorf("Missing required field: path")
	}
	if req.OldText == "" {
		return nil, fmt.Errorf("Search text must not be empty")
	}
	return &req, nil
}

// patch performs the find-and-replace operation.
func patch(req *PatchRequest) (int, error) {
	buf, err := readFileWithRetry(req.Path)
	if err != nil {
		return ExitFileError, err
	}
	originalSize := len(buf)
	if err := checkRefusals(buf, req.Path); err != nil {
		return ExitFileError, err
	}
	isCRLF := bytes.Contains(buf, []byte("\r\n"))
	search := []byte(req.OldText)
	replace := []byte(req.NewText)
	if isCRLF {
		search = normaliseToCRLF(search)
		replace = normaliseToCRLF(replace)
	}
	idx := bytes.Index(buf, search)
	if idx == -1 {
		return ExitNotFound, fmt.Errorf("Search text not found")
	}
	count := countOccurrences(buf, search)
	if count > 1 {
		return ExitNotUnique, fmt.Errorf("Search text not unique (found %d times)", count)
	}
	result := make([]byte, 0, len(buf)-len(search)+len(replace))
	result = append(result, buf[:idx]...)
	result = append(result, replace...)
	result = append(result, buf[idx+len(search):]...)
	if err := atomicWrite(req.Path, result); err != nil {
		return ExitWriteError, err
	}
	fmt.Fprintf(os.Stdout, "Replaced 1 occurrence in %s (%d → %d bytes)\n", req.Path, originalSize, len(result))
	return ExitSuccess, nil
}

// atomicWrite writes data to a temp file then renames over the target.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".strpatch-*")
	if err != nil {
		return fmt.Errorf("Write failed: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Clean up temp file on any failure.
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("Write failed: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("Write failed: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("Write failed: %w", err)
	}
	tmpName = "" // Rename succeeded, don't remove.
	return nil
}

// checkRefusals validates the file buffer against all refusal conditions.
func checkRefusals(buf []byte, path string) error {
	if len(buf) > MaxFileSize {
		return fmt.Errorf("File exceeds 10 MB size limit")
	}
	if len(buf) >= 2 {
		if buf[0] == 0xFF && buf[1] == 0xFE {
			return fmt.Errorf("File is UTF-16; only UTF-8/ASCII supported")
		}
		if buf[0] == 0xFE && buf[1] == 0xFF {
			return fmt.Errorf("File is UTF-16; only UTF-8/ASCII supported")
		}
	}
	scanLen := BinaryScanSize
	if len(buf) < scanLen {
		scanLen = len(buf)
	}
	if bytes.ContainsRune(buf[:scanLen], 0) {
		return fmt.Errorf("File appears to be binary, not text")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("File not found: %s", path)
	}
	if info.Mode().Perm()&0200 == 0 {
		return fmt.Errorf("File is read-only")
	}
	return nil
}

// countOccurrences returns the number of non-overlapping occurrences of search in buf.
func countOccurrences(buf []byte, search []byte) int {
	count := 0
	offset := 0
	for {
		idx := bytes.Index(buf[offset:], search)
		if idx == -1 {
			return count
		}
		count++
		offset += idx + len(search)
	}
}

// normaliseToCRLF replaces bare \n (not preceded by \r) with \r\n.
func normaliseToCRLF(data []byte) []byte {
	var result []byte
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' && (i == 0 || data[i-1] != '\r') {
			result = append(result, '\r', '\n')
		} else {
			result = append(result, data[i])
		}
	}
	return result
}

// readFileWithRetry reads a file, retrying on sharing violation errors.
func readFileWithRetry(path string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryBackoffs[attempt-1])
		}
		data, err := os.ReadFile(path)
		if err == nil {
			return data, nil
		}
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("File not found: %s", path)
		}
		lastErr = err
		if !isSharingViolation(err) {
			return nil, fmt.Errorf("Read failed: %w", err)
		}
	}
	_ = lastErr
	return nil, fmt.Errorf("File is locked by another process")
}
