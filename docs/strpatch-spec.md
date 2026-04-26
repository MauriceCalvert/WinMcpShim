# strpatch.exe Specification

Version: 1.1.0
Date: 2026-04-02
Status: Release

This document is the authoritative specification for strpatch.exe.
When the implementation diverges from this document, the spec is
correct and the code is a bug.

---

## 1. Purpose

strpatch.exe finds a unique string in a text file and replaces it
with another string, preserving the file's original line-ending
convention byte-for-byte. It is a standalone executable used by
the WinMcpShim for the `edit` tool, and can also be used directly
from the command line.

Written in Go. Source: `strpatch/main.go`.

## 2. Why a Separate Tool

Existing tools (sed, perl, PowerShell -replace) all process files
line-by-line, which means they have opinions about line endings
and silently mangle them. Claude Code's own Edit tool has this
exact bug — it normalises to LF internally, then fails to match
CRLF file content.

strpatch.exe operates on raw bytes. It never interprets line
endings during its core read/match/write cycle. The only
line-ending awareness is a targeted normalisation step that
ensures Claude's LF-only search strings can match files using
CRLF.

## 3. Algorithm

### 3.1 Read

Read the entire file into a `[]byte` using `os.ReadFile`. This
is a byte slice, not a string. No text-mode conversion occurs.

### 3.2 Detect line-ending convention

Scan the buffer for the first occurrence of `\r\n` (bytes 0x0D
0x0A) using `bytes.Contains`. If found, the file uses CRLF.
Otherwise LF.

Files with mixed line endings are treated as CRLF if any `\r\n`
is present.

### 3.3 Normalise search and replace strings

If the file uses CRLF:

- Replace every bare `\n` in the search string with `\r\n`
  (only where `\n` is not already preceded by `\r`).
- Same for the replacement string.

This handles the common case: Claude sends LF-only text because
JSON transport strips `\r`, but the file on disk has `\r\n`.

If the file uses LF, no normalisation is needed.

### 3.4 Match

Search the buffer for the (normalised) search string using
`bytes.Index`. Exact byte match.

- Not found (`bytes.Index` returns -1): exit with error.
- Found more than once: exit with error including count.
- Found exactly once: proceed.

### 3.5 Splice

Construct the output:

- `buffer[:matchStart]`
- normalised replacement
- `buffer[matchStart+len(normalisedSearch):]`

The BOM, if present, is part of the prefix and is preserved
automatically. No BOM detection or handling needed.

### 3.6 Write

Write the output to a temporary file in the same directory
using `os.CreateTemp`, then `os.Rename` over the original.
This is atomic on NTFS.

If any step fails, remove the temp file (if it exists) using
`os.Remove` and exit with an error. The original file is never
modified in place.

### 3.7 File locking

Open the original file with `os.ReadFile`. On Windows, Go's
file operations respect sharing mode defaults. If another
process has the file exclusively locked, the read fails.

Retry 3 times with 50 ms, 200 ms, 500 ms backoff on sharing
violation errors. If still locked, exit with error.

## 4. Refusals

strpatch.exe detects and refuses the following cases. It never
attempts silent conversion or fuzzy matching.

| Condition              | Detection                   | Exit code | Error message |
|------------------------|-----------------------------|-----------|---------------|
| File not found         | `os.ErrNotExist`            | 3 | `File not found: <path>` |
| File is binary         | Null byte in first 8 KB     | 3 | `File appears to be binary, not text` |
| File is UTF-16         | BOM FF FE or FE FF          | 3 | `File is UTF-16; only UTF-8/ASCII supported` |
| File too large         | Size > 10 MB                | 3 | `File exceeds 10 MB size limit` |
| Search text not found  | `bytes.Index` returns -1    | 1 | `Search text not found` |
| Search text not unique | Second `bytes.Index` >= 0   | 2 | `Search text not unique (found N times)` |
| Search text is empty   | Zero-length field            | 5 | `Search text must not be empty` |
| File is locked         | Sharing violation after 3 retries | 3 | `File is locked by another process` |
| Write failed           | Temp file or rename fails   | 4 | `Write failed: <error>` |
| File is read-only      | Permission check             | 3 | `File is read-only` |
| Invalid JSON on stdin  | `json.Unmarshal` fails       | 5 | `Invalid JSON input on stdin` |
| Missing required field | Field not present in JSON   | 5 | `Missing required field: <name>` |
| Stdin empty / EOF      | `io.ReadAll` returns empty   | 5 | `No input received on stdin` |

## 5. Input interface

### 5.1 The escaping problem

The search and replacement strings can contain newlines, quotes,
backslashes, tabs — any character. Windows command-line argument
passing cannot represent a literal newline in an argument string.
No escaping scheme handles this reliably across all cases.

### 5.2 Solution: JSON on stdin

strpatch.exe reads a single JSON object from stdin:

```json
{
    "path": "D:\\projects\\example\\main.py",
    "old_text": "def foo():\n    return 1",
    "new_text": "def foo():\n    return 2"
}
```

All special characters are handled by JSON's own escaping. The
shim already has the parameters as JSON fields from the
`tools/call` request — it writes them directly to strpatch's
stdin pipe with no further transformation.

### 5.3 JSON parsing

strpatch.exe reads stdin until EOF (the shim closes the write
end of the pipe after sending the JSON). It parses the JSON
using Go's `encoding/json` into a struct:

```go
type PatchRequest struct {
    Path    string `json:"path"`
    OldText string `json:"old_text"`
    NewText string `json:"new_text"`
}
```

### 5.4 Command-line usage

For manual testing from a terminal:

```
echo {"path":"test.txt","old_text":"foo","new_text":"bar"} | strpatch.exe
```

strpatch.exe takes no command-line arguments. Everything comes
from stdin.

### 5.5 Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Search text not found |
| 2 | Search text not unique |
| 3 | File error (not found, locked, binary, UTF-16, too large, read-only) |
| 4 | Write error (temp file creation, rename failure) |
| 5 | Input error (invalid JSON, missing field, empty stdin) |

### 5.6 Output

On success: writes to stdout:
`Replaced 1 occurrence in <path> (<original> → <new> bytes)`

On failure: writes the error message to stderr and nothing
to stdout.

## 6. Shim integration

The shim spawns strpatch.exe via `exec.Command` with no
command-line arguments. It creates a pipe to strpatch's stdin
(`cmd.StdinPipe`) and writes a JSON object containing `path`,
`old_text`, and `new_text` extracted directly from the
`tools/call` parameters. It then closes the write end of the
pipe (signalling EOF) and reads stdout/stderr.

No escaping is needed at any stage:

1. Claude sends JSON-RPC with the parameters as JSON strings.
2. The shim extracts the raw JSON fields.
3. The shim writes them as a JSON object to strpatch's stdin.
4. strpatch decodes the JSON strings into Go strings (`[]byte`).
5. strpatch matches and replaces on the raw file bytes.

The shim treats exit code 0 as success. All other exit codes
produce a JSON-RPC error response containing the stderr output.

## 7. Edge cases

| § | Case | Behaviour |
|---|------|-----------|
| 7.1 | Replacement contains the search string | Allowed. Match is performed once on the original buffer. No re-scanning |
| 7.2 | Replacement is empty | Allowed. Effectively deletes the matched text |
| 7.3 | Search string spans a CRLF boundary | After normalisation, search contains `\r\n` where file does. Works correctly |
| 7.4 | Very long search/replace strings | Limited by memory. 10 MB file size limit is the practical constraint |
| 7.5 | File ends without final newline | No special handling. Raw bytes — matches or doesn't |
| 7.6 | File has UTF-8 BOM | BOM (EF BB BF) is part of the buffer, preserved through splice |
| 7.7 | Strings containing tabs | Tabs (0x09) are just bytes. JSON `\t` decoded automatically |
| 7.8 | Strings containing backslashes | JSON `\\` decoded to single 0x5C. No double-escaping because stdin is JSON, not CLI |
| 7.9 | Strings containing quotes | JSON `\"` decoded to 0x22. No CLI quoting issues |

## 8. What strpatch.exe does not do

- Regex matching.
- Fuzzy or whitespace-normalised matching.
- Multi-file operations.
- Recursive directory traversal.
- UTF-16 transcoding.
- Binary file patching.
- Tab/space conversion.
- Command-line argument parsing.
- Any operation other than exact find-and-replace on a single
  text file.

## 9. Build

```
cd WinMcpShim/strpatch
go build -o strpatch.exe .
```

No external dependencies — uses only Go standard library
(`encoding/json`, `bytes`, `os`, `fmt`, `io`). Produces a
statically linked executable with no runtime dependencies.

## 10. Function inventory

All functions are in `strpatch/main.go` unless noted.

| Function | Signature | Pure | §Req |
|----------|-----------|------|------|
| `main` | `()` | No | — |
| `run` | `() int` | No | — |
| `readInput` | `() (*PatchRequest, error)` | No | 5.2, 5.3 |
| `patch` | `(req *PatchRequest) (int, error)` | No | 3.1–3.6 |
| `checkRefusals` | `(buf []byte, path string) error` | No | 4.1–4.5, 4.11 |
| `normaliseToCRLF` | `(data []byte) []byte` | Yes | 3.3 |
| `countOccurrences` | `(buf, search []byte) int` | Yes | 3.4 |
| `atomicWrite` | `(path string, data []byte) error` | No | 3.6 |
| `readFileWithRetry` | `(path string) ([]byte, error)` | No | 3.7 |
| `isSharingViolation` | `(err error) bool` | Yes | 3.7 |

Platform-specific: `isSharingViolation` is in
`sharing_windows.go` (checks `syscall.Errno == 32`) and
`sharing_other.go` (returns false).

## 11. Requirements traceability matrix

Previously maintained in `RTM.md` (Part 2). Merged here for
single-source traceability.

### Status key

| Code    | Meaning |
|---------|---------|
| OK      | Implemented and tested |
| ACCEPT  | Known limitation, accepted by spec |

### §3 Algorithm

| Req | Description | Test | Status | Notes |
|-----|-------------|------|--------|-------|
| 3.1 | Read entire file as []byte | — | OK | By construction |
| 3.2 | Detect CRLF via bytes.Contains | `TestHappyPath_CRLF_LFSearch` | OK | |
| 3.3.1 | Normalise bare \n to \r\n in search if CRLF | `TestHappyPath_CRLF_LFSearch` | OK | |
| 3.3.2 | Normalise bare \n to \r\n in replace if CRLF | `TestHappyPath_CRLF_LFSearch` | OK | |
| 3.3.3 | Don't double-convert existing \r\n | `TestHappyPath_CRLF_CRLFSearch` | OK | |
| 3.4.1 | Exact byte match via bytes.Index | `TestHappyPath_LF` | OK | |
| 3.4.2 | Not found: exit 1 | `TestRefusal_NotFound` | OK | |
| 3.4.3 | Not unique: exit 2 with count | `TestRefusal_NotUnique` | OK | |
| 3.5 | Splice: prefix + replacement + suffix | `TestHappyPath_LF` | OK | |
| 3.6 | Atomic write via temp + rename | — | ACCEPT | Crash-resilience untestable in CI |
| 3.7 | File lock retry 3x with backoff | `TestRefusal_FileLocked` | OK | |

### §4 Refusals

| Req | Description | Test | Status | Notes |
|-----|-------------|------|--------|-------|
| 4.1 | File not found | `TestRefusal_FileNotFound` | OK | |
| 4.2 | Binary file | `TestRefusal_BinaryFile` | OK | |
| 4.3 | UTF-16 LE (BOM FF FE) | `TestRefusal_UTF16` | OK | |
| 4.4 | UTF-16 BE (BOM FE FF) | `TestRefusal_UTF16BE` | OK | |
| 4.5 | File > 10 MB | `TestRefusal_TooLarge` | OK | |
| 4.6 | Search not found | `TestRefusal_NotFound` | OK | |
| 4.7 | Search not unique | `TestRefusal_NotUnique` | OK | |
| 4.8 | Empty search text | `TestRefusal_EmptySearch` | OK | |
| 4.9 | File locked | `TestRefusal_FileLocked` | OK | |
| 4.10 | Write failed | — | ACCEPT | Requires OS-level ACL manipulation |
| 4.11 | Read-only file | `TestRefusal_ReadOnly` | OK | |
| 4.12 | Invalid JSON | `TestRefusal_InvalidJSON` | OK | |
| 4.13 | Missing required field | `TestRefusal_MissingField` | OK | |
| 4.14 | Empty stdin | `TestRefusal_EmptyStdin` | OK | |
| 4.15 | Truncated JSON | `TestRefusal_TruncatedJSON` | OK | |

### §5 Input interface

| Req | Description | Test | Status | Notes |
|-----|-------------|------|--------|-------|
| 5.2 | Read JSON from stdin | all strpatch tests | OK | |
| 5.3 | Parse into PatchRequest struct | — | OK | By construction |
| 5.5.1 | Exit 0 on success | `TestHappyPath_LF` | OK | |
| 5.5.2 | Exit 1 not found | `TestRefusal_NotFound` | OK | |
| 5.5.3 | Exit 2 not unique | `TestRefusal_NotUnique` | OK | |
| 5.5.4 | Exit 3 file error | `TestRefusal_BinaryFile` etc | OK | |
| 5.5.5 | Exit 4 write error | — | ACCEPT | Same as 4.10 |
| 5.5.6 | Exit 5 input error | `TestRefusal_InvalidJSON` etc | OK | |
| 5.6.1 | Success output to stdout | `TestHappyPath_LF` | OK | |
| 5.6.2 | Error output to stderr | `TestRefusal_NotFound` | OK | |

### §7 Edge cases

| Req | Description | Test | Status | Notes |
|-----|-------------|------|--------|-------|
| 7.1 | Replacement contains search string | `TestHappyPath_ReplacementContainsSearch` | OK | |
| 7.2 | Empty replacement (deletion) | `TestHappyPath_EmptyReplacement` | OK | |
| 7.3 | Search spans CRLF boundary | `TestHappyPath_CRLF_LFSearch` | OK | |
| 7.4 | Large search/replace strings | `TestHappyPath_LargeFile` | OK | |
| 7.5 | File without final newline | `TestHappyPath_NoFinalNewline` | OK | |
| 7.6 | UTF-8 BOM preserved | `TestHappyPath_UTF8BOM` | OK | |
| 7.7 | Tabs | `TestHappyPath_Tabs` | OK | |
| 7.8 | Backslashes | `TestHappyPath_Backslashes` | OK | |
| 7.9 | Quotes | `TestHappyPath_Quotes` | OK | |
| 7.10 | Unicode (multi-byte UTF-8) | `TestHappyPath_Unicode` | OK | |

### Atomicity

| Req | Description | Test | Status | Notes |
|-----|-------------|------|--------|-------|
| A.1 | Original intact on not-found | `TestAtomicity_OriginalIntactOnNotFound` | OK | |
| A.2 | Original intact on not-unique | `TestAtomicity_OriginalIntactOnNotUnique` | OK | |
| A.3 | Kill mid-write: original intact | — | ACCEPT | Crash-atomicity unreliable in CI |
| A.4 | Kill after temp, before rename | — | ACCEPT | Sub-millisecond window |

### Known limitations

| § | Limitation | Severity | Rationale |
|---|-----------|----------|-----------|
| 3.6 | Crash-resilience untested | Low | temp+rename is correct; kill-timing unreliable in CI |
| 4.10 | Write-failed path untested | Low | Requires OS-level ACL manipulation beyond test scope |
| 5.5.5 | Exit code 4 untested | Low | Same as 4.10 |

## 12. Summary

| Status | Count |
|--------|-------|
| OK | 45 |
| ACCEPT | 5 |
| **Total** | **50** |

Every requirement is either tested (OK) or has a documented
rationale for acceptance (ACCEPT). No CODE, no MISS.
