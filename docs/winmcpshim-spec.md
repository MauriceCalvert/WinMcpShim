# WinMcpShim Specification

Version: 1.0.0
Date: 2026-04-01
Status: Release

This document is the authoritative specification for WinMcpShim.
When the implementation diverges from this document, the spec is
correct and the code is a bug (see Appendix A.3).

---


## 1. Problem

The MCP stdio transport requires that a server process write only valid JSON-RPC messages to stdout. Any stray byte — from a subprocess leak, a library logging call, npm-style server startup banners, Node.js "Experimental Warning" notices, a Python runtime startup message, or a C extension — corrupts the stream and kills the connection.

Additionally, many common operations (file I/O, text search) are often served by multiple independent MCP servers, each with startup costs and overlapping functionality.

## 2. Solution

A single compiled Go program (the **shim**) replaces all MCP servers. 
It acts as a full MCP server from Claude Desktop's
perspective, implementing built-in file and folder operations and
delegating configured external tools (such as grep) to native executables.

Reliability: Developed using practices drawn from
[DO-178C](https://en.wikipedia.org/wiki/DO-178C) safety engineering:
specification-first design, a comprehensive
[requirements traceability matrix](#15-requirements-traceability-matrix), rigorous
path-confinement post-checks, and Job Object-based process isolation.

## 3. Architecture

```
  Claude Desktop
       │
  stdin │ stdout          The shim's fd 0 and fd 1 are
       │                  Claude Desktop's pipes.
  ┌────▼─────┐
  │          │
  │   SHIM   │──── stderr / log file ────► diagnostics
  │   (Go)   │
  │          │
  └──┬───┬───┘
     │   │
     │   ├──► Built-in tools (read/write/edit/copy/move/delete/list/search/info/run)
     │   │    Implemented in Go using os/filepath and os/exec packages.
     │   │
     │   └──► Configured external tools (grep, find, etc.)
     │        Spawned via os/exec, stdout/stderr captured.
     │
     ▼
  Native executables         All spawned via os/exec.Cmd.
  (grep.exe, python.exe,     Never via cmd.exe or any shell.
   strpatch.exe, etc.)       stdout/stderr captured by shim.
```

## 4. MCP Protocol Surface

The shim understands exactly four JSON-RPC methods:

- `initialize` — respond with server capabilities declaring `"tools": {}`
- `notifications/initialized` (also accepted as `initialized`) —
  notification, no response
- `tools/list` — respond with the tool registry (built-in + configured)
- `tools/call` — dispatch to the appropriate handler

The canonical MCP method name is `notifications/initialized`. The shim
accepts both forms for compatibility with clients that may use the
short form.

All other methods are rejected with JSON-RPC error -32601 (Method not
found).

### 4.1 Request ID Preservation

JSON-RPC allows `id` to be an integer, string, or null. The shim
preserves the `id` field using `json.RawMessage` so it is echoed back
in responses with its original type and value intact.

### 4.2 Tool Annotations

Every tool in the `tools/list` response includes an `annotations`
object per MCP spec `ToolAnnotations`. This is required for Anthropic
Connectors Directory compliance — missing annotations are the most
common rejection reason (30% of submissions).

The annotations object contains:

- `title` (string) — human-readable name for UI display
- `readOnlyHint` (boolean) — true if the tool only reads, never modifies
- `destructiveHint` (boolean) — true if the tool may destructively modify
- `idempotentHint` (boolean) — true if repeated calls have no extra effect
- `openWorldHint` (boolean) — true if the tool interacts with external
  entities; false for all shim tools (filesystem only)

Annotations for built-in tools are hardcoded in the schema builder.
Annotations for configured external tools are declared in `shim.toml`
via `title`, `read_only`, `destructive`, and `idempotent` fields.

### 4.3 Protocol Version

The shim echoes whatever protocol version the client requests. If the
client omits the version, the shim responds with `MaxProtocolVersion`
(currently `"2025-06-18"`). This avoids negotiation failures when
Claude Desktop updates its protocol version ahead of the shim.

## 5. Built-In Tools

All paths must be absolute.

### 5.1 Tool List

| Tool     | Operation                                    | Implementation                     |
|----------|----------------------------------------------|------------------------------------|
| read     | Read text file with optional offset/limit    | os.Open, io.ReadAt                 |
| write    | Create or overwrite a text file, append mode | os.CreateTemp, os.Rename           |
| edit     | Find unique string, replace it               | Delegates to strpatch.exe (§7)     |
| copy     | Copy file or directory tree                  | io.Copy / filepath.WalkDir         |
| move     | Move or rename file or directory             | os.Rename                          |
| delete   | Delete file or empty directory               | os.Remove (not RemoveAll)          |
| list     | List directory contents with optional filter | os.ReadDir, filepath.Match         |
| search   | Recursive file search with glob pattern      | filepath.WalkDir, filepath.Match   |
| info     | File/directory metadata                      | os.Stat                            |
| run      | Execute an arbitrary command                 | os/exec.Cmd                        |
| grep     | Search file contents by regex (fallback)     | regexp + filepath.WalkDir          |

### 5.2 read

Returns file content as a text string.

Parameters:
- `path` (string, required) — absolute path
- `offset` (integer, optional) — byte offset to start reading
- `limit` (integer, optional) — maximum bytes to return

Constraints:
- Maximum return size: 512 KB. If file exceeds this (without offset/limit),
  return an error stating the file size and advising use of offset/limit.
- Binary detection: scan the first 8 KB for null bytes. If found, refuse
  with error "File appears to be binary, not text."
- UTF-16 detection: check for BOM (FF FE or FE FF). If found, decode
  the file content from UTF-16 to UTF-8 before returning it. Both
  UTF-16LE (FF FE) and UTF-16BE (FE FF) are supported. The BOM is
  stripped from the output. This applies to all read-only text tools:
  read, head, tail, cat, wc, and diff. Write and edit remain UTF-8
  only — the shim does not produce UTF-16 output.
  Rationale: Windows system tools (PowerShell 5.1 Out-File, reg export,
  wmic) produce UTF-16 by default. Refusing these files entirely blocks
  legitimate workflows on CJK Windows installations and any system
  where legacy tools are in use.
- UTF-8 BOM (EF BB BF): skip the BOM, return content without it.

**Implementation constraint:** When `offset` and/or `limit` are
specified, the implementation must use `os.Open` + `io.ReadAt` (or
`file.Seek` + bounded read) to read only the requested byte range.
It must NOT read the entire file into memory and then slice. For a
500 KB file with `offset=0, limit=100`, only 100 bytes (plus the
8 KB binary-detection header) should be read from disk.

### 5.3 write

Creates or overwrites a file.

Parameters:
- `path` (string, required) — absolute path
- `content` (string, required) — text to write
- `append` (boolean, optional, default false) — append to existing file

Atomicity: write to a temporary file in the same directory (using
os.CreateTemp), then os.Rename over the target. If the process dies
at any point, either the original or the complete new file exists.

Line endings: content arrives from Claude with LF. The shim writes LF
as-is. If the file already exists and uses CRLF, the shim detects this
(scan first 4 KB for \r\n) and converts LF to CRLF in the output. For
new files, use LF.

**CRLF detection and append:** When `append` is true, the shim reads
the existing file once for both CRLF detection and existing content.
It does not re-read the file between these operations. For non-append
writes, CRLF detection reads the existing file, then the atomic write
(temp file + rename) replaces it. Between the detection read and the
rename, the file could change. This is a known TOCTOU limitation
accepted as benign — the worst case is a single write with mismatched
line endings, corrected on the next write.

### 5.4 edit

Delegates to strpatch.exe (see §7 and strpatch-spec.md).

Parameters:
- `path` (string, required) — absolute path
- `old_text` (string, required) — exact text to find, must appear once
- `new_text` (string, required) — replacement text

Escaping: the search and replacement strings can contain newlines,
tabs, quotes, backslashes — any character. These cannot be passed
reliably as Windows command-line arguments (there is no escaping
scheme that handles a literal newline in an argument string).

Instead, the shim spawns strpatch.exe with no command-line arguments
and pipes the parameters as a JSON object to its stdin:

```json
{"path": "...", "old_text": "...", "new_text": "..."}
```

The shim extracts the three fields directly from the tools/call
JSON-RPC params and writes them as a JSON object to strpatch's stdin
pipe. It then closes the write end of the pipe (signalling EOF to
strpatch) and reads stdout/stderr. No escaping or transformation
occurs at any stage — JSON handles all special characters natively.

**Path confinement for edit:** The shim performs the pre-check only
(§8.1 step 1) on the path before delegating to strpatch. The
post-check (GetFinalPathNameByHandle on an open handle) is not
performed because strpatch — not the shim — opens the file.
Introducing a post-check would require the shim to open the file,
verify, close it, then delegate to strpatch, creating a TOCTOU gap
between close and strpatch's open. Since strpatch is trusted code
(a compiled Go binary shipped alongside the shim, not user-supplied),
the pre-check alone is sufficient. If an attacker can swap a junction
between the shim's pre-check and strpatch's open, they already have
write access to the allowed directory, which is a broader compromise.

### 5.5 copy

Parameters:
- `source` (string, required) — absolute path
- `destination` (string, required) — absolute path
- `recursive` (boolean, optional, default false) — copy directory trees

Refuses to overwrite existing files. Returns error if destination exists.

### 5.6 move

Parameters:
- `source` (string, required) — absolute path
- `destination` (string, required) — absolute path

Uses os.Rename. Refuses to overwrite existing files.

### 5.7 delete

Parameters:
- `path` (string, required) — absolute path

Deletes a single file or an **empty** directory only (os.Remove, not
os.RemoveAll). Refuses to delete non-empty directories. No recursive
delete. This is deliberate — Claude should not be able to wipe
directory trees in a single call.

### 5.8 list

Parameters:
- `path` (string, required) — absolute directory path
- `pattern` (string, optional) — glob filter (e.g. "*.py")

Returns names, types (file/directory), and sizes. Does not recurse.

### 5.9 search

Parameters:
- `path` (string, required) — root directory
- `pattern` (string, required) — glob pattern
- `max_results` (integer, optional, default 100) — cap on returned entries

Recursively searches for files matching the glob. Returns absolute paths.
Stops after max_results to prevent runaway output.

### 5.10 info

Parameters:
- `path` (string, required) — absolute path

Returns: size, creation time, modification time, type (file/directory),
read-only flag.

### 5.11 run

Executes an arbitrary command via os/exec. This is how Claude runs
Python scripts, PowerShell one-liners, or any other executable.

Parameters:
- `command` (string, required) — the executable path
- `args` (string, optional) — command-line arguments
- `timeout` (integer, optional, default from config) — inactivity
  timeout in seconds (see §6.6)

The shim passes `command` and `args` directly to exec.Command. No
shell interpretation — cmd.exe is never involved. Claude must specify
the full executable path or a name resolvable via PATH.

**Argument splitting:** The `args` parameter is a single string that
the shim splits into an argument array before passing to exec.Command.
Splitting rules:

- Whitespace (space, tab) separates arguments.
- Double-quoted strings are treated as a single argument:
  `"path with spaces.txt"` → one argument `path with spaces.txt`.
- Single-quoted strings are treated as a single argument:
  `'path with spaces.txt'` → one argument `path with spaces.txt`.
- Quotes are stripped from the result; they are delimiters, not content.
- No backslash escape sequences. A backslash is a literal character.
  This matches Windows path conventions where `\` is a path separator.
- An unclosed quote extends to end of string (no error).

These rules are deliberately simple and Windows-native. They do not
attempt to emulate Unix shell quoting (no `\"` escapes, no `$`
expansion, no globbing). For arguments that cannot be expressed under
these rules, Claude should use `run` with `powershell` or structure
the work differently.

Examples of what Claude sends:
- `{"command": "python", "args": "D:\\projects\\script.py --verbose", "timeout": 10}`
- `{"command": "C:\\Program Files\\Git\\usr\\bin\\grep.exe", "args": "-rn TODO D:\\projects", "timeout": 10}`
- `{"command": "powershell", "args": "-Command Get-Process", "timeout": 10}`

The inactivity timeout and output size limit apply as for configured
external tools (§6.3, §6.5). Default inactivity timeout: 10 seconds
(configurable in config via `inactivity_timeout`). Maximum permitted
timeout: `max_timeout` from config (default 60 seconds).
Output limit: 100 KB.

Path confinement (§8.1) applies to `command` — the executable must
be within an allowed root or on the system PATH.

### 5.12 cat

Read and concatenate one or more text files.

Parameters:
- `paths` (string, required) — space-separated absolute paths
  (quote paths containing spaces), or a JSON array of strings.

Behaviour: read each file in order, concatenate with a newline
separator between files. Each file gets binary/UTF-16 checks and
path confinement (pre-check + post-check) independently. If any
file fails, return error naming the failed file.

Max combined output: 512 KB.

### 5.13 mkdir

Create a directory and any missing parent directories.

Parameters:
- `path` (string, required) — absolute directory path.

Behaviour: `os.MkdirAll(path, 0755)`. Idempotent: succeeds if
directory already exists.

Path confinement: pre-check only (destination may not exist yet).

### 5.14 head

Return the first N lines of a text file.

Parameters:
- `path` (string, required) — absolute file path.
- `lines` (integer, optional, default 10) — number of lines.

Behaviour: open file, binary/UTF-16 check, read first N lines
via scanner. No offset parameter — deliberately simple.

### 5.15 tail

Return the last N lines of a text file.

Parameters:
- `path` (string, required) — absolute file path.
- `lines` (integer, optional, default 10) — number of lines.

Behaviour: for files <= 512 KB, read entire file and return last
N lines. For files > 512 KB, seek backward from end (64 KB
chunk), drop the first partial line, return last N lines.
Binary/UTF-16 checks apply.

### 5.16 diff

Compare two text files and produce a unified diff.

Parameters:
- `file1` (string, required) — absolute path to first file.
- `file2` (string, required) — absolute path to second file.
- `context_lines` (integer, optional, default 3) — lines of
  context around each change.

Behaviour: read both files, compute LCS-based unified diff in
pure Go. Output has `--- +++ @@` headers. Returns empty string
if files are identical. Binary/UTF-16 checks on both files.

No external dependency — implemented in pure Go.

### 5.17 wc

Count lines, words, and bytes in a text file.

Parameters:
- `path` (string, required) — absolute file path.

Behaviour: open file, binary/UTF-16 check, scan line by line
counting lines (newlines), words (whitespace-separated tokens),
and bytes. Return as `lines: N\nwords: N\nbytes: N`.

### 5.18 tree

Recursive indented directory listing.

Parameters:
- `path` (string, required) — absolute directory path.
- `depth` (integer, optional, default 3) — maximum depth.
- `pattern` (string, optional) — glob filter for filenames
  (directories always shown).

Behaviour: `filepath.WalkDir` with depth tracking. Output
indented with two spaces per level. Directories end with `/`,
files don't. Cap: 500 entries max.

### 5.19 roots

Return the allowed_roots list from config.

Parameters: none.

Behaviour: return the `allowed_roots` list, one path per line.
Lets Claude discover where it can work without trial and error.
No path confinement needed (no path parameter).

### 5.20 grep (built-in fallback)

Search file contents by regular expression.

Parameters:
- `path` (string, required) — file or directory to search.
- `pattern` (string, required) — Go RE2 regular expression.
- `recursive` (boolean, optional, default false) — search
  subdirectories.
- `ignore_case` (boolean, optional, default false) — case-insensitive
  matching.
- `line_numbers` (boolean, optional, default true) — prefix matches
  with line numbers.
- `include` (string, optional) — only search files matching this glob
  (e.g. `*.py`).
- `context` (integer, optional) — lines of context around each match.
- `max_results` (integer, optional, default 1000) — cap on returned
  matching lines.

Behaviour: compile the pattern with Go's `regexp` package (RE2
semantics — no backreferences or lookahead). If `path` is a file,
search it line by line. If `path` is a directory and `recursive` is
true, walk the tree with `filepath.WalkDir`, applying the `include`
glob filter. For each file, perform binary/UTF-16 checks per §5.2.
Skip binary files silently. Decode UTF-16 files before matching.
Strip `\r` before matching so `$` anchors work on CRLF files.

Output format: one match per line. If searching multiple files,
prefix each line with the filename and (if `line_numbers` is true)
line number, separated by colons: `path:line:text`.

Path confinement: pre-check + post-check per §8.1 on the root
`path` parameter. Individual files during recursive walk inherit
confinement from the root.

**Registration:** The built-in grep is always registered,
unconditionally. Any `[tools.grep]` section in `shim.toml` is ignored
and the shim emits a `notifications/message` at `warning` level
("grep: [tools.grep] in config is ignored (exe=<path>); built-in
grep is always used").

**Rationale:** External GNU grep from Git for Windows is an MSYS2
binary that mangles Windows paths under recursive search. The
built-in implementation has native Windows path handling, allowed-root
confinement, and symlink/junction escape detection, so it is strictly
safer as well as correct.

**Limitations vs GNU grep:** RE2 regex only (no backreferences,
no lookahead/lookbehind). No Boyer-Moore optimisation — line-by-line
scanning via `bufio.Scanner`. Adequate for source code search;
slower than GNU grep on very large corpora.

## 6. Configured External Tools

### 6.1 Config-Driven Tool Declaration

Configured tools differ from `run` in one important way: they have
rich parameter schemas that Claude can discover via tools/list. This
makes Claude far more likely to use them correctly and consistently.

`run` is a generic escape hatch. Configured tools are curated,
well-described interfaces.

External tools are declared in the config file with explicit descriptions
and parameter schemas. The shim does not auto-discover executables.

The config may list directories for discovery purposes only. Running
the shim with `--scan` lists all .exe files in those directories, to
help the user decide what to add to the config. The config is the sole
source of truth for what is exposed to Claude.

Example:

```toml
[tools.sed]
exe = "C:\\Program Files\\Git\\usr\\bin\\sed.exe"
description = "Apply regex substitutions or line-based transforms to a file."
inactivity_timeout = 5
max_output = 102400
success_codes = [0]

[tools.sed.params]
expression = { type = "string", description = "sed expression (e.g. s/old/new/g)", required = true, flag = "-e" }
path = { type = "string", description = "Input file path", position = 1 }
in_place = { type = "boolean", description = "Edit file in place", default = false, flag = "-i" }
```

**Parameter types:** Each parameter is either a flag or a positional
argument:

- **Flag parameters** have a `flag` field (e.g. `flag = "-e"`). Boolean
  flags are included in the command line when true, omitted when false.
  String/integer flags are emitted as `flag value` (e.g. `-m 10`).

- **Positional parameters** have a `position` field (integer, 1-based).
  They are placed on the command line in position order, after all
  flags. In the sed example above, `path` (position 1) appears
  after the `-e` flag, producing:
  `sed.exe -e "s/old/new/g" "D:\file.txt"`

- A parameter without `flag` or `position` is a config error.
- A parameter with both `flag` and `position` is a config error.
  Each parameter is one or the other, never both.

Note: the `timeout` parameter is not declared in `[tools.sed.params]`.
The shim injects it automatically into every tool's schema (see §6.6).

### 6.2 Execution Model

All external tools are executed via exec.Command directly. Never via
cmd.exe or any shell. This prevents command injection — characters
like &, |, > have no special meaning.

### 6.3 Inactivity Timeout

A single inactivity limit applies to every external tool and to the
`run` built-in. It detects hung, deadlocked, or stalled child
processes by monitoring output activity.

**Mechanism:** The shim monitors both stdout and stderr of the child
process. A `time.Timer` is initialised to the timeout duration when
the child is spawned. Every time either the stdout or stderr reading
goroutine receives bytes from the child, the timer is reset. If the
timer fires (no output on either stream for the timeout duration),
the child is considered stalled and is killed.

**Default:** 10 seconds. Configurable per tool in the config file
via the `inactivity_timeout` field. Overridable per call by Claude
via the `timeout` parameter in the tool schema (see §6.6). Clamped
to the range [1, `max_timeout`] seconds (see §11.1).

**Rationale:** Monitoring both stdout and stderr ensures that a child
producing diagnostic output on stderr (but not yet on stdout) is
recognised as alive. Either stream serves as a heartbeat.

**On expiry:** The shim kills the child process (see §9.8 for the
kill sequence) and returns a **text result** (not a JSON-RPC error)
containing a diagnostic message:

```
winmcpshim: <tool> produced no output for <N> seconds.
You can retry with a higher timeout (max <M>).
```

Where `<M>` is the `max_timeout` value from the config.

Returning a text result rather than a JSON-RPC error allows Claude to
read the message, understand the situation, and retry with an adjusted
timeout or a restructured request.

**Safety backstop — total timeout:** The config also declares a
`total_timeout` per tool (default: 300 seconds). This is an absolute
wall-clock limit from spawn to completion, implemented using
`context.WithTimeout` on the exec.Cmd. It catches the edge case of a
child that trickles output slowly enough to keep resetting the
inactivity timer but never completes. Unlike the inactivity timeout,
the total timeout is not exposed to Claude — it is a config-level
safety constraint. On total timeout expiry, the same kill sequence
and diagnostic message apply, stating "exceeded total timeout".

### 6.4 Exit Code Handling

Each tool config declares which exit codes are non-error. For grep,
codes 0 and 1 are success. Only unlisted codes trigger an error
response.

For the `run` built-in, exit code 0 is success. All other codes
are reported but not treated as tool errors — Claude sees the exit
code and stderr output and decides how to proceed.

### 6.5 Output Size Limit

If a child process produces more than 100 KB of stdout, the shim
truncates the output, kills the child (see §9.8 for the kill
sequence), and appends "[truncated -- output exceeded 100 KB]".
This is configurable per tool. Even 100 KB is a large amount of
text for Claude to process usefully; the limit exists to prevent a
runaway grep from flooding the context window.

Config example:

```toml
[tools.sed]
max_output = 102400   # bytes
```

**Stderr cap:** The same `max_output` limit applies to the stderr
buffer. Unlike stdout, exceeding the stderr limit does not kill the
child — the pipe continues to be drained (to prevent deadlock per
§9.5) but the buffer stops accumulating. When stderr is truncated,
the string `[stderr truncated]` is appended to the captured stderr.
This prevents unbounded memory growth from a child that floods stderr
with verbose logging or repeated error messages.

### 6.6 Timeout Parameter Injection

The shim automatically injects a `timeout` parameter into every
tool's schema when responding to `tools/list`. This applies to all
configured external tools and the `run` built-in. Built-in tools
that do not spawn child processes (read, write, copy, move, delete,
list, search, info) do not receive a `timeout` parameter.

The injected parameter definition:

```json
{
  "name": "timeout",
  "type": "integer",
  "description": "Inactivity timeout in seconds (default: <N> for this tool, max: <M>)"
}
```

Where `<N>` is the per-tool default from the `inactivity_timeout`
config field, and `<M>` is the `max_timeout` value from the
`[security]` config section. The parameter is optional — it is not
marked `required` in the schema.

**Naming convention:** The config field is `inactivity_timeout`
(descriptive, for the human editing TOML). The schema parameter
exposed to Claude is `timeout` (concise, for the LLM sending
JSON-RPC calls). These are different namespaces and the name
difference is deliberate.

**Dispatch behaviour:** On every `tools/call`, the shim:

1. Checks whether `timeout` is present in the call's params.
2. If present, clamps it to the range [1, `max_timeout`].
3. If absent, uses the per-tool `inactivity_timeout` from the config.
4. Strips `timeout` from the params so the tool handler never sees it.
5. Uses the resolved value as the inactivity timeout for the child.

Making `timeout` visible in the schema (with its default stated in
the description) means Claude can proactively set a higher value for
tools it expects to be slow, without needing to fail first.

## 7. strpatch.exe

A separate executable for find-and-replace in text files. See
strpatch-spec.md for its full specification. Also written in Go.

Unlike other external tools which receive command-line arguments,
strpatch.exe receives its parameters as a JSON object on stdin
(see §5.4). This avoids the Windows command-line escaping problem:
there is no reliable way to pass a literal newline as a command-line
argument.

The reason edit is not built into the shim: the string matching
requires CRLF-aware normalisation logic that is fiddly to get right.
Isolating it in a separate process means a bug in edit cannot crash
the shim or corrupt the MCP connection.

## 8. Security

### 8.1 Path Confinement

The config file declares allowed root directories:

```toml
[security]
allowed_roots = [
    "D:\\projects",
    "C:\\Users\\Maurice",
]
```

**The TOCTOU problem:** A naïve approach resolves the path (using
`filepath.EvalSymlinks` + `filepath.Abs`), checks it falls within an
allowed root, then opens the file. Between the resolve and the open,
an attacker (or Claude itself) can swap a symlink or NTFS junction to
point outside the allowed roots. This is a real exploit on Windows
where junction points are trivially created.

**Correct approach — verify after open:** The shim uses a two-step
check:

1. **Pre-check (fast path):** Resolve the path using `filepath.Abs`
   + `filepath.Clean` and verify it falls within an allowed root.
   This catches obvious violations without opening the file. Note:
   `EvalSymlinks` is NOT used here — it is susceptible to TOCTOU.

2. **Post-check (authoritative):** Open the file (or directory),
   then call `GetFinalPathNameByHandle` on the open handle. This
   returns the true canonical path of the already-open file,
   resolving all symlinks, junctions, and reparse points as they
   exist at the moment of the call. Verify this resolved path
   falls within an allowed root. If not, close the handle
   immediately and return a confinement error.

```go
// Pseudocode for path confinement
cleanPath := filepath.Clean(filepath.Abs(requestedPath))
if !isWithinAllowedRoots(cleanPath) {
    return error  // fast reject
}
handle, err := os.Open(cleanPath)
if err != nil {
    return error
}
realPath := windows.GetFinalPathNameByHandle(handle, 0)
if !isWithinAllowedRoots(realPath) {
    handle.Close()
    return error  // symlink/junction escape detected
}
// proceed with operation using the open handle
```

The post-check eliminates the TOCTOU window because the path is
resolved on the already-open handle. The file cannot be swapped
after the check.

**Known limitation:** The pre-check uses `filepath.Clean`, not
`EvalSymlinks`. This means a symlink pointing outside allowed roots
will pass the pre-check but be caught by the post-check. The
pre-check is an optimisation, not a security boundary. The
post-check is the security boundary.

**Error messages:** When a confinement check fails, the error message
must include both the rejected path and the allowed roots:

```
Path not within allowed directories: D:\Users\Someone\Documents\report.txt
Allowed roots: D:\projects, C:\Users\Momo
```

This gives Claude actionable information to relay to the human,
rather than a bare rejection that leaves everyone guessing where
files can be accessed.

The `run` tool's `command` parameter is also subject to confinement:

- **Absolute path:** The executable path is resolved via the
  post-check (GetFinalPathNameByHandle on the open handle) and
  must fall within an allowed root.

- **Unqualified name** (e.g. `python`, `powershell`): The shim
  calls `exec.LookPath` to resolve the name to an absolute path.
  If `LookPath` succeeds, the executable is allowed regardless of
  its location. This is necessary because system executables live
  in directories outside allowed roots (C:\Windows\System32,
  C:\Python314, C:\Program Files\Git\usr\bin, etc.). The
  confinement model trusts the system PATH — if an executable is
  on PATH, the user has already decided it is available. This
  means any directory on the system PATH is effectively an allowed
  source for executables. This is a deliberate design choice, not
  a loophole — the PATH is under the user's control.

### 8.2 No Shell Execution

External tools and the `run` built-in are spawned via exec.Command,
never via cmd.exe. No shell metacharacter interpretation occurs. This
prevents command injection via tool arguments.

Note: Claude can ask to run powershell.exe or cmd.exe explicitly via
the `run` tool — this is permitted because Claude is constructing the
arguments, not interpreting untrusted input through a shell. The
restriction is that the *shim* never introduces a shell layer.

### 8.3 No Recursive Delete

The delete tool uses os.Remove (not os.RemoveAll). It refuses
non-empty directories. There is no recursive delete capability.
This is a deliberate safety constraint.

### 8.4 No Binary File Operations

read and edit refuse binary files (detected by null bytes in the first
8 KB). UTF-16 files (detected by BOM) are decoded to UTF-8 for
read-only operations (see §5.2). Write and edit operate on UTF-8
text files only.

## 9. Error Handling, Process Lifecycle, and Failure Modes

### 9.1 General Principles

- Detect and refuse is preferred over silent conversion or fuzzy matching.
- Every refusal includes a clear, specific error message.
- The shim never exits on a tool error. It returns a JSON-RPC error
  response and continues serving.
- All errors are wrapped with context using fmt.Errorf("...: %w", err)
  so error messages include the full chain of what went wrong.
- Every `tools/call` dispatch is wrapped in a `recover()` handler
  (see §9.11) so that a panic in any tool handler cannot kill the shim.

### 9.2 File Locking

Windows files may be locked by other processes (antivirus, sync clients,
editors). On sharing violation errors, retry 3 times with 50ms, 200ms,
500ms backoff. If still locked, return error "File is locked by another
process."

### 9.3 Atomic Writes

All file-modifying operations (write, edit, copy) write to a temporary
file in the same directory (os.CreateTemp), then os.Rename over the
target. This guarantees that a crash or power failure never produces a
truncated file.

### 9.4 Long Paths

Go's os package handles long paths on Windows transparently when the
application manifest enables long path support, or when using the
\\?\ prefix. All paths must be absolute.

### 9.5 Concurrent Pipe Draining

When the shim spawns a child process (external tools, `run`, or
strpatch), it creates pipes for both stdout and stderr. These pipes
**must** be drained concurrently in separate goroutines. Sequential
draining (read all of stdout, then read all of stderr) causes a
deadlock if the child fills the stderr pipe buffer (64 KB on Windows)
while the shim is still blocked reading stdout.

**Implementation:** Two goroutines, one per pipe, each reading into a
`bytes.Buffer`. A `sync.WaitGroup` tracks completion:

```go
var stdoutBuf, stderrBuf bytes.Buffer
var wg sync.WaitGroup
wg.Add(2)
go func() {
    defer wg.Done()
    // read from stdoutPipe into stdoutBuf
    // reset inactivity timer on each read
}()
go func() {
    defer wg.Done()
    // read from stderrPipe into stderrBuf
    // reset inactivity timer on each read
}()
wg.Wait()
```

Both goroutines feed the same inactivity timer (§6.3). Each read
from either pipe resets the timer.

Note: Go's `cmd.CombinedOutput()` drains both pipes concurrently
but merges them into one buffer. The shim requires separate buffers
because stdout is the tool result and stderr is diagnostics. The
two-goroutine approach is therefore explicit and mandatory.

### 9.6 Child Stdin Handling

Every child process must have its stdin explicitly managed to prevent
the child from blocking on an unexpected read.

**strpatch:** The shim writes the JSON parameter object to the child's
stdin pipe, then closes the write end of the pipe. strpatch reads
until EOF, processes the request, and exits. This is specified in §5.4.

**All other child processes** (configured tools, `run`): The child's
stdin is connected to `os.DevNull`. The child receives immediate EOF
if it attempts to read. No hang is possible.

This is a single line in the implementation:

```go
cmd.Stdin = nil  // Go connects to os.DevNull when Stdin is nil
```

### 9.7 Concurrent Requests

The shim processes tool calls sequentially — one at a time, in order
received. This prevents concurrent edits to the same file and keeps
the implementation simple. Claude's typical behaviour is to wait for
each tool result before sending the next call.

### 9.8 Child Process Lifecycle

When the shim spawns a child process, the following lifecycle applies
in all cases — normal completion, timeout, output truncation, or any
other termination cause. All items in this section are mandatory for
v1 release. The shim must not ship without Job Object support, WER
suppression, and the full kill/cleanup sequence.

**Job Object (Windows):** Before starting the child, the shim creates
a Windows Job Object and configures it with
`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`. The child process is assigned
to the Job Object immediately after creation. When the Job Object
handle is closed, Windows terminates all processes in the job — the
child and any grandchildren it may have spawned. This prevents orphaned
grandchild processes.

```go
// Pseudocode for Job Object lifecycle
job, _ := windows.CreateJobObject(nil, nil)
info := JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
    BasicLimitInformation: JOBOBJECT_BASIC_LIMIT_INFORMATION{
        LimitFlags: JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
    },
}
windows.SetInformationJobObject(job, ...)
cmd.Start()
windows.AssignProcessToJobObject(job, cmd.Process.Handle)
defer windows.CloseHandle(job)  // kills child + grandchildren
```

**WER Dialog Suppression:** Before starting the child, the shim sets
`CREATE_DEFAULT_ERROR_MODE` in the child's creation flags via
`SysProcAttr`. This prevents the Windows Error Reporting dialog from
appearing if the child crashes (access violation, divide by zero,
stack overflow). Without this, a crashing child displays a modal
dialog that blocks the process indefinitely; the shim would have to
wait for the inactivity timeout to kill it. With WER suppressed, the
child terminates immediately with a crash exit code (e.g. 0xC0000005),
allowing the shim to detect the failure at once.

```go
cmd.SysProcAttr = &syscall.SysProcAttr{
    CreationFlags: windows.CREATE_DEFAULT_ERROR_MODE,
}
```

**Kill sequence:** When the shim must terminate a child (inactivity
timeout, total timeout, output truncation, or shim shutdown):

1. `cmd.Process.Kill()` — sends `TerminateProcess`. On Windows this
   is unconditional; the child cannot trap or ignore it.
2. `wg.Wait()` — wait for both pipe-reading goroutines (§9.5) to
   finish draining. The kill unblocks any pending reads on the pipes.
3. `cmd.Wait()` — release the process handle, thread handle, and
   pipe handles. This call is mandatory even after a kill. Omitting
   it leaks OS handles.
4. `windows.CloseHandle(job)` — close the Job Object handle, which
   kills any grandchild processes.

Steps 2–4 are typically handled via `defer` statements established
immediately after `cmd.Start()` succeeds, ensuring they execute
regardless of how the function exits (normal return, timeout, panic
caught by `recover()`).

**Handle leak prevention:** Over a long session (hundreds of tool
calls), leaked process handles, pipe handles, or Job Object handles
accumulate until the OS refuses to create new processes. The `defer`
chain guarantees cleanup:

```go
cmd.Start()
defer cmd.Wait()                   // always release process handle
defer windows.CloseHandle(job)     // always release job object
// ... start pipe goroutines ...
defer wg.Wait()                    // always wait for pipe goroutines
```

The `defer` order ensures correct sequencing: deferred calls execute
in LIFO order, so `wg.Wait()` runs first (drain pipes), then
`CloseHandle(job)` (kill grandchildren), then `cmd.Wait()` (release
handles).

### 9.9 Runaway Child Processes

Three mechanisms prevent a child from running indefinitely:

1. **Inactivity timeout** (§6.3): kills a child that produces no
   output on either stdout or stderr for the configured duration.
   Default 10 seconds, maximum `max_timeout` (default 60),
   overridable per call by Claude.

2. **Total timeout** (§6.3): kills a child that exceeds the absolute
   wall-clock limit, regardless of output. Config-level only, not
   exposed to Claude. Default 300 seconds.

3. **Output size limit** (§6.5): kills a child that produces more
   than the configured maximum output. Default 100 KB.

On any of these, the kill sequence in §9.8 applies.

### 9.10 Windows-Specific Process Considerations

**TerminateProcess behaviour:** `cmd.Process.Kill()` calls
`TerminateProcess` on Windows, which is unconditional — the child
cannot trap, catch, or ignore it. This is safer than Unix, where
`SIGKILL` can hang on uninterruptible I/O. On Windows, kill always
succeeds.

**Orphaned grandchildren:** Without Job Objects (§9.8), killing a
child does not kill its grandchildren. They become orphaned — running
indefinitely with no parent. The Job Object mechanism is the only
reliable solution on Windows. It is not optional.

**CreateProcess cost:** Each tool call spawns a new child process
via `CreateProcess`. This costs approximately 20–50 ms for compiled
executables and 1–3 seconds for Python (interpreter startup plus
imports). This cost is accepted as a design tradeoff: spawn-per-call
eliminates state leaks between calls, crash recovery complexity, and
the need for a protocol between the shim and a persistent worker.
Given that LLM inference between tool calls typically takes 3–15
seconds, the spawn overhead is a small fraction of round-trip time.

### 9.11 Panic Recovery

Every `tools/call` dispatch is wrapped in a `recover()` handler.
If any code in the tool handler panics (nil pointer dereference,
index out of range, explicit `panic()` call), the `recover()` catches
it and returns a JSON-RPC error response for that single call. The
shim's main loop continues serving the next request.

```go
func handleToolCall(request Request) (response Response) {
    defer func() {
        if r := recover(); r != nil {
            response = errorResponse(
                request.ID,
                fmt.Sprintf("internal error: %v", r),
            )
        }
    }()
    // ... dispatch to tool handler ...
}
```

Without `recover()`, a panic in any tool handler would unwind the
entire call stack and kill the shim process, severing the connection
to Claude Desktop.

The `recover()` is placed only around `tools/call` dispatch. The
JSON-RPC read loop and protocol handling are the shim's own code
and should be panic-free by testing.

### 9.12 Shutdown Procedure

The shim shuts down when it detects that Claude Desktop has
disconnected. Two signals indicate disconnection:

1. **EOF on stdin:** The `json.Decoder` reading stdin returns `io.EOF`.
   Claude Desktop has closed its end of the pipe.

2. **Broken pipe on stdout:** A write to stdout returns an error
   (EPIPE or equivalent). Claude Desktop has closed its end of the
   pipe while the shim was writing a response. The `writeAndLog`
   function (or equivalent) must check the return value of every
   write to stdout and trigger shutdown on error. Ignoring write
   errors leaves the shim running as an orphan.

On either signal, the shim initiates an orderly shutdown:

1. Set a shutdown flag to prevent further tool dispatch.
2. If a child process is currently running:
   a. Kill it (`cmd.Process.Kill()`).
   b. Wait for pipe-reading goroutines (`wg.Wait()`).
   c. Close the Job Object handle.
   d. Wait for the child (`cmd.Wait()`).
3. Flush and close the log file (if `--log` is active).
4. Exit with code 0.

No JSON-RPC response is written during shutdown — there is nobody
listening. The shim does not attempt to write a "goodbye" message.

If the shim is between tool calls (idle, waiting on stdin) when EOF
arrives, shutdown is immediate: close the log file, exit 0.

### 9.13 Config File Errors

- Missing config: shim starts with built-in tools only, no external tools.
- Malformed config: refuse to start, log error and exit.
- Config saved as UTF-16: detect BOM, refuse with message "Config file
  must be saved as UTF-8."
- Missing or incomplete `[builtin_descriptions]`: refuse to start, log
  error naming the built-in tool(s) missing a description. Every built-in
  tool must have a description entry (see §11.2).

### 9.14 Failure Table

| Failure                      | Detection                           | Response                                      |
|------------------------------|-------------------------------------|-----------------------------------------------|
| Tool call to unknown tool    | Name not in registry                | JSON-RPC error -32602                         |
| File not found               | os.ErrNotExist                      | JSON-RPC error with path                      |
| File is binary               | Null bytes in first 8 KB            | Refuse: "File appears to be binary"           |
| File is UTF-16               | BOM FF FE or FE FF                  | Decode to UTF-8 for read-only tools |
| File too large for read      | Size > 512 KB without offset/limit  | Refuse with file size, advise offset/limit    |
| File locked                  | Sharing violation after retry       | Refuse: "File is locked by another process"   |
| Path outside allowed roots   | Pre-check + post-check (§8.1) | Refuse: "Path not within allowed directories" |
| Symlink/junction escape      | GetFinalPathNameByHandle (§8.1)| Refuse: "Path not within allowed directories" |
| Inactivity timeout           | No output on stdout or stderr       | Text result with diagnostic (§6.3)            |
| Total timeout                | Wall-clock limit exceeded           | Text result with diagnostic (§6.3)            |
| Output exceeds limit         | Byte count during read              | Truncate, kill child, note truncation         |
| Child crash (access violation)| Non-zero exit code after WER suppression | JSON-RPC error with exit code             |
| Child hangs on stdin         | Cannot occur — stdin is os.DevNull  | N/A (prevented by §9.6)                       |
| Pipe deadlock                | Cannot occur — concurrent draining  | N/A (prevented by §9.5)                       |
| Orphaned grandchildren       | Cannot occur — Job Object kills all | N/A (prevented by §9.8)                       |
| Handle leak                  | Cannot occur — defer chain          | N/A (prevented by §9.8)                       |
| Panic in tool handler        | recover() catches                   | JSON-RPC error: "internal error: ..."         |
| Claude Desktop disconnects   | EOF on stdin or broken pipe on stdout| Kill child, clean up, exit 0 (§9.12)         |
| Delete non-empty directory   | os.Remove fails, dir not empty      | Refuse: "Directory is not empty"              |
| Destination already exists   | os.Stat succeeds                    | Refuse: "Destination already exists"          |
| Missing builtin_description  | Config parse at startup             | Fatal: refuse to start, name missing tool     |
| Param has both flag+position | Config parse at startup             | Fatal: refuse to start, name invalid param    |

### 9.15 Critical Error Alerting

Certain errors represent conditions that should never occur during
normal operation. When one is detected, the shim must alert Claude
in a way that ensures the user is informed — not buried in a
generic "the tool encountered an error" summary.

#### 9.15.1 Classification

An error is **critical** if it indicates a security breach,
an internal invariant violation, or corruption. The exhaustive
list of critical errors:

| Trigger | Detected by | Meaning |
|---------|-------------|---------|
| Path confinement post-check failure | `checkPathConfinementFull` | Symlink/junction escaped allowed roots — security breach |
| Panic recovery triggered | `recover()` in `handleToolsCall` | Internal invariant violated |
| Config integrity failure at runtime | Config reload or validation | Configuration corrupted after startup |

Normal errors (file not found, search text not unique, timeout,
permission denied, etc.) are **not** critical. They are expected
operational conditions and continue to use the existing error
response format unchanged.

#### 9.15.2 Tool response format

Critical errors are returned as a normal `tools/call` response
with `isError: true` and specially formatted text content:

```
🛑 CRITICAL: <message>

This should never occur during normal operation.
Please alert the user about this issue immediately.
Do not retry this operation.
```

The `🛑 CRITICAL:` prefix and the explicit instruction to
alert the user are deliberate prompt engineering. Claude reads
tool response text as context for its reply; an explicit
instruction in that text reliably changes Claude's tone and
causes it to surface the issue prominently.

#### 9.15.3 Supplementary notification

In addition to the tool response, the shim sends a
`notifications/message` at `error` level:

```json
{
    "jsonrpc": "2.0",
    "method": "notifications/message",
    "params": {
        "level": "error",
        "logger": "winmcpshim",
        "data": "<same message>"
    }
}
```

The notification is sent **before** the tool response (belt
and braces — if the tool response is somehow lost, the
notification still reaches the log). Claude Desktop surfaces
error-level notifications in its log viewer.

#### 9.15.4 Implementation

A single function wraps the critical error text:

```go
func criticalErrorText(msg string) string
```

This function is called at each critical error site. The
caller then returns the formatted text as a tool error
response (isError: true) via the existing error path, and
separately calls `sendCriticalNotification(msg)` to emit
the notification.

Normal error paths are not modified. Only the three trigger
sites listed in §9.15.1 call this function.

## 10. Diagnostics

### 10.1 --verbose Flag

When set, the shim writes diagnostics to stderr:
- `[shim:in]` + every JSON-RPC request received from Claude Desktop
- `[shim:out]` + every JSON-RPC response sent to Claude Desktop
- `[shim:call]` + tool name and parameters for each tools/call
- `[shim:spawn]` + command line for each external tool or run command
- `[shim:event]` + startup, shutdown, exit codes, timeouts, kills

When run from a terminal, stderr is visible directly. When run by
Claude Desktop, stderr is discarded.

### 10.2 --log Flag

When `--log <folder>` is specified, the shim creates a log file in
the given folder at startup. The filename is the startup timestamp:

```
YYMMDDHHMMSS.log
```

For example, a shim starting on 26 March 2026 at 14:05:33 creates:

```
D:\logs\shim\260326140533.log
```

The log receives exactly the same output as --verbose stderr, but
is always enabled when the flag is present (--verbose is not
required). If both --verbose and --log are set, output goes to
both stderr and the log file.

The folder must already exist. If it does not, or the log file
cannot be created, the shim writes an error to stderr and exits.

The log file is opened at startup and kept open for the lifetime
of the shim process. Writes are flushed after every line (using
a bufio.Writer with explicit Flush, or log.SetOutput with an
io.MultiWriter) to ensure the log is useful even if the shim
crashes.

### 10.3 --scan Flag

Lists all .exe files found in directories declared in the config.
Outputs to stdout and exits. Used as a discovery aid when populating
the config file with external tools.

## 11. Configuration

### 11.1 Config File

Located alongside the shim executable: `shim.toml`.

Parsed using a TOML library (e.g. github.com/BurntSushi/toml).

```toml
[security]
allowed_roots = [
    "D:\\projects",
    "C:\\Users\\Maurice",
]
max_timeout = 60          # maximum inactivity timeout Claude can request via
                          # the timeout schema parameter (seconds, §6.6)

[builtin_descriptions]
cat = "Read and concatenate one or more files."
copy = "Copy a file or directory tree."
delete = "Delete a single file or empty directory."
diff = "Compare two text files and show unified diff."
edit = "Find-and-replace a unique string in a text file."
grep = "Search inside file contents by regex."
head = "Show the first N lines of a file (default 10)."
info = "Get file or directory metadata: size, timestamps, type."
list = "List directory contents with optional glob filter."
mkdir = "Create a directory (and parent directories)."
move = "Move or rename a file or directory."
read = "Read a text file. Returns content as a string. Use offset/limit for large files."
roots = "Return the list of allowed root directories."
run = "Execute an arbitrary command. Use timeout to control inactivity limit."
search = "Recursively search for files matching a glob pattern."
tail = "Show the last N lines of a file (default 10)."
tree = "Recursive indented directory listing with depth limit."
wc = "Count lines, words, and bytes in a file."
write = "Create or overwrite a text file. Atomic write via temp file + rename."

[scan_dirs]
paths = [
    "C:\\Program Files\\Git\\usr\\bin",
]

[run]
inactivity_timeout = 10   # default inactivity timeout (seconds)
total_timeout = 300       # absolute wall-clock limit (seconds)
max_output = 102400

[tools.sed]
exe = "C:\\Program Files\\Git\\usr\\bin\\sed.exe"
description = "Apply regex substitutions or line-based transforms to a file."
inactivity_timeout = 5
total_timeout = 15
max_output = 102400
success_codes = [0]

[tools.sed.params]
expression = { type = "string", description = "sed expression (e.g. s/old/new/g)", required = true, flag = "-e" }
path = { type = "string", description = "Input file path", position = 1 }
in_place = { type = "boolean", description = "Edit file in place", default = false, flag = "-i" }
```

Note: `[tools.grep]` is a reserved name — grep is always served by
the built-in implementation. Any `[tools.grep]` section is ignored
with a startup warning. See §5 for the rationale.

### 11.2 Built-In Tool Descriptions

Built-in tool descriptions are declared in the config file under a
`[builtin_descriptions]` table. Every built-in tool must have a
description entry — all 18 built-in tools (cat, copy, delete,
diff, edit, grep, head, info, list, mkdir, move, read, roots, run,
search, tail, tree, wc, write).

If any built-in tool is missing its description in the config, the
shim refuses to start and logs an error naming the missing tool.
This is a deliberate fail-fast: a tool without a description is
invisible or confusing to Claude.

```toml
[builtin_descriptions]
cat = "Read and concatenate one or more files."
copy = "Copy a file or directory tree."
delete = "Delete a single file or empty directory."
diff = "Compare two text files and show unified diff."
edit = "Find-and-replace a unique string in a text file."
grep = "Search inside file contents by regex."
head = "Show the first N lines of a file (default 10)."
info = "Get file or directory metadata: size, timestamps, type."
list = "List directory contents with optional glob filter."
mkdir = "Create a directory (and parent directories)."
move = "Move or rename a file or directory."
read = "Read a text file. Returns content as a string. Use offset/limit for large files."
roots = "Return the list of allowed root directories."
run = "Execute an arbitrary command. Use timeout to control inactivity limit."
search = "Recursively search for files matching a glob pattern."
tail = "Show the last N lines of a file (default 10)."
tree = "Recursive indented directory listing with depth limit."
wc = "Count lines, words, and bytes in a file."
write = "Create or overwrite a text file. Atomic write via temp file + rename."
```

The descriptions are included verbatim in the `tools/list` response
as the tool's `description` field. They should be concise (one line)
and action-oriented.

### 11.3 Command Line

```
shim.exe [--verbose] [--log <folder>] [--scan]
```

- `--verbose`: write diagnostics to stderr.
- `--log <folder>`: write diagnostics to a timestamped log file in
  the given folder. Filename format: `YYMMDDHHMMSS.log`.
- `--scan`: list discovered executables and exit.

### 11.4 Claude Desktop Configuration

```json
{
    "mcpServers": {
        "files": {
            "command": "D:\\projects\\MCP_tools\\WinMcpShim\\winmcpshim.exe",
            "args": ["--log", "D:\\logs\\shim"]
        }
    }
}
```

## 12. Build

### 12.1 Prerequisites

Go 1.25 or later. Single zip download from https://go.dev/dl/,
no installer required. Extract to a folder and add to PATH.

### 12.2 Project Structure

```
WinMcpShim/
├── go.mod
├── go.sum
├── shim/
│   ├── main.go              # Entry point, arg parsing, MCP protocol loop
│   └── logging.go           # --verbose / --log output
├── shared/
│   ├── config.go            # TOML config loading
│   ├── constants.go         # All shared constants
│   ├── critical.go          # Critical error formatting
│   ├── helpers.go           # File I/O helpers, atomic write, retry
│   ├── jsonrpc.go           # JSON-RPC message types and helpers
│   ├── security.go          # Path confinement checks
│   ├── security_windows.go  # GetFinalPathNameByHandle post-check
│   ├── process_windows.go   # Job Objects, WER suppression
│   ├── sharing_windows.go   # Sharing violation detection
│   └── fileinfo_windows.go  # File creation time extraction
├── tools/
│   ├── builtin.go           # Built-in tool implementations + schemas
│   ├── edit.go              # Edit tool (strpatch delegation)
│   ├── external.go          # Configured tool dispatch
│   ├── grep.go              # Built-in grep fallback
│   └── run.go               # run tool and child process lifecycle
├── installer/               # Installer library (types, discovery, config)
├── cmd/
│   ├── install/main.go      # install.exe entry point
│   ├── uninstall/main.go    # uninstall.exe entry point
│   └── bench/main.go        # Benchmark suite
├── testhelpers/rogue/       # Adversarial test helper (rogue.exe)
└── strpatch/
    ├── go.mod
    └── main.go              # strpatch.exe entry point
```

### 12.3 Build Commands

```
cd WinMcpShim
go build -o winmcpshim.exe ./shim
cd strpatch
go build -o strpatch.exe .
```

Both produce statically linked executables with no runtime
dependencies. Expected binary size: ~5 MB each.

### 12.4 Dependencies

- github.com/BurntSushi/toml — TOML config parsing
- golang.org/x/sys/windows — Job Object and WER APIs
- No other external dependencies. All file I/O, JSON, process
  management, and concurrency use the Go standard library.

## 13. What the Shim Does Not Do

- Recursive delete.
- Binary file operations.
- UTF-16 write (the shim reads UTF-16 but only writes UTF-8).
- Network access.
- Registry access.
- Clipboard access.
- Screenshot or UI interaction.
- Fuzzy matching or silent encoding conversion.

## 14. Testing

### 14.1 Approach

Integration tests using Go's testing package. Each test spawns the
shim as a subprocess (exec.Command), sends JSON-RPC on stdin, reads
responses from stdout, and asserts on results. Go's testing
infrastructure handles parallel test execution, timeouts, and
reporting.

A separate Python test harness is also viable if preferred.

### 14.2 File Operation Tests

Each built-in tool is tested against:
- Normal operation (happy path)
- File not found
- Binary file (should refuse)
- UTF-16 file (should decode to UTF-8 for read-only tools)
- File locked by another process
- Path outside allowed roots
- File > 512 KB (for read without offset/limit)
- CRLF line endings preserved through write cycle
- Atomic write (kill shim mid-write, verify file integrity)
- Long paths (> 260 characters)
- Symlink/junction escape: create a junction inside allowed roots
  pointing outside, verify post-check catches it via
  GetFinalPathNameByHandle
- read with offset/limit: verify only the requested range is read
  (not the entire file loaded into memory)

### 14.3 run Tool Tests

- Run python with a simple script, verify stdout captured
- Run an executable that exits non-zero, verify exit code reported
- Run a command that hangs (inactivity timeout triggers)
- Run a command that runs too long (total timeout triggers)
- Run a command producing > 100 KB output (truncation and kill)
- Run with path outside allowed roots (refused)
- Arguments containing special characters (no injection)
- Timeout parameter respected: default 10, explicit override,
  clamped to [1, 60]
- Timeout parameter stripped from args before dispatch

### 14.4 Configured Tool Tests

- grep with matches
- grep with no matches (exit code 1, not an error)
- grep with invalid regex (exit code 2, is an error)
- Tool inactivity timeout (mock slow tool, no output)
- Tool total timeout (mock steady-drip tool)
- Tool producing > 100 KB output (truncation and kill)
- Arguments with special characters (no injection)
- Timeout parameter injected into schema in tools/list
- Timeout parameter override via tools/call

### 14.5 Process Lifecycle Tests

- Child crash with WER suppressed: spawn a program that triggers an
  access violation, verify shim receives non-zero exit code
  immediately (no 10-second wait for dialog)
- Job Object cleanup: spawn a child that spawns a grandchild (e.g.
  python launching a subprocess), kill the child, verify the
  grandchild is also terminated
- Handle leak: run 500 sequential tool calls, verify no handle
  accumulation (use Process Explorer or Go runtime metrics)
- Pipe deadlock prevention: spawn a child that writes 100 KB to
  stderr and 100 KB to stdout, verify both captured without deadlock
- Child stdin: spawn a child that reads from stdin, verify it
  receives EOF immediately and does not hang
- Concurrent pipe draining: verify stdout and stderr are captured
  in separate buffers (not merged)

### 14.6 Panic Recovery Tests

- Inject a deliberate panic in a test tool handler, verify shim
  returns JSON-RPC error and continues serving subsequent requests
- Verify panic recovery logs the error via the diagnostic system

### 14.7 Shutdown Tests

- Simulate Claude Desktop disconnect (close shim's stdin), verify
  shim exits cleanly with code 0
- Simulate disconnect while child is running, verify child is killed
  and shim exits cleanly
- Simulate broken pipe on stdout (close shim's stdout while it is
  writing a response), verify clean shutdown
- Verify log file is flushed and closed on shutdown

### 14.8 UTF-16 Decode Tests

- Read a UTF-16LE file (FF FE BOM), verify content returned as UTF-8
- Read a UTF-16BE file (FE FF BOM), verify content returned as UTF-8
- head, tail, cat, wc, diff on UTF-16 files: verify decode works
- write to a path that was read as UTF-16: verify output is UTF-8
  (no round-trip to UTF-16)
- edit on a UTF-16 file: verify refusal (edit is not read-only)

### 14.9 Built-in Grep Tests

- Single file: match found, verify path:line:text format
- Single file: no match, verify empty result
- Recursive directory search with include filter
- ignore_case flag
- context lines around matches
- max_results cap: generate more matches than cap, verify truncation
- Binary file in search tree: verify skipped silently
- UTF-16 file in search tree: verify decoded and searched
- CRLF file: pattern anchored with $, verify match works
- Path outside allowed roots: verify confinement error
- Fallback registration: configure external grep with missing exe,
  verify built-in registered and warning notification sent
- External forbidden: configure external grep with valid exe,
  verify built-in still wins and warning notification sent
- Invalid regex: verify clear error message

### 14.10 Stderr Cap Tests

- Child producing > MaxOutput on stderr: verify buffer truncated,
  child not killed, stdout still captured, "[stderr truncated]"
  appended
- Child producing < MaxOutput on stderr: verify full stderr captured

### 14.11 Confinement Error Message Tests

- Path outside allowed roots: verify error message includes the
  rejected path and all allowed roots


## Appendix A. Compliance Summary

All mandatory items are implemented and tested. The full
requirements traceability matrix is in §15 below.

### A.1 Mandatory Items

| §     | Item                         | Status | Test                                           |
|-------|------------------------------|--------|------------------------------------------------|
| 4.2   | Tool annotations             | OK     | `TestToolsList_NewBuiltins`                    |
| 8.1   | Path confinement (post-check)| OK     | `TestSecurity_JunctionEscape`                  |
| 9.5   | Concurrent pipe draining     | OK     | `TestRun_ConcurrentPipeDraining`               |
| 9.8   | Job Objects                  | OK     | `TestRun_JobObjectGrandchildKilled`            |
| 9.8   | WER dialog suppression       | OK     | `TestRun_WERSuppression`                       |
| 9.8   | Kill sequence                | OK     | `TestRun_HandleLeak`                           |
| 9.8   | Handle leak prevention       | OK     | `TestRun_HandleLeak`                           |
| 9.11  | Panic recovery               | OK     | `TestPanicRecovery`                            |
| 9.12  | Shutdown on EOF              | OK     | `TestShutdown_StdinEOF`                        |
| 9.12  | Shutdown on broken pipe      | OK     | `TestShutdown_BrokenStdoutPipe`                |

### A.2 Known Limitations

| §     | Limitation                                | Severity | Rationale                                     |
|-------|-------------------------------------------|----------|-----------------------------------------------|
| 5.3   | CRLF detection TOCTOU on non-append write | Low      | Worst case: one write with wrong line endings  |
| 5.3   | Atomic write crash-resilience untested    | Low      | temp+rename is correct; kill-timing unreliable |
| 8.1   | Pre-check does not resolve symlinks       | None     | Post-check is the security boundary            |
| 9.10  | CreateProcess cost per call               | Low      | Accepted tradeoff for isolation                |

### A.3 Implementation vs Spec Defects

When the implementation diverges from this specification, the
specification is authoritative. Implementation bugs are tracked
as defects, not as spec amendments. The spec is not weakened to
match a broken implementation.

Exception: if the spec itself contains an error or ambiguity
discovered during implementation, the spec is amended with a
dated note explaining the change.


---

## 15. Requirements Traceability Matrix

Previously maintained in `RTM.md` (Part 1). Merged here for
single-source traceability.

### Status key

| Code    | Meaning |
|---------|---------|
| OK      | Implemented and tested |
| N/A     | Not testable or informational only |
| ACCEPT  | Known limitation, accepted by spec |
| PARTIAL | Partial coverage |
| MISS    | Not implemented |

### §4 MCP Protocol Surface

| Req | Description | Test | Status | Notes |
|-----|-------------|------|--------|-------|
| 4.1 | Respond to `initialize` with capabilities declaring `"tools": {}` | `TestProtocol_Initialize` | OK | |
| 4.2 | Accept `notifications/initialized` and `initialized` — no response | `TestProtocol_Initialize` (via handshake) | OK | Both forms accepted |
| 4.3 | Respond to `tools/list` with full tool registry | `TestProtocol_ToolsList` | OK | |
| 4.4 | Respond to `tools/call` — dispatch to handler | multiple tool tests | OK | |
| 4.5 | Reject unknown methods with -32601 | `TestProtocol_UnknownMethod` | OK | |
| 4.6 | Preserve request `id` type (int, string, null) via `json.RawMessage` | `TestProtocol_IdTypePreservation` | OK | |
| 4.7 | Notification (no `id`) — log, no response | `TestProtocol_NotificationNoResponse` | OK | |
| 4.8 | Tool annotations on all tools | `TestToolsList_NewBuiltins` | OK | |
| 4.9 | Protocol version 2025-06-18 | `TestProtocol_Initialize` | OK | |

### §5 Built-In Tools

| Req | Description | Test | Status |
|-----|-------------|------|--------|
| 5.2.1 | read: return file content | `TestRead_HappyPath` | OK |
| 5.2.2 | read: max 512 KB without offset/limit | `TestRead_TooLarge` | OK |
| 5.2.3 | read: binary detection | `TestRead_BinaryFile` | OK |
| 5.2.4 | read: UTF-16 decode to UTF-8 | `TestRead_UTF16File`, `TestRead_UTF16LEDecode`, `TestRead_UTF16BEDecode` | OK | |
| 5.2.5 | read: skip UTF-8 BOM | `TestRead_UTF8BOMSkipped` | OK |
| 5.2.6 | read: offset/limit partial read | `TestRead_OffsetLimit` | OK |
| 5.2.7 | read: path confinement | `TestRead_PathOutsideRoots` | OK |
| 5.3.1 | write: create or overwrite | `TestWrite_HappyPath` | OK |
| 5.3.2 | write: atomic via temp+rename | — | ACCEPT |
| 5.3.3 | write: append mode | `TestWrite_AppendMode` | OK |
| 5.3.4 | write: CRLF preservation | `TestWrite_CRLFPreservation` | OK |
| 5.3.5 | write: new files use LF | `TestWrite_NewFileLF` | OK |
| 5.3.6 | write: single read for append+CRLF | `TestWrite_AppendMode` | OK |
| 5.3.7 | write: TOCTOU on non-append | — | ACCEPT |
| 5.4.1 | edit: delegate to strpatch via stdin pipe | `TestEdit_HappyPath` | OK |
| 5.4.2 | edit: JSON on stdin, no CLI args | — | N/A |
| 5.4.3 | edit: pre-check confinement only | — | N/A |
| 5.4.4 | edit: search not found error | `TestEdit_NotFound` | OK |
| 5.4.5 | edit: search not unique error | `TestEdit_NotUnique` | OK |
| 5.5.1 | copy: file | `TestCopy_HappyPath` | OK |
| 5.5.2 | copy: refuse overwrite | `TestCopy_DestinationExists` | OK |
| 5.5.3 | copy: recursive directory | `TestCopy_RecursiveDir` | OK |
| 5.5.4 | copy: refuse dir without recursive | `TestCopy_DirWithoutRecursive` | OK |
| 5.6.1 | move: file | `TestMove_HappyPath` | OK |
| 5.6.2 | move: refuse overwrite | `TestMove_DestinationExists` | OK |
| 5.7.1 | delete: file | `TestDelete_File` | OK |
| 5.7.2 | delete: empty directory | `TestDelete_EmptyDir` | OK |
| 5.7.3 | delete: refuse non-empty dir | `TestDelete_NonEmptyDir` | OK |
| 5.7.4 | delete: no recursive (os.Remove only) | `TestDelete_NonEmptyDirSemantics` | OK |
| 5.8.1 | list: directory contents | `TestList_WithAndWithoutPattern` | OK |
| 5.8.2 | list: glob filter | `TestList_WithAndWithoutPattern` | OK |
| 5.8.3 | list: names, types, sizes | `TestList_OutputFormat` | OK |
| 5.9.1 | search: recursive | `TestSearch_Recursive` | OK |
| 5.9.2 | search: glob pattern | `TestSearch_Recursive` | OK |
| 5.9.3 | search: max_results cap | `TestSearch_MaxResults` | OK |
| 5.9.4 | search: absolute paths | `TestSearch_AbsolutePaths` | OK |
| 5.10.1 | info: size, times, type, read-only | `TestInfo_CreationTimeAndReadOnly` | OK |
| 5.11.1 | run: execute via os/exec | `TestRun_SimpleCommand` | OK |
| 5.11.2 | run: argument splitting | `TestRun_ArgumentSplitting` | OK |
| 5.11.3 | run: no backslash escapes | — | N/A |
| 5.11.4 | run: inactivity timeout | `TestRun_InactivityTimeout` | OK |
| 5.11.5 | run: total timeout | `TestRun_TotalTimeout` | OK |
| 5.11.6 | run: output truncation | `TestRun_OutputTruncation` | PARTIAL |
| 5.11.7 | run: path confinement on command | `TestRun_CommandConfinement` | OK |
| 5.11.8 | run: timeout parameter | `TestRun_TimeoutParameter` | OK |
| 5.12.1 | cat: multiple files | `TestCat_MultipleFiles` | OK |
| 5.12.2 | cat: error on missing file | `TestCat_MissingFile` | OK |
| 5.12.3 | cat: binary/UTF-16 checks per file | — | N/A |
| 5.12.4 | cat: path confinement per file | — | N/A |
| 5.12.5 | cat: 512 KB combined limit | `TestCat_MaxOutputExceeded` | OK |
| 5.13.1 | mkdir: create with parents | `TestMkdir_HappyPath` | OK |
| 5.13.2 | mkdir: path confinement | `TestMkdir_OutsideRoots` | OK |
| 5.13.3 | mkdir: idempotent | — | N/A |
| 5.14.1 | head: first N lines | `TestHead_HappyPath` | OK |
| 5.14.2 | head: default 10 | — | N/A |
| 5.14.3 | head: binary/UTF-16 checks | — | N/A |
| 5.14.4 | head: file not found | `TestHead_FileNotFound` | OK |
| 5.15.1 | tail: last N lines | `TestTail_HappyPath` | OK |
| 5.15.2 | tail: default 10 | — | N/A |
| 5.15.3 | tail: binary/UTF-16 checks | `TestTail_BinaryRefused` | OK |
| 5.15.4 | tail: large file seek | `TestTail_LargeFile` | OK |
| 5.16.1 | diff: unified output | `TestDiff_DifferentFiles` | OK |
| 5.16.2 | diff: empty for identical | `TestDiff_IdenticalFiles` | OK |
| 5.16.3 | diff: binary/UTF-16 checks | — | N/A |
| 5.16.4 | diff: context_lines parameter | `TestDiff_ContextLines` | OK |
| 5.17.1 | wc: lines, words, bytes | `TestWc_HappyPath` | OK |
| 5.17.2 | wc: binary/UTF-16 checks | — | N/A |
| 5.17.3 | wc: file not found | `TestWc_FileNotFound` | OK |
| 5.18.1 | tree: recursive indented listing | `TestTree_HappyPath` | OK |
| 5.18.2 | tree: depth limit | `TestTree_DepthLimit` | OK |
| 5.18.3 | tree: empty dir | `TestTree_EmptyDir` | OK |
| 5.18.4 | tree: 500 entry cap | — | N/A |
| 5.18.5 | tree: pattern filter | `TestTree_PatternFilter` | OK |
| 5.19.1 | roots: return allowed_roots | `TestRoots_ReturnsAllowedRoots` | OK |
| 5.20.1 | grep: single file match | `TestGrep_SingleFileMatch` | OK |
| 5.20.2 | grep: recursive directory search | `TestGrep_RecursiveSearch` | OK |
| 5.20.3 | grep: ignore_case flag | `TestGrep_IgnoreCase` | OK |
| 5.20.4 | grep: include glob filter | `TestGrep_RecursiveSearch` | OK |
| 5.20.5 | grep: context lines | `TestGrep_ContextLines` | OK |
| 5.20.6 | grep: max_results cap | `TestGrep_MaxResults` | OK |
| 5.20.7 | grep: binary file skipped silently | `TestGrep_BinaryFileSkipped` | OK |
| 5.20.8 | grep: UTF-16 file decoded before matching | `TestGrep_UTF16FileDecoded` | OK |
| 5.20.9 | grep: CRLF stripped before matching | `TestGrep_CRLFStripped` | OK |
| 5.20.10 | grep: path confinement | `TestGrep_PathConfinement` | OK |
| 5.20.11 | grep: fallback registration when external exe missing | `TestGrep_FallbackRegistration` | OK |
| 5.20.12 | grep: external exe forbidden, built-in always wins | `TestGrep_ExternalForbidden` | OK |

### §6 Configured External Tools

| Req | Description | Test | Status |
|-----|-------------|------|--------|
| 6.1.1 | Config-driven declaration with param schemas | `TestConfiguredTool_GrepIntegration` | OK |
| 6.1.2 | Flag parameters (boolean, string, integer) | `TestConfiguredTool_GrepIntegration` | OK |
| 6.1.3 | Positional parameters in order | `TestConfiguredTool_GrepIntegration` | OK |
| 6.1.4 | Reject param with both flag and position | `TestConfig_ParamValidation` | OK |
| 6.1.5 | Reject param with neither flag nor position | `TestConfig_ParamValidation` | OK |
| 6.2 | No shell execution | — | N/A |
| 6.3.1 | Inactivity timeout with timer reset on stdout+stderr | `TestRun_ConcurrentPipeDraining` | OK |
| 6.3.2 | Text result on inactivity timeout | `TestRun_TimeoutIsTextResult` | OK |
| 6.3.3 | Total timeout via context.WithTimeout | `TestRun_TotalTimeout` | OK |
| 6.3.4 | Total timeout not exposed to Claude | `TestToolsList_NoTotalTimeout` | OK |
| 6.3.5 | Diagnostic message format | `TestRun_TimeoutIsTextResult` | OK |
| 6.4 | Exit code handling per success_codes | `TestConfiguredTool_GrepIntegration` | OK |
| 6.5 | Output size limit: truncate, kill | `TestRun_OutputTruncation` | OK |
| 6.5.1 | Stderr buffer capped at MaxOutput, pipe still drained | `TestRun_StderrTruncated`, `TestRun_StderrUnderLimit` | OK |
| 6.6.1 | Inject timeout param into schemas | `TestToolsList_TimeoutParam` | OK |
| 6.6.2 | Clamp timeout to [1, max_timeout] | `TestClampTimeout` | OK |
| 6.6.3 | Strip timeout from params before dispatch | `TestRun_TimeoutStrippedFromParams` | OK |

### §8 Security

| Req | Description | Test | Status |
|-----|-------------|------|--------|
| 8.1.1 | Pre-check: filepath.Abs + filepath.Clean | `TestRead_PathOutsideRoots` | OK |
| 8.1.2 | Post-check: GetFinalPathNameByHandle | `TestSecurity_JunctionEscape` | OK |
| 8.1.3 | Symlink/junction escape detected | `TestSecurity_JunctionEscape` | OK |
| 8.1.4 | run command: absolute path post-check | `TestRun_CommandConfinement` | OK |
| 8.1.5 | run command: unqualified via LookPath | `TestRun_CommandConfinement` | OK |
| 8.1.6 | Confinement error includes allowed roots | `TestCriticalError_ConfinementFormat` | OK |
| 8.2 | No shell execution | — | N/A |
| 8.3 | No recursive delete | `TestDelete_NonEmptyDir` | OK |
| 8.4 | No binary file operations | `TestRead_BinaryFile` | OK |

### §9 Error Handling, Process Lifecycle

| Req | Description | Test | Status |
|-----|-------------|------|--------|
| 9.1.1 | Errors wrapped with context | — | N/A |
| 9.1.2 | Never exit on tool error | `TestErrorContinuation` | OK |
| 9.2 | File locking: retry 3x with backoff | `TestRead_FileLocked` | OK |
| 9.3 | Atomic writes for modifying ops | — | ACCEPT |
| 9.4 | Long paths (> 260 chars) | `TestLongPaths` | OK |
| 9.5 | Concurrent pipe draining | `TestRun_ConcurrentPipeDraining` | OK |
| 9.6.1 | strpatch: stdin pipe with JSON | `TestEdit_HappyPath` | OK |
| 9.6.2 | Other children: stdin = DevNull | `TestRun_ChildStdinEOF` | OK |
| 9.7 | Sequential request processing | — | N/A |
| 9.8.1 | Job Object with KILL_ON_JOB_CLOSE | `TestRun_JobObjectGrandchildKilled` | OK |
| 9.8.2 | WER dialog suppression | `TestRun_WERSuppression` | OK |
| 9.8.3 | Kill sequence: Kill, drain, close job, Wait | `TestRun_HandleLeak` | OK |
| 9.8.4 | Handle leak prevention via defer | `TestRun_HandleLeak` | OK |
| 9.9.1 | Inactivity timeout kills child | `TestRun_InactivityTimeout` | OK |
| 9.9.2 | Total timeout kills child | `TestRun_TotalTimeout` | OK |
| 9.9.3 | Output size limit kills child | `TestRun_OutputTruncation` | PARTIAL |
| 9.10 | TerminateProcess unconditional on Windows | — | N/A |
| 9.11 | Panic recovery | `TestPanicRecovery` | OK |
| 9.12.1 | Shutdown on stdin EOF | `TestShutdown_StdinEOF` | OK |
| 9.12.2 | Shutdown on broken stdout pipe | `TestShutdown_BrokenStdoutPipe` | OK |
| 9.12.3 | Kill running child during shutdown | `TestShutdown_WhileChildRunning` | OK |
| 9.12.4 | Flush and close log on shutdown | `TestDiag_LogFile` | OK |
| 9.13.1 | Missing config: built-in tools only | `TestConfig_MissingConfigStartsWithBuiltins` | OK |
| 9.13.2 | Malformed config: refuse to start | `TestConfig_MalformedConfigRefusesStart` | OK |
| 9.13.3 | UTF-16 config: detect BOM, refuse | `TestConfig_UTF16ConfigRefusesStart` | OK |
| 9.13.4 | Missing builtin_descriptions: refuse | `TestConfig_MissingBuiltinDescriptionsRefusesStart` | OK |
| 9.15.1 | Critical errors classified (confinement, panic, config) | `TestCriticalError_ConfinementFormat` | OK |
| 9.15.2 | Critical tool response: isError + formatted text with 🛑 prefix | `TestCriticalError_ResponseFormat` | OK |
| 9.15.3 | Supplementary notifications/message at error level | `TestCriticalError_Notification` | OK |
| 9.15.4 | criticalErrorText function wraps message | `TestCriticalErrorText_Format` | OK |

### §10 Diagnostics

| Req | Description | Test | Status |
|-----|-------------|------|--------|
| 10.1.1 | --verbose writes to stderr | `TestDiag_Verbose` | OK |
| 10.1.2 | [shim:spawn] log tag | `TestDiag_Verbose` | OK |
| 10.2 | --log writes timestamped file | `TestDiag_LogFile` | OK |
| 10.3 | --scan lists exes and exits | `TestDiag_Scan` | OK |

### §11 Configuration

| Req | Description | Test | Status |
|-----|-------------|------|--------|
| 11.1.1 | allowed_roots parsed from TOML | `TestRead_PathOutsideRoots` | OK |
| 11.1.2 | max_timeout configurable | `TestConfig_MaxTimeoutClamp` | OK |
| 11.2 | Every built-in needs description | `TestConfig_MissingBuiltinDescriptionsRefusesStart` | OK |
| 11.3 | --verbose, --log, --scan flags | multiple diag tests | OK |

### §12 Build

| Req | Description | Test | Status |
|-----|-------------|------|--------|
| 12.4 | golang.org/x/sys dependency | — | N/A |

### §14 Spec-prescribed tests

| Req | Description | Test | Status |
|-----|-------------|------|--------|
| 14.2.1 | File locked by another process | `TestRead_FileLocked` | OK |
| 14.2.2 | Atomic write survives kill | — | ACCEPT |
| 14.2.3 | Long paths > 260 chars | `TestLongPaths` | OK |
| 14.2.4 | Symlink/junction escape | `TestSecurity_JunctionEscape` | OK |
| 14.2.5 | read offset/limit partial read | `TestRead_OffsetLimit` | OK |
| 14.3.1 | Timeout param respected | `TestRun_TimeoutParameter`, `TestConfig_MaxTimeoutClamp` | OK |
| 14.3.2 | Timeout param stripped | `TestRun_TimeoutStrippedFromParams` | OK |
| 14.3.3 | Args with special chars | `TestRun_SpecialCharsNoInjection` | OK |
| 14.4.1 | Configured tool: grep matches | — | N/A |
| 14.4.2 | Configured tool: grep no matches | — | N/A |
| 14.4.3 | Configured tool: grep invalid regex | — | N/A |
| 14.4.4 | Timeout param in tools/list | `TestToolsList_TimeoutParam` | OK |
| 14.4.5 | Timeout param override | `TestConfig_MaxTimeoutClamp` | OK |
| 14.5.1 | WER suppression | `TestRun_WERSuppression` | OK |
| 14.5.2 | Job Object grandchild killed | `TestRun_JobObjectGrandchildKilled` | OK |
| 14.5.3 | Handle leak: 200 sequential calls | `TestRun_HandleLeak` | OK |
| 14.5.4 | Pipe deadlock: 100 KB each stream | `TestRun_ConcurrentPipeDraining` | OK |
| 14.5.5 | Child stdin: receives EOF | `TestRun_ChildStdinEOF` | OK |
| 14.5.6 | Concurrent pipe draining: separate buffers | `TestRun_ConcurrentPipeDraining` | OK |
| 14.6.1 | Panic in handler: error response, continue | `TestPanicRecovery` | OK |
| 14.6.2 | Panic recovery logs error | `TestPanicRecovery` | OK |
| 14.7.1 | Shutdown on stdin close | `TestShutdown_StdinEOF` | OK |
| 14.7.2 | Shutdown while child running | `TestShutdown_WhileChildRunning` | OK |
| 14.7.3 | Broken stdout pipe | `TestShutdown_BrokenStdoutPipe` | OK |
| 14.7.4 | Log file flushed on shutdown | `TestDiag_LogFile` | OK |
| 14.8.1 | UTF-16LE decode | `TestRead_UTF16LEDecode` | OK |
| 14.8.2 | UTF-16BE decode | `TestRead_UTF16BEDecode` | OK |
| 14.8.3 | UTF-16 across read-only tools (head, tail, cat, wc, diff) | `TestHead_UTF16`, `TestTail_UTF16`, `TestCat_UTF16`, `TestWc_UTF16`, `TestDiff_UTF16` | OK |
| 14.8.4 | Write after UTF-16 read produces UTF-8 | `TestWrite_AfterUTF16Read` | OK |
| 14.8.5 | Edit refuses UTF-16 file | `TestEdit_RefusesUTF16` | OK |
| 14.9.1 | grep: single file match | `TestGrep_SingleFileMatch` | OK |
| 14.9.2 | grep: no match | `TestGrep_NoMatchesAnywhere` | OK |
| 14.9.3 | grep: recursive with include | `TestGrep_RecursiveSearch` | OK |
| 14.9.4 | grep: ignore_case | `TestGrep_IgnoreCase` | OK |
| 14.9.5 | grep: context lines | `TestGrep_ContextLines` | OK |
| 14.9.6 | grep: max_results cap | `TestGrep_MaxResults` | OK |
| 14.9.7 | grep: binary file skipped | `TestGrep_BinaryFileSkipped` | OK |
| 14.9.8 | grep: UTF-16 file decoded | `TestGrep_UTF16FileDecoded` | OK |
| 14.9.9 | grep: CRLF stripped for matching | `TestGrep_CRLFStripped` | OK |
| 14.9.10 | grep: path confinement | `TestGrep_PathConfinement` | OK |
| 14.9.11 | grep: fallback registration | `TestGrep_FallbackRegistration` | OK |
| 14.9.12 | grep: external forbidden | `TestGrep_ExternalForbidden` | OK |
| 14.9.13 | grep: invalid regex | `TestGrep_InvalidRegex` | OK |
| 14.10.1 | Stderr > MaxOutput: truncated, child alive, stdout intact | `TestRun_StderrTruncated`, `TestRun_BothTruncated` | OK |
| 14.10.2 | Stderr < MaxOutput: fully captured | `TestRun_StderrUnderLimit` | OK |
| 14.11.1 | Confinement error includes allowed roots | `TestCriticalError_ConfinementFormat` | OK |

### Summary

| Status | Count |
|--------|-------|
| OK | 179 |
| N/A | 21 |
| ACCEPT | 4 |
| PARTIAL | 2 |
| MISS | 0 |
| **Total** | **206** |

All 10 mandatory items (Appendix A.1) are implemented and tested.
All MISS items resolved: UTF-16 decode, built-in grep, confinement
error messages, and stderr cap are implemented and tested.
