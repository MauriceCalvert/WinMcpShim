package tools

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/MauriceCalvert/WinMcpShim/shared"
)

// Grep searches file contents by regex pattern (built-in fallback for external grep).
func Grep(params map[string]interface{}, allowedRoots []string) (string, error) {
	path, err := shared.RequireString(params, "path")
	if err != nil {
		return "", err
	}
	pattern, err := shared.RequireString(params, "pattern")
	if err != nil {
		return "", err
	}
	recursive, _ := shared.OptionalBool(params, "recursive")
	ignoreCase, _ := shared.OptionalBool(params, "ignore_case")
	lineNumbers := true
	if ln, hasLN := shared.OptionalBool(params, "line_numbers"); hasLN {
		lineNumbers = ln
	}
	include, _ := shared.OptionalString(params, "include")
	contextLines, _ := shared.OptionalInt(params, "context")
	if contextLines < 0 {
		contextLines = 0
	}
	maxResults, hasMax := shared.OptionalInt(params, "max_results")
	if !hasMax || maxResults <= 0 {
		maxResults = 1000
	}

	if pattern == "" {
		return "", fmt.Errorf("empty pattern")
	}

	if ignoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex %q: %v", pattern, err)
	}

	info, err := shared.StatWithRetry(path)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	matchCount := 0

	if !info.IsDir() {
		// Single file
		if err := shared.CheckPathConfinementFull(path, allowedRoots); err != nil {
			return "", err
		}
		matchCount = grepFile(path, "", re, lineNumbers, contextLines, maxResults, &sb)
		if matchCount >= maxResults {
			fmt.Fprintf(&sb, "[results truncated at %d matches]\n", maxResults)
		}
		return sb.String(), nil
	}

	// Directory
	if err := shared.CheckPathConfinementFull(path, allowedRoots); err != nil {
		return "", err
	}
	if !recursive {
		// Non-recursive: only files directly in path
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", fmt.Errorf("read directory %s: %w", path, err)
		}
		for _, entry := range entries {
			if matchCount >= maxResults {
				break
			}
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if include != "" {
				matched, _ := filepath.Match(include, name)
				if !matched {
					continue
				}
			}
			fp := filepath.Join(path, name)
			// Skip symlinks/junctions that resolve outside roots
			if shared.CheckPathConfinementFull(fp, allowedRoots) != nil {
				continue
			}
			n := grepFile(fp, fp, re, lineNumbers, contextLines, maxResults-matchCount, &sb)
			matchCount += n
		}
		if matchCount >= maxResults {
			fmt.Fprintf(&sb, "[results truncated at %d matches]\n", maxResults)
		}
		return sb.String(), nil
	}

	// Recursive walk
	filepath.WalkDir(path, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Permission denied or other walk errors — skip silently
			return nil
		}
		if matchCount >= maxResults {
			return filepath.SkipAll
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if include != "" {
			matched, _ := filepath.Match(include, name)
			if !matched {
				return nil
			}
		}
		// Skip symlinks/junctions that resolve outside roots
		if shared.CheckPathConfinementFull(p, allowedRoots) != nil {
			return nil
		}
		n := grepFile(p, p, re, lineNumbers, contextLines, maxResults-matchCount, &sb)
		matchCount += n
		return nil
	})

	if matchCount >= maxResults {
		fmt.Fprintf(&sb, "[results truncated at %d matches]\n", maxResults)
	}
	return sb.String(), nil
}

// grepFile searches a single file for regex matches and appends formatted output to sb.
// prefix is the file path to include in output (empty for single-file mode).
// Returns the number of matching lines found.
func grepFile(path string, prefix string, re *regexp.Regexp, lineNumbers bool, contextLines int, maxResults int, sb *strings.Builder) int {
	f, err := shared.OpenFileWithRetry(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	// Read header for encoding detection
	header := make([]byte, shared.BinaryScanSize)
	n, _ := f.Read(header)
	header = header[:n]
	if n == 0 {
		return 0
	}

	enc, encErr := shared.DetectTextEncoding(header)
	if encErr != nil {
		// Binary file — skip silently
		return 0
	}

	var lines []string
	if enc == shared.UTF16LE || enc == shared.UTF16BE {
		// Read entire file, decode
		f.Seek(0, io.SeekStart)
		data, err := io.ReadAll(io.LimitReader(f, int64(shared.MaxReadSize)))
		if err != nil {
			return 0
		}
		decoded, err := shared.DecodeUTF16(data, enc)
		if err != nil {
			return 0
		}
		lines = strings.Split(decoded, "\n")
		for i := range lines {
			lines[i] = strings.TrimRight(lines[i], "\r")
		}
	} else {
		// UTF-8: scan line by line
		f.Seek(0, io.SeekStart)
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
	}

	if contextLines <= 0 {
		return grepNoContext(lines, prefix, re, lineNumbers, maxResults, sb)
	}
	return grepWithContext(lines, prefix, re, lineNumbers, contextLines, maxResults, sb)
}

// grepNoContext matches lines without context.
func grepNoContext(lines []string, prefix string, re *regexp.Regexp, lineNumbers bool, maxResults int, sb *strings.Builder) int {
	count := 0
	for i, line := range lines {
		if count >= maxResults {
			break
		}
		if re.MatchString(line) {
			formatLine(sb, prefix, i+1, ':', line, lineNumbers)
			count++
		}
	}
	return count
}

// grepWithContext matches lines with before/after context.
func grepWithContext(lines []string, prefix string, re *regexp.Regexp, lineNumbers bool, contextLines int, maxResults int, sb *strings.Builder) int {
	count := 0
	lastPrinted := -1 // index of the last line we printed

	for i, line := range lines {
		if count >= maxResults {
			break
		}
		if !re.MatchString(line) {
			continue
		}
		count++

		// Before-context
		start := i - contextLines
		if start < 0 {
			start = 0
		}
		if start <= lastPrinted {
			start = lastPrinted + 1
		}

		// Separator if there's a gap
		if lastPrinted >= 0 && start > lastPrinted+1 {
			sb.WriteString("--\n")
		}

		// Print before-context lines
		for j := start; j < i; j++ {
			formatLine(sb, prefix, j+1, '-', lines[j], lineNumbers)
		}

		// Print match line
		formatLine(sb, prefix, i+1, ':', line, lineNumbers)
		lastPrinted = i

		// After-context
		end := i + contextLines
		if end >= len(lines) {
			end = len(lines) - 1
		}
		for j := i + 1; j <= end; j++ {
			if count >= maxResults {
				break
			}
			if re.MatchString(lines[j]) {
				// This match will be handled in the outer loop
				break
			}
			formatLine(sb, prefix, j+1, '-', lines[j], lineNumbers)
			lastPrinted = j
		}
	}
	return count
}

// formatLine writes a single output line in grep format.
func formatLine(sb *strings.Builder, prefix string, lineNum int, sep byte, text string, lineNumbers bool) {
	if prefix != "" {
		sb.WriteString(prefix)
		if lineNumbers {
			fmt.Fprintf(sb, "%c%d%c%s\n", sep, lineNum, sep, text)
		} else {
			sb.WriteByte(sep)
			sb.WriteString(text)
			sb.WriteByte('\n')
		}
	} else {
		if lineNumbers {
			fmt.Fprintf(sb, "%d%c%s\n", lineNum, sep, text)
		} else {
			sb.WriteString(text)
			sb.WriteByte('\n')
		}
	}
}

// BuiltinGrepSchema returns the MCP tool schema for the built-in grep.
func BuiltinGrepSchema(description string) shared.ToolSchema {
	return shared.ToolSchema{
		Name:        "grep",
		Description: description,
		InputSchema: shared.RawJSON(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute path to file or directory to search"},
				"pattern": {"type": "string", "description": "Regular expression pattern (RE2 syntax)"},
				"recursive": {"type": "boolean", "description": "Search directories recursively (default false)"},
				"ignore_case": {"type": "boolean", "description": "Case-insensitive matching (default false)"},
				"line_numbers": {"type": "boolean", "description": "Show line numbers (default true)"},
				"include": {"type": "string", "description": "Glob pattern to filter filenames (e.g. *.py)"},
				"context": {"type": "integer", "description": "Lines of context around each match"},
				"max_results": {"type": "integer", "description": "Maximum matching lines to return (default 1000)"}
			},
			"required": ["path", "pattern"]
		}`),
		Annotations: &shared.ToolAnnotations{
			Title:           "Grep (Content Search)",
			ReadOnlyHint:    true,
			DestructiveHint: false,
			IdempotentHint:  true,
			OpenWorldHint:   false,
		},
	}
}
