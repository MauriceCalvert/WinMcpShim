// Package tools implements all tool handlers for the shim.
package tools

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/MauriceCalvert/WinMcpShim/shared"
)

// Read reads a text file with optional offset/limit (§5.2).
func Read(params map[string]interface{}, allowedRoots []string) (string, error) {
	path, err := shared.RequireString(params, "path")
	if err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(path, allowedRoots); err != nil {
		return "", err
	}
	info, err := shared.StatWithRetry(path)
	if err != nil {
		return "", err
	}
	offset, hasOffset := shared.OptionalInt(params, "offset")
	limit, hasLimit := shared.OptionalInt(params, "limit")
	hasRange := (hasOffset && offset > 0) || (hasLimit && limit > 0)
	fileSize := info.Size()
	if !hasRange && fileSize > shared.MaxReadSize {
		return "", fmt.Errorf("File is %d bytes (limit %d). Use offset/limit to read in parts.", fileSize, shared.MaxReadSize)
	}
	f, err := shared.OpenFileWithRetry(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	headerSize := shared.BinaryScanSize
	if int64(headerSize) > fileSize {
		headerSize = int(fileSize)
	}
	header := make([]byte, headerSize)
	if headerSize > 0 {
		n, readErr := f.ReadAt(header, 0)
		header = header[:n]
		if readErr != nil && readErr != io.EOF {
			return "", fmt.Errorf("read header %s: %w", path, readErr)
		}
	}
	enc, encErr := shared.DetectTextEncoding(header)
	if encErr != nil {
		return "", encErr
	}
	if enc == shared.UTF16LE || enc == shared.UTF16BE {
		return readUTF16(f, path, fileSize, enc, offset, limit, hasRange, hasLimit)
	}
	bomOffset := 0
	if len(header) >= 3 && header[0] == 0xEF && header[1] == 0xBB && header[2] == 0xBF {
		bomOffset = 3
	}
	if hasRange {
		return readRange(f, path, fileSize, offset+bomOffset, limit, hasLimit)
	}
	data := make([]byte, fileSize-int64(bomOffset))
	n, readErr := f.ReadAt(data, int64(bomOffset))
	if readErr != nil && readErr != io.EOF {
		return "", fmt.Errorf("read %s: %w", path, readErr)
	}
	return string(data[:n]), nil
}

// readRange reads a byte range from a UTF-8 file.
func readRange(f *os.File, path string, fileSize int64, offset int, limit int, hasLimit bool) (string, error) {
	readOffset := int64(offset)
	readSize := fileSize - readOffset
	if hasLimit && limit > 0 && int64(limit) < readSize {
		readSize = int64(limit)
	}
	if readSize <= 0 {
		return "", nil
	}
	buf := make([]byte, readSize)
	n, readErr := f.ReadAt(buf, readOffset)
	if readErr != nil && readErr != io.EOF {
		return "", fmt.Errorf("read %s: %w", path, readErr)
	}
	return string(buf[:n]), nil
}

// readUTF16 reads a UTF-16 file with optional offset/limit range.
func readUTF16(f *os.File, path string, fileSize int64, enc shared.TextEncoding, offset int, limit int, hasRange bool, hasLimit bool) (string, error) {
	if hasRange {
		if offset%2 != 0 {
			return "", fmt.Errorf("offset must be even for UTF-16 files")
		}
		readOffset := int64(2 + offset)
		readSize := fileSize - readOffset
		if hasLimit && limit > 0 && int64(limit) < readSize {
			readSize = int64(limit)
		}
		if readSize <= 0 {
			return "", nil
		}
		if readSize%2 != 0 {
			readSize--
		}
		buf := make([]byte, readSize)
		rn, readErr := f.ReadAt(buf, readOffset)
		if readErr != nil && readErr != io.EOF {
			return "", fmt.Errorf("read %s: %w", path, readErr)
		}
		buf = buf[:rn]
		if len(buf)%2 != 0 {
			buf = buf[:len(buf)-1]
		}
		var bom []byte
		if enc == shared.UTF16LE {
			bom = []byte{0xFF, 0xFE}
		} else {
			bom = []byte{0xFE, 0xFF}
		}
		return shared.DecodeUTF16(append(bom, buf...), enc)
	}
	if fileSize > int64(shared.MaxReadSize) {
		return "", fmt.Errorf("File is %d bytes (limit %d). Use offset/limit to read in parts.", fileSize, shared.MaxReadSize)
	}
	allData := make([]byte, fileSize)
	rn, readErr := f.ReadAt(allData, 0)
	if readErr != nil && readErr != io.EOF {
		return "", fmt.Errorf("read %s: %w", path, readErr)
	}
	return shared.DecodeUTF16(allData[:rn], enc)
}

// Write creates or overwrites a text file (§5.3).
func Write(params map[string]interface{}, allowedRoots []string) (string, error) {
	path, err := shared.RequireString(params, "path")
	if err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(path, allowedRoots); err != nil {
		return "", err
	}
	content, err := shared.RequireString(params, "content")
	if err != nil {
		return "", err
	}
	appendMode, _ := shared.OptionalBool(params, "append")
	existing, existErr := shared.ReadFileWithRetry(path)
	isCRLF := false
	if existErr == nil && len(existing) > 0 {
		scanLen := shared.CRLFScanSize
		if len(existing) < scanLen {
			scanLen = len(existing)
		}
		isCRLF = bytes.Contains(existing[:scanLen], []byte("\r\n"))
	} else if existErr != nil && !shared.IsNotExist(existErr) {
		return "", fmt.Errorf("read %s: %w", path, existErr)
	}
	if appendMode && existErr == nil {
		content = string(existing) + content
	}
	output := []byte(content)
	if isCRLF {
		output = shared.NormaliseToCRLF(output)
	}
	if err := shared.AtomicWrite(path, output); err != nil {
		return "", err
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(output), path), nil
}

// Copy copies a file or directory tree (§5.5).
func Copy(params map[string]interface{}, allowedRoots []string) (string, error) {
	source, err := shared.RequireString(params, "source")
	if err != nil {
		return "", err
	}
	destination, err := shared.RequireString(params, "destination")
	if err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(source, allowedRoots); err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(destination, allowedRoots); err != nil {
		return "", err
	}
	if _, err := os.Stat(destination); err == nil {
		return "", fmt.Errorf("Destination already exists: %s", destination)
	}
	recursive, _ := shared.OptionalBool(params, "recursive")
	srcInfo, err := os.Stat(source)
	if err != nil {
		return "", fmt.Errorf("source not found: %s", source)
	}
	if srcInfo.IsDir() {
		if !recursive {
			return "", fmt.Errorf("source is a directory; use recursive=true to copy directory trees")
		}
		return shared.CopyDir(source, destination)
	}
	return shared.CopyFile(source, destination)
}

// Move moves or renames a file or directory (§5.6).
func Move(params map[string]interface{}, allowedRoots []string) (string, error) {
	source, err := shared.RequireString(params, "source")
	if err != nil {
		return "", err
	}
	destination, err := shared.RequireString(params, "destination")
	if err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(source, allowedRoots); err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(destination, allowedRoots); err != nil {
		return "", err
	}
	if _, err := os.Stat(destination); err == nil {
		return "", fmt.Errorf("Destination already exists: %s", destination)
	}
	if err := os.Rename(source, destination); err != nil {
		return "", fmt.Errorf("move %s to %s: %w", source, destination, err)
	}
	return fmt.Sprintf("Moved %s to %s", source, destination), nil
}

// Delete deletes a file or empty directory (§5.7).
func Delete(params map[string]interface{}, allowedRoots []string) (string, error) {
	path, err := shared.RequireString(params, "path")
	if err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(path, allowedRoots); err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		if strings.Contains(err.Error(), "directory is not empty") || strings.Contains(err.Error(), "The directory is not empty") {
			return "", fmt.Errorf("Directory is not empty: %s", path)
		}
		return "", fmt.Errorf("delete %s: %w", path, err)
	}
	return fmt.Sprintf("Deleted %s", path), nil
}

// List lists directory contents with optional pattern filter (§5.8).
func List(params map[string]interface{}, allowedRoots []string) (string, error) {
	path, err := shared.RequireString(params, "path")
	if err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(path, allowedRoots); err != nil {
		return "", err
	}
	pattern, _ := shared.OptionalString(params, "pattern")
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("list %s: %w", path, err)
	}
	var sb strings.Builder
	for _, entry := range entries {
		name := entry.Name()
		if pattern != "" {
			matched, _ := filepath.Match(pattern, name)
			if !matched {
				continue
			}
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		kind := "file"
		if entry.IsDir() {
			kind = "dir"
		}
		fmt.Fprintf(&sb, "%s\t%s\t%d\n", name, kind, info.Size())
	}
	return sb.String(), nil
}

// Search recursively searches for files matching a glob pattern (§5.9).
func Search(params map[string]interface{}, allowedRoots []string) (string, error) {
	path, err := shared.RequireString(params, "path")
	if err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(path, allowedRoots); err != nil {
		return "", err
	}
	pattern, err := shared.RequireString(params, "pattern")
	if err != nil {
		return "", err
	}
	maxResults, _ := shared.OptionalInt(params, "max_results")
	if maxResults <= 0 {
		maxResults = shared.DefaultMaxResults
	}
	var results []string
	filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if len(results) >= maxResults {
			return filepath.SkipAll
		}
		matched, _ := filepath.Match(pattern, d.Name())
		if matched {
			results = append(results, p)
		}
		return nil
	})
	return strings.Join(results, "\n"), nil
}

// Info returns file or directory metadata (§5.10).
func Info(params map[string]interface{}, allowedRoots []string) (string, error) {
	path, err := shared.RequireString(params, "path")
	if err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(path, allowedRoots); err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("info %s: %w", path, err)
	}
	kind := "file"
	if info.IsDir() {
		kind = "directory"
	}
	readOnly := info.Mode().Perm()&0200 == 0
	created := shared.FileCreationTime(info)
	return fmt.Sprintf("type: %s\nsize: %d\ncreated: %s\nmodified: %s\nread_only: %v",
		kind, info.Size(), created, info.ModTime().Format(time.RFC3339), readOnly), nil
}

// Cat reads and concatenates multiple files (§5.12).
func Cat(params map[string]interface{}, allowedRoots []string) (string, error) {
	pathsRaw, err := shared.RequireString(params, "paths")
	if err != nil {
		return "", err
	}
	var paths []string
	if strings.HasPrefix(strings.TrimSpace(pathsRaw), "[") {
		if err := json.Unmarshal([]byte(pathsRaw), &paths); err != nil {
			return "", fmt.Errorf("paths must be space-separated absolute paths or a JSON array of strings")
		}
	} else {
		paths = splitPaths(pathsRaw)
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no paths provided")
	}
	var sb strings.Builder
	for i, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if err := shared.CheckPathConfinementFull(p, allowedRoots); err != nil {
			return "", err
		}
		data, err := shared.ReadFileWithRetry(p)
		if err != nil {
			return "", fmt.Errorf("cat %s: %w", p, err)
		}
		enc, encErr := shared.DetectTextEncoding(data)
		if encErr != nil {
			return "", fmt.Errorf("%s: %w", p, encErr)
		}
		if i > 0 {
			sb.WriteString("\n")
		}
		if enc == shared.UTF16LE || enc == shared.UTF16BE {
			decoded, err := shared.DecodeUTF16(data, enc)
			if err != nil {
				return "", fmt.Errorf("%s: %w", p, err)
			}
			sb.WriteString(decoded)
		} else {
			sb.Write(data)
		}
		if sb.Len() > shared.MaxReadSize {
			return "", fmt.Errorf("combined output exceeds %d bytes", shared.MaxReadSize)
		}
	}
	return sb.String(), nil
}

// splitPaths splits a space-separated path string, respecting double quotes.
func splitPaths(s string) []string {
	var paths []string
	var current strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			inQuote = !inQuote
		} else if (c == ' ' || c == '\t') && !inQuote {
			if current.Len() > 0 {
				paths = append(paths, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		paths = append(paths, current.String())
	}
	return paths
}

// Diff computes a unified diff between two text files (§5.16).
func Diff(params map[string]interface{}, allowedRoots []string) (string, error) {
	file1, err := shared.RequireString(params, "file1")
	if err != nil {
		return "", err
	}
	file2, err := shared.RequireString(params, "file2")
	if err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(file1, allowedRoots); err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(file2, allowedRoots); err != nil {
		return "", err
	}
	contextLines, hasCtx := shared.OptionalInt(params, "context_lines")
	if !hasCtx || contextLines < 0 {
		contextLines = 3
	}
	data1, err := shared.ReadFileWithRetry(file1)
	if err != nil {
		return "", err
	}
	text1, err := decodeForDiff(data1, file1)
	if err != nil {
		return "", err
	}
	data2, err := shared.ReadFileWithRetry(file2)
	if err != nil {
		return "", err
	}
	text2, err := decodeForDiff(data2, file2)
	if err != nil {
		return "", err
	}
	lines1 := strings.Split(text1, "\n")
	lines2 := strings.Split(text2, "\n")
	return unifiedDiff(file1, file2, lines1, lines2, contextLines), nil
}

// decodeForDiff detects encoding and decodes file data for diffing.
func decodeForDiff(data []byte, name string) (string, error) {
	enc, err := shared.DetectTextEncoding(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	if enc == shared.UTF16LE || enc == shared.UTF16BE {
		decoded, err := shared.DecodeUTF16(data, enc)
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		return decoded, nil
	}
	return string(data), nil
}

// unifiedDiff produces a unified diff between two slices of lines.
func unifiedDiff(name1, name2 string, a, b []string, contextLines int) string {
	// Simple LCS-based diff
	m, n := len(a), len(b)
	// Build edit script using Myers-like approach (simple O(mn) DP)
	// dp[i][j] = LCS length of a[:i] and b[:j]
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	// Backtrack to get edit operations
	type editOp struct {
		op   byte // ' ' = context, '-' = delete from a, '+' = insert from b
		line string
		aIdx int // 1-based index in a (-1 if insert)
		bIdx int // 1-based index in b (-1 if delete)
	}
	var ops []editOp
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			ops = append(ops, editOp{' ', a[i-1], i, j})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			ops = append(ops, editOp{'+', b[j-1], -1, j})
			j--
		} else {
			ops = append(ops, editOp{'-', a[i-1], i, -1})
			i--
		}
	}
	// Reverse ops (built in reverse order)
	for left, right := 0, len(ops)-1; left < right; left, right = left+1, right-1 {
		ops[left], ops[right] = ops[right], ops[left]
	}
	// Check if files are identical
	allContext := true
	for _, op := range ops {
		if op.op != ' ' {
			allContext = false
			break
		}
	}
	if allContext {
		return ""
	}
	// Group into hunks with context
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n", name1)
	fmt.Fprintf(&sb, "+++ %s\n", name2)
	// Find change regions and expand with context
	type hunkRange struct{ start, end int }
	var changes []hunkRange
	for idx, op := range ops {
		if op.op != ' ' {
			if len(changes) > 0 && idx-changes[len(changes)-1].end <= 2*contextLines {
				changes[len(changes)-1].end = idx
			} else {
				changes = append(changes, hunkRange{idx, idx})
			}
		}
	}
	for _, ch := range changes {
		start := ch.start - contextLines
		if start < 0 {
			start = 0
		}
		end := ch.end + contextLines + 1
		if end > len(ops) {
			end = len(ops)
		}
		// Compute line ranges for the hunk header
		aStart, aCount := 0, 0
		bStart, bCount := 0, 0
		for k := start; k < end; k++ {
			op := ops[k]
			if op.op == ' ' || op.op == '-' {
				if aStart == 0 {
					aStart = op.aIdx
				}
				aCount++
			}
			if op.op == ' ' || op.op == '+' {
				if bStart == 0 {
					bStart = op.bIdx
				}
				bCount++
			}
		}
		if aStart == 0 {
			aStart = 1
		}
		if bStart == 0 {
			bStart = 1
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", aStart, aCount, bStart, bCount)
		for k := start; k < end; k++ {
			op := ops[k]
			fmt.Fprintf(&sb, "%c%s\n", op.op, op.line)
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// Head returns the first N lines of a text file (§5.14).
func Head(params map[string]interface{}, allowedRoots []string) (string, error) {
	path, err := shared.RequireString(params, "path")
	if err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(path, allowedRoots); err != nil {
		return "", err
	}
	n, hasN := shared.OptionalInt(params, "lines")
	if !hasN || n <= 0 {
		n = 10
	}
	f, err := shared.OpenFileWithRetry(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	// Read header for encoding detection
	header := make([]byte, shared.BinaryScanSize)
	headerN, _ := f.Read(header)
	header = header[:headerN]
	enc, encErr := shared.DetectTextEncoding(header)
	if encErr != nil {
		return "", encErr
	}
	if enc == shared.UTF16LE || enc == shared.UTF16BE {
		// Cannot use bufio.Scanner on UTF-16; read entire file, decode, split.
		f.Seek(0, io.SeekStart)
		data, err := io.ReadAll(io.LimitReader(f, int64(shared.MaxReadSize)))
		if err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		decoded, err := shared.DecodeUTF16(data, enc)
		if err != nil {
			return "", err
		}
		lines := strings.Split(decoded, "\n")
		// Trim \r from CRLF
		for i := range lines {
			lines[i] = strings.TrimRight(lines[i], "\r")
		}
		if len(lines) > n {
			lines = lines[:n]
		}
		return strings.Join(lines, "\n"), nil
	}
	// UTF-8 path
	f.Seek(0, io.SeekStart)
	scanner := bufio.NewScanner(f)
	var lines []string
	for scanner.Scan() && len(lines) < n {
		lines = append(lines, scanner.Text())
	}
	return strings.Join(lines, "\n"), nil
}

// Mkdir creates a directory and any missing parents (§5.13).
func Mkdir(params map[string]interface{}, allowedRoots []string) (string, error) {
	path, err := shared.RequireString(params, "path")
	if err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(path, allowedRoots); err != nil {
		return "", err
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", path, err)
	}
	return fmt.Sprintf("Created %s", path), nil
}

// Roots returns the allowed_roots from config (§5.19).
func Roots(cfg *shared.Config) (string, error) {
	if len(cfg.Security.AllowedRoots) == 0 {
		return "(no allowed roots configured)", nil
	}
	return strings.Join(cfg.Security.AllowedRoots, "\n"), nil
}

// Tail returns the last N lines of a text file (§5.15).
func Tail(params map[string]interface{}, allowedRoots []string) (string, error) {
	path, err := shared.RequireString(params, "path")
	if err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(path, allowedRoots); err != nil {
		return "", err
	}
	n, hasN := shared.OptionalInt(params, "lines")
	if !hasN || n <= 0 {
		n = 10
	}
	info, err := shared.StatWithRetry(path)
	if err != nil {
		return "", err
	}
	f, err := shared.OpenFileWithRetry(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fileSize := info.Size()
	headerBuf := make([]byte, shared.BinaryScanSize)
	if int64(shared.BinaryScanSize) > fileSize {
		headerBuf = make([]byte, fileSize)
	}
	headerN, _ := f.ReadAt(headerBuf, 0)
	headerBuf = headerBuf[:headerN]
	enc, encErr := shared.DetectTextEncoding(headerBuf)
	if encErr != nil {
		return "", encErr
	}
	if enc == shared.UTF16LE || enc == shared.UTF16BE {
		return tailUTF16(f, path, fileSize, enc, n)
	}
	if fileSize <= int64(shared.MaxReadSize) {
		data, err := io.ReadAll(f)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		if err := shared.CheckTextFile(data); err != nil {
			return "", err
		}
		return tailSmall(data, n), nil
	}
	return tailSeek(f, path, fileSize, n)
}

// tailSmall returns the last n lines from in-memory file content.
func tailSmall(content []byte, n int) string {
	lines := strings.Split(string(content), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// tailSeek returns the last n lines from a large file using seek-based reading.
func tailSeek(f *os.File, path string, fileSize int64, n int) (string, error) {
	chunkSize := int64(64 * 1024)
	offset := fileSize - chunkSize
	if offset < 0 {
		offset = 0
	}
	f.Seek(offset, io.SeekStart)
	data, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("read tail %s: %w", path, err)
	}
	if err := shared.CheckTextFile(data); err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	if len(lines) <= n {
		return strings.Join(lines, "\n"), nil
	}
	return strings.Join(lines[len(lines)-n:], "\n"), nil
}

// tailUTF16 returns the last n lines from a UTF-16 encoded file.
func tailUTF16(f *os.File, path string, fileSize int64, enc shared.TextEncoding, n int) (string, error) {
	if fileSize <= int64(shared.MaxReadSize) {
		f.Seek(0, io.SeekStart)
		data, err := io.ReadAll(f)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		decoded, err := shared.DecodeUTF16(data, enc)
		if err != nil {
			return "", err
		}
		return tailFromDecoded(decoded, n, false), nil
	}
	chunkSize := int64(64 * 1024)
	offset := fileSize - chunkSize
	if offset < 2 {
		offset = 0
	} else if offset%2 != 0 {
		offset--
	}
	f.Seek(offset, io.SeekStart)
	data, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("read tail %s: %w", path, err)
	}
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	if offset == 0 {
		decoded, err := shared.DecodeUTF16(data, enc)
		if err != nil {
			return "", err
		}
		return tailFromDecoded(decoded, n, false), nil
	}
	var bom []byte
	if enc == shared.UTF16LE {
		bom = []byte{0xFF, 0xFE}
	} else {
		bom = []byte{0xFE, 0xFF}
	}
	decoded, err := shared.DecodeUTF16(append(bom, data...), enc)
	if err != nil {
		return "", err
	}
	return tailFromDecoded(decoded, n, true), nil
}

// tailFromDecoded extracts the last n lines from decoded UTF-16 text.
func tailFromDecoded(decoded string, n int, dropFirst bool) string {
	lines := strings.Split(decoded, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if dropFirst && len(lines) > 0 {
		lines = lines[1:]
	}
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// Tree produces a recursive indented directory listing (§5.18).
func Tree(params map[string]interface{}, allowedRoots []string) (string, error) {
	path, err := shared.RequireString(params, "path")
	if err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(path, allowedRoots); err != nil {
		return "", err
	}
	maxDepth, hasDepth := shared.OptionalInt(params, "depth")
	if !hasDepth || maxDepth <= 0 {
		maxDepth = 3
	}
	pattern, _ := shared.OptionalString(params, "pattern")
	var sb strings.Builder
	count := 0
	maxEntries := 500
	basePath := filepath.Clean(path)
	filepath.WalkDir(basePath, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if count >= maxEntries {
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(basePath, p)
		if rel == "." {
			return nil
		}
		depth := strings.Count(rel, string(filepath.Separator)) + 1
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if pattern != "" && !d.IsDir() {
			matched, _ := filepath.Match(pattern, d.Name())
			if !matched {
				return nil
			}
		}
		indent := strings.Repeat("  ", depth-1)
		name := d.Name()
		if d.IsDir() {
			name += "/"
		}
		fmt.Fprintf(&sb, "%s%s\n", indent, name)
		count++
		return nil
	})
	return strings.TrimRight(sb.String(), "\n"), nil
}

// Wc counts lines, words, and bytes in a text file (§5.17).
func Wc(params map[string]interface{}, allowedRoots []string) (string, error) {
	path, err := shared.RequireString(params, "path")
	if err != nil {
		return "", err
	}
	if err := shared.CheckPathConfinementFull(path, allowedRoots); err != nil {
		return "", err
	}
	f, err := shared.OpenFileWithRetry(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	// Read header for encoding detection
	header := make([]byte, shared.BinaryScanSize)
	headerN, _ := f.Read(header)
	header = header[:headerN]
	enc, encErr := shared.DetectTextEncoding(header)
	if encErr != nil {
		return "", encErr
	}
	if enc == shared.UTF16LE || enc == shared.UTF16BE {
		// Read entire file, decode to UTF-8, count on decoded string.
		f.Seek(0, io.SeekStart)
		data, err := io.ReadAll(io.LimitReader(f, int64(shared.MaxReadSize)))
		if err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		decoded, err := shared.DecodeUTF16(data, enc)
		if err != nil {
			return "", err
		}
		lines := strings.Split(decoded, "\n")
		// Trim \r from CRLF
		for i := range lines {
			lines[i] = strings.TrimRight(lines[i], "\r")
		}
		lineCount := len(lines)
		if lineCount > 0 && lines[lineCount-1] == "" {
			lineCount--
		}
		wordCount := 0
		for _, line := range lines {
			words := strings.FieldsFunc(line, unicode.IsSpace)
			wordCount += len(words)
		}
		byteCount := len(decoded)
		return fmt.Sprintf("lines: %d\nwords: %d\nbytes: %d", lineCount, wordCount, byteCount), nil
	}
	// UTF-8 path
	f.Seek(0, io.SeekStart)
	var lineCount, wordCount int
	var byteCount int64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		lineCount++
		byteCount += int64(len(scanner.Bytes())) + 1 // +1 for newline
		words := strings.FieldsFunc(line, unicode.IsSpace)
		wordCount += len(words)
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return fmt.Sprintf("lines: %d\nwords: %d\nbytes: %d", lineCount, wordCount, byteCount), nil
}
