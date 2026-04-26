package shared

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
)

// IsNotExist reports whether err indicates a file or path was not found.
// Checks os.IsNotExist, then unwraps to syscall.Errno for Windows
// ERROR_FILE_NOT_FOUND (2) and ERROR_PATH_NOT_FOUND (3) which
// os.IsNotExist may miss on Go 1.25+.
func IsNotExist(err error) bool {
	if os.IsNotExist(err) {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == 2 || errno == 3 // ERROR_FILE_NOT_FOUND, ERROR_PATH_NOT_FOUND
	}
	return false
}

// RequireString extracts a required string parameter.
func RequireString(params map[string]interface{}, key string) (string, error) {
	v, ok := params[key]
	if !ok {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("parameter %s must be a string", key)
	}
	return s, nil
}

// OptionalBool extracts an optional boolean parameter.
func OptionalBool(params map[string]interface{}, key string) (bool, bool) {
	v, ok := params[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

// OptionalInt extracts an optional integer parameter.
func OptionalInt(params map[string]interface{}, key string) (int, bool) {
	v, ok := params[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		if n > float64(math.MaxInt) || n < float64(math.MinInt) || n != math.Trunc(n) {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

// OptionalString extracts an optional string parameter.
func OptionalString(params map[string]interface{}, key string) (string, bool) {
	v, ok := params[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// AtomicWrite writes data to a temp file then renames over the target (§9.3).
func AtomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".shim-write-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tmpName = ""
	return nil
}

// TextEncoding represents the detected encoding of a text file.
type TextEncoding int

const (
	// UTF8 is plain UTF-8 (or ASCII).
	UTF8 TextEncoding = iota
	// UTF16LE is UTF-16 Little Endian (BOM: FF FE).
	UTF16LE
	// UTF16BE is UTF-16 Big Endian (BOM: FE FF).
	UTF16BE
)

// DetectTextEncoding detects whether data is UTF-8, UTF-16LE, UTF-16BE, or binary.
// Returns an error for binary files (null bytes in header with no UTF-16 BOM).
func DetectTextEncoding(header []byte) (TextEncoding, error) {
	if len(header) >= 2 {
		if header[0] == 0xFF && header[1] == 0xFE {
			return UTF16LE, nil
		}
		if header[0] == 0xFE && header[1] == 0xFF {
			return UTF16BE, nil
		}
	}
	scanLen := BinaryScanSize
	if len(header) < scanLen {
		scanLen = len(header)
	}
	if bytes.ContainsRune(header[:scanLen], 0) {
		return UTF8, fmt.Errorf("File appears to be binary, not text")
	}
	return UTF8, nil
}

// DecodeUTF16 decodes UTF-16 encoded bytes (after BOM detection) to a UTF-8 string.
// The 2-byte BOM must be present at the start and is stripped.
// Returns an error if the data has an odd byte count or contains malformed surrogates.
func DecodeUTF16(data []byte, enc TextEncoding) (string, error) {
	if enc != UTF16LE && enc != UTF16BE {
		return "", fmt.Errorf("DecodeUTF16 called with non-UTF-16 encoding")
	}
	if len(data) < 2 {
		return "", fmt.Errorf("UTF-16 data too short for BOM")
	}
	// Strip BOM
	data = data[2:]
	if len(data)%2 != 0 {
		return "", fmt.Errorf("UTF-16 data has odd byte count (%d bytes after BOM)", len(data))
	}
	unitCount := len(data) / 2
	u16 := make([]uint16, unitCount)
	var byteOrder binary.ByteOrder
	if enc == UTF16LE {
		byteOrder = binary.LittleEndian
	} else {
		byteOrder = binary.BigEndian
	}
	for i := 0; i < unitCount; i++ {
		u16[i] = byteOrder.Uint16(data[i*2 : i*2+2])
	}
	runes := utf16.Decode(u16)
	var sb strings.Builder
	sb.Grow(len(runes))
	for _, r := range runes {
		sb.WriteRune(r)
	}
	return sb.String(), nil
}

// CheckTextFile checks for binary and UTF-16 content (§8.4).
func CheckTextFile(data []byte) error {
	if len(data) >= 2 {
		if (data[0] == 0xFF && data[1] == 0xFE) || (data[0] == 0xFE && data[1] == 0xFF) {
			return fmt.Errorf("File is UTF-16 encoded; only UTF-8 and ASCII are supported")
		}
	}
	scanLen := BinaryScanSize
	if len(data) < scanLen {
		scanLen = len(data)
	}
	if bytes.ContainsRune(data[:scanLen], 0) {
		return fmt.Errorf("File appears to be binary, not text")
	}
	return nil
}

// NormaliseToCRLF replaces bare \n (not preceded by \r) with \r\n.
func NormaliseToCRLF(data []byte) []byte {
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

// OpenFileWithRetry opens a file for reading with sharing violation retry (§9.2).
func OpenFileWithRetry(path string) (*os.File, error) {
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(RetryBackoffs[attempt-1])
		}
		f, err := os.Open(path)
		if err == nil {
			return f, nil
		}
		if IsNotExist(err) {
			return nil, fmt.Errorf("File not found: %s", path)
		}
		if !IsSharingViolation(err) {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
	}
	return nil, fmt.Errorf("File is locked by another process")
}

// ReadFileWithRetry reads a file with sharing violation retry (§9.2).
func ReadFileWithRetry(path string) ([]byte, error) {
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(RetryBackoffs[attempt-1])
		}
		data, err := os.ReadFile(path)
		if err == nil {
			return data, nil
		}
		if IsNotExist(err) {
			return nil, fmt.Errorf("File not found %s: %w", path, err)
		}
		if !IsSharingViolation(err) {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
	}
	return nil, fmt.Errorf("File is locked by another process")
}

// StatWithRetry stats a file with sharing violation retry (§9.2).
func StatWithRetry(path string) (os.FileInfo, error) {
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(RetryBackoffs[attempt-1])
		}
		info, err := os.Stat(path)
		if err == nil {
			return info, nil
		}
		if IsNotExist(err) {
			return nil, fmt.Errorf("File not found: %s", path)
		}
		if !IsSharingViolation(err) {
			return nil, fmt.Errorf("stat %s: %w", path, err)
		}
	}
	return nil, fmt.Errorf("File is locked by another process")
}

// CopyFile copies a single file atomically (§9.3).
func CopyFile(src, dst string) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("copy read %s: %w", src, err)
	}
	defer in.Close()
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".shim-copy-*")
	if err != nil {
		return "", fmt.Errorf("copy write %s: %w", dst, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return "", fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return "", fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	tmpName = ""
	return fmt.Sprintf("Copied %s to %s", src, dst), nil
}

// CopyDir recursively copies a directory tree (§5.5).
func CopyDir(src, dst string) (string, error) {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return "", fmt.Errorf("create %s: %w", dst, err)
	}
	count := 0
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		_, copyErr := CopyFile(p, target)
		if copyErr != nil {
			return copyErr
		}
		count++
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Copied %d files from %s to %s", count, src, dst), nil
}
