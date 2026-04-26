# WinMcpShim Installer Specification

Version: 1.0.0
Date: 2026-04-02
Status: Draft

This document specifies the installer and uninstaller for
WinMcpShim. When the implementation diverges from this document,
the spec is correct and the code is a bug.

---

## 1. Purpose

`install.exe` configures WinMcpShim on a target machine.
`uninstall.exe` reverses the configuration. Both are compiled Go
binaries with no runtime dependencies. Both discover the
environment automatically, make no assumptions, validate all
inputs, and either complete fully or roll back to the prior state.

---

## 2. Deliverables

| File             | Source                  | Purpose                               |
|------------------|-------------------------|---------------------------------------|
| `install.exe`    | `cmd/install/main.go`   | Interactive installer                 |
| `uninstall.exe`  | `cmd/uninstall/main.go` | Interactive uninstaller               |

Both import from `installer/`, a new package containing all
shared logic. `mage build` builds both alongside
`winmcpshim.exe` and `strpatch.exe`.

Note: the main shim binary is renamed from `shim.exe` to
`winmcpshim.exe`. The `mage build` target uses
`go build -o winmcpshim.exe ./shim` for the shim build step.
The source directory remains `shim/`. The config file remains
`shim.toml` (the binary locates it by this name via
`shared.ConfigPath()`).

No wrapper scripts. The user double-clicks the `.exe`.

### 2.1 Dependencies

No new module dependencies. The installer uses only:

- `encoding/json` (stdlib) — Claude Desktop config
- `github.com/BurntSushi/toml` (already in go.mod) — validation
- `golang.org/x/sys/windows/registry` (already in go.mod via
  `golang.org/x/sys`) — Git discovery
- `os`, `os/exec`, `path/filepath`, `fmt`, `bufio`, `strings`,
  `regexp`, `time` (stdlib)

### 2.2 Code structure

```
installer/
    types.go            — CheckResult, Plan, UndoStack, constants
    types_test.go
    discover.go         — environment discovery functions
    discover_test.go    — tests for §5.1, §5.2, §5.3
    toml.go             — shim.toml text manipulation
    toml_test.go        — tests for §5.4
    claude_config.go    — Claude Desktop JSON manipulation
    claude_config_test.go — tests for §5.6
    atomic.go           — atomic file write, backup
    atomic_test.go      — tests for INV-02
    rollback.go         — undo stack
    rollback_test.go    — tests for §5.8
    process.go          — Claude process detection and termination
    process_test.go     — tests for INS-08
cmd/
    install/
        main.go         — Pass 1/2/3 orchestration, interactive prompts
    uninstall/
        main.go         — uninstall orchestration
```

Functions in the `installer` package are non-interactive: they
accept parameters and return results. All user interaction
(prompts, confirmations, coloured output) lives in `main.go`.
This separation makes the package fully testable via `go test`
with no mocking of stdin/stdout.

---

## 3. Execution model — install

The installer runs in three sequential passes.

### 3.1 Pass 1 — Discover

Read-only. Examines the machine, builds a list of `CheckResult`
values. No side effects. No user interaction.

If any check has status FAIL, the installer prints all results
and exits with code 1. This gives the user the complete picture
rather than failing on the first problem.

### 3.2 Pass 2 — Interact

Collects user input required by the plan:

- Allowed roots (only if shim.toml is missing or unconfigured).
- Log directory (offer default, accept override).
- Git installation (only if not found — offer download, re-probe).
- Claude process kill (only if processes detected).

Displays the complete plan. Gets a single Y/N confirmation.
If N, exits with code 2.

### 3.3 Pass 3 — Execute

Carries out the plan. Maintains an undo stack. Each step pushes
its reverse operation before executing the forward operation.
On any failure, rolls back all changes in reverse order, then
exits with code 3. Concludes with a verification step
(`winmcpshim.exe --scan`).

---

## 4. Execution model — uninstall

The uninstaller is simpler: discover, confirm, execute.

### 4.1 Discover

- Locate `claude_desktop_config.json`.
- Parse it; check whether `mcpServers.WinMcpShim` exists.
- Detect running Claude processes.

### 4.2 Confirm

Display what will be removed. Single Y/N.

### 4.3 Execute

- Kill Claude processes (if running, with user consent).
- Back up `claude_desktop_config.json`.
- Remove the `WinMcpShim` entry from `mcpServers`.
- Write the modified config via atomic rename.
- Report what was done. Remind user to restart Claude Desktop.

The uninstaller does NOT delete `winmcpshim.exe`, `shim.toml`, or log
files. It only disconnects WinMcpShim from Claude Desktop.

---

## 5. Global invariants

| ID      | Invariant                                                          |
|---------|--------------------------------------------------------------------|
| INV-01  | Neither tool modifies anything during its discovery pass           |
| INV-02  | All file writes use atomic pattern: write `.tmp`, rename to target |
| INV-03  | All paths with spaces and Unicode are handled correctly            |
| INV-04  | Each step in Pass 3 pushes its reverse onto the undo stack before executing |
| INV-05  | On any unhandled error in Pass 3, the undo stack executes in reverse |
| INV-06  | Both tools are idempotent — each step checks current state before acting |

---

## 6. Requirements — install

### §6.1 Environment checks (Pass 1)

| Req     | Description                                                | On failure |
|---------|------------------------------------------------------------|------------|
| INS-01  | OS must be Windows 10 build >= 10240                       | FAIL       |
| INS-02  | `winmcpshim.exe` must exist in installer directory               | FAIL       |
| INS-03  | `strpatch.exe` must exist in installer directory           | FAIL       |
| INS-04  | `shim.toml.example` must exist in installer directory      | FAIL       |

The installer directory is the directory containing `install.exe`,
determined via `os.Executable()` + `filepath.Dir()`.

### §6.2 Git for Windows (Pass 1 + Pass 2)

| Req      | Description                                               | On failure |
|----------|-----------------------------------------------------------|------------|
| INS-05   | Git for Windows must be located                           | Proceed to INS-05a |
| INS-05a  | If not found: display download URL, offer to open browser. After user reports installation complete, re-run discovery. User declines -> FAIL | FAIL |
| INS-05b  | All 8 required executables must be present in `usr\bin`: `grep.exe`, `gawk.exe`, `sed.exe`, `sort.exe`, `xxd.exe`, `dos2unix.exe`, `cut.exe`, `uniq.exe` | FAIL (partial Git) |

**Git discovery order** (stop at first hit):

1. Registry `HKLM\SOFTWARE\GitForWindows` -> `InstallPath`.
2. `where.exe grep` on PATH — if found, derive Git root as
   grandparent of the exe's directory.
3. Common paths in order:
   - `%ProgramFiles%\Git`
   - `%ProgramFiles(x86)%\Git`
   - `%LOCALAPPDATA%\Programs\Git`
   - `C:\Git`

Registry first because it is definitive and immune to PATH
pollution. `where.exe` second because a grep on PATH might
not belong to Git (e.g. GnuWin32, MSYS2, Cygwin). Common
paths last as a fallback.

Verification: `$gitRoot\usr\bin\<exe>` must exist for all 8
executables. A partial Git installation is treated as not found.

### §6.3 Claude Desktop (Pass 1 + Pass 2)

| Req      | Description                                               | On failure |
|----------|-----------------------------------------------------------|------------|
| INS-06   | `%APPDATA%\Claude` must exist (Claude Desktop installed)  | FAIL       |
| INS-07   | Detect all running Claude processes via `CreateToolhelp32Snapshot`. Match executable name `claude.exe` exactly (case-insensitive). All Electron sub-processes use this same name | Proceed to INS-07a |
| INS-07a  | If processes found: list each PID. Warn that closing the Claude Desktop window is NOT sufficient — all background processes must be terminated (they are all named "Claude"). Offer to kill all. User declines -> FAIL | FAIL |
| INS-07b  | After kill: wait 2 seconds, re-enumerate. If any `claude.exe` survive -> FAIL | FAIL |
| INS-07c  | After Claude processes are gone: wait up to 5 seconds for `winmcpshim.exe` to also exit. It terminates naturally when its stdin pipe closes. If it survives -> WARN (not fatal, but shim.toml may be briefly locked) | WARN |

Process enumeration uses `windows.CreateToolhelp32Snapshot` +
`Process32First`/`Process32Next` from `golang.org/x/sys/windows`.
This avoids shelling out to `tasklist.exe` or PowerShell.

Process termination uses `windows.OpenProcess` +
`windows.TerminateProcess`. Only `claude.exe` processes are
terminated directly. Child processes — `conhost.exe` (Windows
system process) and `winmcpshim.exe` (the shim) — are NOT
killed directly. They terminate naturally when their parent
Claude processes exit:

- `conhost.exe` — cleaned up by Windows.
- `winmcpshim.exe` — exits when its stdin pipe closes (Claude
  held the write end). The installer waits for this to avoid
  a race condition where shim.toml is still open.

### §6.4 shim.toml configuration (Pass 1 + Pass 2 + Pass 3)

| Req      | Description                                               |
|----------|-----------------------------------------------------------|
| INS-08   | If `shim.toml` does not exist: create from `shim.toml.example` |
| INS-08a  | If `shim.toml` exists and contains the literal string `CHANGE_ME`: prompt for allowed roots |
| INS-08b  | If `shim.toml` exists and does NOT contain `CHANGE_ME`: skip root prompting (already configured) |
| INS-09   | Every allowed-root path entered by user must be validated: trimmed, trailing backslash removed, resolved to absolute, confirmed to exist as a directory |
| INS-09a  | Invalid paths are reported individually and excluded. After filtering, if zero valid paths remain, re-prompt. At least one valid root required |
| INS-09b  | Duplicate paths (case-insensitive, after normalisation) are silently deduplicated |
| INS-10   | The `allowed_roots` block in shim.toml is replaced with validated paths formatted as a TOML string array with double-backslash escaping |
| INS-11   | All `exe = "..."` values in `[tools.*]` sections are updated to use the discovered Git `usr\bin` path |
| INS-12   | The `[scan_dirs]` `paths` array is updated to use the discovered Git `usr\bin` path |
| INS-13   | `C:\Windows\System32\tar.exe` is verified to exist. If absent -> WARN (tar tool unavailable, not fatal) |

**TOML handling strategy:** The toml file is read and modified as
text (string replacement), not via the TOML library. This
preserves comments, blank lines, and formatting. After
modification, the result is validated by parsing with
`BurntSushi/toml` to confirm it is still syntactically correct.
If validation fails, the modification is a bug.

The text operations are:

- `SetAllowedRoots(content string, roots []string) string` —
  replaces everything from the line matching
  `allowed_roots = [` through the next `]` (inclusive) with a
  freshly formatted block.

- `SetGitPaths(content string, gitUsrBin string) string` —
  replaces every occurrence of `C:\\Program Files\\Git\\usr\\bin`
  (the template default) with the escaped discovered path.
  Also replaces the unescaped form in `scan_dirs`.

Both are pure functions: string in, string out. The caller
handles file I/O.

### §6.5 Log directory (Pass 2 + Pass 3)

| Req      | Description                                               |
|----------|-----------------------------------------------------------|
| INS-14   | Offer default log directory `%USERPROFILE%\logs\shim`. User may override |
| INS-14a  | Entered path is validated: resolved to absolute, parent must exist or be creatable |
| INS-15   | Create the log directory if it does not exist              |
| INS-15a  | Verify writeability: create a temp file, write to it, delete it |

### §6.6 Claude Desktop configuration (Pass 1 + Pass 2 + Pass 3)

| Req      | Description                                               |
|----------|-----------------------------------------------------------|
| INS-16   | Before any modification: back up `claude_desktop_config.json` to `.bak.<timestamp>` where timestamp is `20060102T150405` (Go reference time format) |
| INS-17   | Parse `claude_desktop_config.json` via `encoding/json`    |
| INS-17a  | If JSON is malformed: report exact error. Offer to back up the corrupt file and create fresh config. User declines -> FAIL |
| INS-18   | If `mcpServers` property does not exist: add it            |
| INS-19   | If `mcpServers.WinMcpShim` does not exist: add it with `command` = `<shimDir>\winmcpshim.exe` and `args` = `["--log", "<logDir>"]` |
| INS-19a  | If `mcpServers.WinMcpShim` exists with correct `command` path: skip |
| INS-19b  | If `mcpServers.WinMcpShim` exists with different `command` path: report both, offer to update. User declines -> keep existing |
| INS-20   | All other properties in the config (including other `mcpServers` entries) must be preserved |
| INS-21   | Write modified config via atomic tmp+rename                |
| INS-22   | If `claude_desktop_config.json` does not exist: create it with just the `mcpServers.WinMcpShim` entry |

**JSON handling strategy:** `encoding/json` with
`json.RawMessage` is not used. The config is small and fully
understood; `map[string]interface{}` via `json.Unmarshal` /
`json.MarshalIndent` is adequate. Formatting changes in the
output JSON (vs the original) are acceptable — the file is
machine-managed.

`UpdateClaudeConfig` is a pure function:
`func(cfg map[string]interface{}, shimExe string, logDir string) (map[string]interface{}, Action)`
where `Action` is one of `Added`, `Updated`, `Skipped`.
The caller handles file I/O and backup.

### §6.7 Verification (Pass 3)

| Req      | Description                                               |
|----------|-----------------------------------------------------------|
| INS-23   | Run `winmcpshim.exe --scan` after all configuration complete     |
| INS-23a  | Exit code 0 -> verification passed                        |
| INS-23b  | Non-zero exit code -> capture stderr, roll back, FAIL      |

### §6.8 Rollback (Pass 3)

| Req      | Description                                               |
|----------|-----------------------------------------------------------|
| INS-24   | On any failure during Pass 3: execute undo stack in reverse order |
| INS-24a  | Each undo action is wrapped in error recovery — a failed undo is logged but does not prevent subsequent undos |
| INS-24b  | After rollback: report what was undone                     |
| INS-24c  | After rollback: system is in the same state as before Pass 3 |

### §6.9 Completion (Pass 3)

| Req      | Description                                               |
|----------|-----------------------------------------------------------|
| INS-25   | On success: display summary (shim path, config path, log dir, number of roots) |
| INS-26   | On success: remind user to restart Claude Desktop          |

---

## 7. Requirements — uninstall

| Req      | Description                                               | On failure |
|----------|-----------------------------------------------------------|------------|
| UNS-01   | Locate `claude_desktop_config.json` at `%APPDATA%\Claude`. If absent -> nothing to do, exit 0 | — |
| UNS-02   | Parse JSON. If malformed -> report error, FAIL             | FAIL |
| UNS-03   | If `mcpServers.WinMcpShim` does not exist -> nothing to do, exit 0 | — |
| UNS-04   | Detect running Claude processes (same as INS-07)           | Proceed to UNS-04a |
| UNS-04a  | If processes found: warn and offer to kill (same as INS-07a). User declines -> FAIL | FAIL |
| UNS-04b  | After kill: wait for `winmcpshim.exe` to also exit (same as INS-07c) | WARN |
| UNS-05   | Back up config to `.bak.<timestamp>` before modification   | FAIL |
| UNS-06   | Remove `mcpServers.WinMcpShim` from config. Preserve all other entries | — |
| UNS-06a  | If `mcpServers` becomes empty after removal: remove the `mcpServers` key entirely | — |
| UNS-07   | Write modified config via atomic tmp+rename                | FAIL (restore from backup) |
| UNS-08   | Display summary: what was removed, what was NOT removed (winmcpshim.exe, shim.toml, logs) | — |
| UNS-09   | Remind user to restart Claude Desktop                      | — |

---

## 8. Exit codes

### Install

| Code | Meaning                                        |
|------|------------------------------------------------|
| 0    | Success                                        |
| 1    | Prerequisites unmet (discovery failure)        |
| 2    | User aborted                                   |
| 3    | Execution failure (rolled back)                |

### Uninstall

| Code | Meaning                                        |
|------|------------------------------------------------|
| 0    | Success (including "nothing to do")            |
| 1    | Config parse error or write failure            |
| 2    | User aborted                                   |

---

## 9. Types

```go
// CheckStatus is the result of a single discovery check.
type CheckStatus int

const (
    StatusOK   CheckStatus = iota
    StatusWarn
    StatusFail
)

// CheckResult holds the outcome of one discovery check.
type CheckResult struct {
    Req    string      // requirement ID, e.g. "INS-01"
    Name   string      // human-readable check name
    Status CheckStatus
    Detail string      // explanation (always populated)
}

// ConfigAction describes what UpdateClaudeConfig decided.
type ConfigAction int

const (
    ActionAdded   ConfigAction = iota
    ActionUpdated
    ActionSkipped
)

// Plan holds everything discovered in Pass 1 plus
// inputs collected in Pass 2.
type Plan struct {
    ShimDir       string        // directory containing install.exe
    ShimExe       string        // full path to winmcpshim.exe
    GitRoot       string        // Git for Windows root (empty if not found)
    GitUsrBin     string        // gitRoot\usr\bin
    ClaudeDir     string        // %APPDATA%\Claude
    ConfigPath    string        // claude_desktop_config.json full path
    TomlPath      string        // shim.toml full path
    TomlState     TomlState     // Missing, Unconfigured, Configured
    ConfigExists  bool
    ConfigAction  ConfigAction  // what needs doing to Claude config
    AllowedRoots  []string      // validated paths (empty if not needed)
    LogDir        string        // validated log directory path
    TarAvailable  bool
    Checks        []CheckResult
}

// TomlState classifies the state of shim.toml.
type TomlState int

const (
    TomlMissing      TomlState = iota
    TomlUnconfigured           // exists, contains CHANGE_ME
    TomlConfigured             // exists, no CHANGE_ME
)
```

---

## 10. Function inventory

All functions in the `installer` package. Interactive functions
(stdin/stdout) are in `cmd/install/main.go` and
`cmd/uninstall/main.go`, not in the package.

### 10.1 discover.go — environment discovery

| Function            | Signature                                             | Pure | Req |
|---------------------|-------------------------------------------------------|------|-----|
| `CheckWindowsVersion` | `(build int) CheckResult`                           | Yes  | INS-01 |
| `CheckShimFiles`    | `(dir string) []CheckResult`                          | No   | INS-02, INS-03, INS-04 |
| `FindGitForWindows` | `() (string, error)`                                  | No   | INS-05 |
| `CheckGitTools`     | `(gitRoot string) ([]string, []string)`               | No   | INS-05b |
| `CheckClaudeDesktop` | `(appData string) CheckResult`                       | No   | INS-06 |
| `CheckTarExe`       | `() CheckResult`                                      | No   | INS-13 |

`CheckGitTools` returns `(present, missing)` — two slices of
executable names.

### 10.2 toml.go — shim.toml text manipulation

| Function            | Signature                                             | Pure | Req |
|---------------------|-------------------------------------------------------|------|-----|
| `GetTomlState`      | `(path string) TomlState`                             | No   | INS-08, INS-08a, INS-08b |
| `SetAllowedRoots`   | `(content string, roots []string) (string, error)`    | Yes  | INS-10 |
| `SetGitPaths`       | `(content string, gitUsrBin string) (string, error)`  | Yes  | INS-11, INS-12 |
| `ValidateToml`      | `(content string) error`                              | Yes  | §6.4 |
| `ValidateRoot`      | `(path string) (string, error)`                       | No   | INS-09 |
| `ValidateRoots`     | `(paths []string) ([]string, []string)`               | No   | INS-09, INS-09a, INS-09b |
| `FormatTomlRoots`   | `(roots []string) string`                             | Yes  | INS-10 |

`ValidateRoot` returns `(normalised, error)`. Normalisation:
trim whitespace, remove trailing backslash, resolve to absolute,
confirm directory exists.

`ValidateRoots` returns `(valid, rejected)` with deduplication.

`FormatTomlRoots` produces the TOML array text:
```
[
    "D:\\projects",
    "C:\\Users\\Momo\\Documents",
]
```

### 10.3 claude_config.go — Claude Desktop JSON

| Function            | Signature                                             | Pure | Req |
|---------------------|-------------------------------------------------------|------|-----|
| `ReadClaudeConfig`  | `(path string) (map[string]interface{}, error)`       | No   | INS-17, INS-17a |
| `UpdateClaudeConfig` | `(cfg map[string]interface{}, shimExe string, logDir string) (map[string]interface{}, ConfigAction)` | Yes | INS-18..INS-20 |
| `RemoveShimEntry`   | `(cfg map[string]interface{}) (map[string]interface{}, bool)` | Yes | UNS-06, UNS-06a |
| `NewClaudeConfig`   | `(shimExe string, logDir string) map[string]interface{}` | Yes | INS-22 |
| `MarshalConfig`     | `(cfg map[string]interface{}) ([]byte, error)`        | Yes  | INS-21 |

### 10.4 atomic.go — safe file operations

| Function            | Signature                                             | Pure | Req |
|---------------------|-------------------------------------------------------|------|-----|
| `WriteAtomic`       | `(path string, data []byte) error`                    | No   | INV-02 |
| `BackupFile`        | `(path string) (string, error)`                       | No   | INS-16, UNS-05 |

`BackupFile` returns the backup path. Timestamp suffix uses
`time.Now().Format("20060102T150405")`. If that path already
exists (sub-second re-run), appends `.1`, `.2`, etc.

### 10.5 rollback.go — undo stack

| Function            | Signature                                             | Pure | Req |
|---------------------|-------------------------------------------------------|------|-----|
| `UndoStack.Push`    | `(description string, fn func() error)`               | Yes  | INV-04 |
| `UndoStack.Execute` | `() []string`                                         | No   | INS-24..INS-24c |

`Execute` returns a log of what was undone (or what failed).
Each entry is one line of text for display.

### 10.6 process.go — Claude process management

| Function            | Signature                                             | Pure | Req |
|---------------------|-------------------------------------------------------|------|-----|
| `FindClaudeProcesses` | `() ([]ProcessInfo, error)`                         | No   | INS-07 |
| `FindShimProcesses` | `() ([]ProcessInfo, error)`                          | No   | INS-07c |
| `KillProcesses`     | `(pids []uint32) error`                               | No   | INS-07a |
| `WaitProcessesGone` | `(pids []uint32, timeout time.Duration) ([]uint32, error)` | No | INS-07b, INS-07c |

`ProcessInfo` is `{ PID uint32, Name string }`.

`FindClaudeProcesses` uses `CreateToolhelp32Snapshot` +
`Process32First`/`Process32Next`. Matches process names where
`strings.EqualFold(name, "claude.exe")`. All Electron
sub-processes (main, GPU, renderer, utility) use this
identical executable name.

`FindShimProcesses` uses the same enumeration. Matches
`strings.EqualFold(name, "winmcpshim.exe")`.

`WaitProcessesGone` polls at 500ms intervals up to `timeout`.
Returns any PIDs still alive.

---

## 11. Test plan

All tests use `go test`. Test files are co-located with source
(`_test.go` suffix). Tests requiring filesystem fixtures create
a temp directory in `t.TempDir()` (automatically cleaned up).

Tests requiring Windows process enumeration run only on Windows
(`//go:build windows`).

### 11.1 Conventions

- Test function names: `Test<Function>_<Scenario>`.
- Each test comment references the requirement(s) it verifies.
- No test depends on the user's machine state. All paths,
  configs, and toml files are created in temp directories.
- Pure functions are tested with table-driven tests.
- IO functions are tested with real temp files.

### 11.2 discover_test.go

| Test | Req | Description |
|------|-----|-------------|
| T-01 | INS-01 | `CheckWindowsVersion` returns FAIL for build 9600 (Windows 8.1) |
| T-02 | INS-01 | `CheckWindowsVersion` returns OK for build 10240 (Windows 10 RTM) |
| T-03 | INS-01 | `CheckWindowsVersion` returns OK for build 19045 (Windows 10 22H2) |
| T-04 | INS-02 | `CheckShimFiles` returns FAIL when `winmcpshim.exe` absent |
| T-05 | INS-03 | `CheckShimFiles` returns FAIL when `strpatch.exe` absent |
| T-06 | INS-04 | `CheckShimFiles` returns FAIL when `shim.toml.example` absent |
| T-07 | INS-02,03,04 | `CheckShimFiles` returns three OKs when all present |
| T-08 | INS-05b | `CheckGitTools` with all 8 exes present returns 8 present, 0 missing |
| T-09 | INS-05b | `CheckGitTools` with `gawk.exe` removed returns 7 present, 1 missing |
| T-10 | INS-05b | `CheckGitTools` with empty `usr\bin` returns 0 present, 8 missing |
| T-11 | INS-06 | `CheckClaudeDesktop` returns FAIL when dir does not exist |
| T-12 | INS-06 | `CheckClaudeDesktop` returns OK when dir exists |
| T-13 | INS-13 | `CheckTarExe` on a real Windows 10 machine returns OK |

### 11.3 toml_test.go

| Test | Req | Description |
|------|-----|-------------|
| T-20 | INS-08 | `GetTomlState` returns `TomlMissing` for non-existent path |
| T-21 | INS-08a | `GetTomlState` returns `TomlUnconfigured` when file contains `CHANGE_ME` |
| T-22 | INS-08b | `GetTomlState` returns `TomlConfigured` when file has real paths |
| T-23 | INS-09 | `ValidateRoot` accepts an existing directory |
| T-24 | INS-09 | `ValidateRoot` rejects a non-existent path |
| T-25 | INS-09 | `ValidateRoot` rejects a path that is a file |
| T-26 | INS-09 | `ValidateRoot` trims whitespace |
| T-27 | INS-09 | `ValidateRoot` removes trailing backslash |
| T-28 | INS-09 | `ValidateRoot` resolves a relative path to absolute |
| T-29 | INS-09b | `ValidateRoots` deduplicates case-insensitive paths |
| T-30 | INS-09b | `ValidateRoots` deduplicates paths differing only by trailing backslash |
| T-31 | INS-09a | `ValidateRoots` returns rejected paths separately |
| T-32 | INS-10 | `SetAllowedRoots` replaces `CHANGE_ME` block with single root |
| T-33 | INS-10 | `SetAllowedRoots` replaces `CHANGE_ME` block with multiple roots |
| T-34 | INS-10 | `SetAllowedRoots` on already-configured content replaces existing roots |
| T-35 | INS-10 | `SetAllowedRoots` output has correct TOML double-backslash escaping |
| T-36 | INS-10 | `SetAllowedRoots` preserves all content outside the `allowed_roots` block |
| T-37 | INS-11 | `SetGitPaths` replaces default Git path with discovered path |
| T-38 | INS-11 | `SetGitPaths` handles Git installed at path with spaces |
| T-39 | INS-11 | `SetGitPaths` handles Git installed on non-C drive |
| T-40 | INS-12 | `SetGitPaths` updates `scan_dirs` paths |
| T-41 | INS-11 | `SetGitPaths` does not modify `tar.exe` path (System32) |
| T-42 | INS-10 | `FormatTomlRoots` with one path produces correct TOML |
| T-43 | INS-10 | `FormatTomlRoots` with three paths produces correct TOML |
| T-44 | INS-10 | `FormatTomlRoots` escapes backslashes in paths |
| T-45 | §6.4 | `ValidateToml` accepts output of `SetAllowedRoots` applied to `shim.toml.example` |
| T-46 | §6.4 | `ValidateToml` rejects a string with unclosed bracket |
| T-47 | INS-11 | `SetGitPaths` + `ValidateToml` round-trip: modified toml is still parseable |
| T-48 | INS-10,11 | Full pipeline: copy shim.toml.example, apply SetAllowedRoots + SetGitPaths, validate with toml parser, verify values via `shared.LoadConfig` |

### 11.4 claude_config_test.go

| Test | Req | Description |
|------|-----|-------------|
| T-50 | INS-17 | `ReadClaudeConfig` parses valid JSON |
| T-51 | INS-17a | `ReadClaudeConfig` returns error with detail for malformed JSON |
| T-52 | INS-17a | `ReadClaudeConfig` returns error for JSON with trailing comma |
| T-53 | INS-22 | `ReadClaudeConfig` returns specific error for non-existent file (caller distinguishes from parse error) |
| T-54 | INS-18 | `UpdateClaudeConfig` adds `mcpServers` when absent |
| T-55 | INS-19 | `UpdateClaudeConfig` adds `WinMcpShim` when `mcpServers` exists but entry absent |
| T-56 | INS-19a | `UpdateClaudeConfig` returns `ActionSkipped` when entry exists with correct path |
| T-57 | INS-19b | `UpdateClaudeConfig` returns `ActionUpdated` when entry exists with different path |
| T-58 | INS-20 | `UpdateClaudeConfig` preserves other `mcpServers` entries |
| T-59 | INS-20 | `UpdateClaudeConfig` preserves non-mcpServers properties (`preferences`, etc.) |
| T-60 | INS-20 | `UpdateClaudeConfig` preserves nested object depth |
| T-61 | INS-22 | `NewClaudeConfig` produces valid JSON with correct structure |
| T-62 | INS-21 | `MarshalConfig` output round-trips through `json.Unmarshal` |
| T-63 | UNS-06 | `RemoveShimEntry` removes `WinMcpShim` and preserves other entries |
| T-64 | UNS-06a | `RemoveShimEntry` removes `mcpServers` key when it becomes empty |
| T-65 | UNS-06 | `RemoveShimEntry` returns false when entry not present |
| T-66 | UNS-03 | `RemoveShimEntry` on config with no `mcpServers` returns false |

### 11.5 atomic_test.go

| Test | Req | Description |
|------|-----|-------------|
| T-70 | INV-02 | `WriteAtomic` creates a new file |
| T-71 | INV-02 | `WriteAtomic` overwrites an existing file |
| T-72 | INV-02 | `WriteAtomic` does not leave `.tmp` file on success |
| T-73 | INV-02 | `WriteAtomic` original file is intact if `.tmp` write fails (read-only dir for temp) |
| T-74 | INV-03 | `WriteAtomic` handles path with spaces |
| T-75 | INS-16 | `BackupFile` creates timestamped copy |
| T-76 | INS-16 | `BackupFile` content matches original byte-for-byte |
| T-77 | INS-16 | `BackupFile` does not overwrite existing backup (appends suffix) |

### 11.6 rollback_test.go

| Test | Req | Description |
|------|-----|-------------|
| T-80 | INS-24 | `UndoStack.Execute` runs actions in reverse order |
| T-81 | INS-24a | A failing undo does not prevent subsequent undos |
| T-82 | INS-24b | `Execute` returns log entries for each action |
| T-83 | INS-24c | After rollback, created temp file is removed |
| T-84 | INS-24c | After rollback, modified file is restored from backup |
| T-85 | INS-24 | Empty undo stack produces no errors |

### 11.7 process_test.go

| Test | Req | Description |
|------|-----|-------------|
| T-90 | INS-07 | `FindClaudeProcesses` returns empty when no Claude processes running (integration — may find real processes on dev machine; test asserts no panic and valid return type) |
| T-91 | INS-07 | `FindClaudeProcesses` name matching is case-insensitive (verified by checking the matching logic, not by spawning processes) |

Note: process tests are limited because we cannot reliably
spawn fake "claude.exe" processes in CI. The matching logic
is extracted into a pure predicate `isClaudeProcess(name string) bool`
which is fully testable:

| Test | Req | Description |
|------|-----|-------------|
| T-92 | INS-07 | `isClaudeProcess("Claude.exe")` returns true |
| T-93 | INS-07 | `isClaudeProcess("claude.exe")` returns true |
| T-94 | INS-07 | `isClaudeProcess("CLAUDE.EXE")` returns true |
| T-95 | INS-07 | `isClaudeProcess("explorer.exe")` returns false |
| T-96 | INS-07 | `isClaudeProcess("claude-helper.exe")` returns false (exact match, not prefix) |
| T-97 | INS-07 | `isClaudeProcess("claudeNOT.exe")` returns false (exact match, not prefix) |
| T-98 | INS-07c | `isShimProcess("winmcpshim.exe")` returns true |
| T-99 | INS-07c | `isShimProcess("WinMcpShim.exe")` returns true (case-insensitive) |
| T-99a | INS-07c | `isShimProcess("claude.exe")` returns false |
| T-99b | INS-07 | `isClaudeProcess("conhost.exe")` returns false |
| T-99c | INS-07 | `isClaudeProcess("winmcpshim.exe")` returns false |

### 11.8 Integration tests

These test the full pipeline and require the real
`shim.toml.example` file. They run in temp directories.

| Test | Req | Description |
|------|-----|-------------|
| T-100 | INS-23a | Full install pipeline with real `shim.toml.example`, mock Git dir, mock Claude dir. Verify `shim.toml` is valid and `claude_desktop_config.json` has correct entry |
| T-101 | INV-06 | Run the same install pipeline twice. Second run makes no file modifications (compare mtimes) |
| T-102 | INS-24c | Simulate failure after shim.toml creation but before Claude config update. Verify rollback removes shim.toml |

---

## 12. Requirements traceability matrix

| Req | Source file | Function(s) | Test(s) | Status |
|-----|-------------|-------------|---------|--------|
| INS-01 | discover.go | `CheckWindowsVersion` | T-01, T-02, T-03 | OK |
| INS-02 | discover.go | `CheckShimFiles` | T-04, T-07 | OK |
| INS-03 | discover.go | `CheckShimFiles` | T-05, T-07 | OK |
| INS-04 | discover.go | `CheckShimFiles` | T-06, T-07 | OK |
| INS-05 | discover.go | `FindGitForWindows` | T-10, T-11, T-12 | OK |
| INS-05a | cmd/install/main.go | (interactive) | manual | OK |
| INS-05b | discover.go | `CheckGitTools` | T-08, T-09, T-10 | OK |
| INS-06 | discover.go | `CheckClaudeDesktop` | T-11, T-12 | OK |
| INS-07 | process.go | `FindClaudeProcesses`, `isClaudeProcess` | T-90..T-97, T-99b, T-99c | OK |
| INS-07a | cmd/install/main.go | (interactive) + `KillProcesses` | manual | OK |
| INS-07b | process.go | `WaitProcessesGone` | manual | OK |
| INS-07c | process.go | `FindShimProcesses`, `WaitProcessesGone` | T-98, T-99, T-99a | OK |
| INS-08 | toml.go | `GetTomlState` | T-20 | OK |
| INS-08a | toml.go | `GetTomlState` | T-21 | OK |
| INS-08b | toml.go | `GetTomlState` | T-22 | OK |
| INS-09 | toml.go | `ValidateRoot`, `ValidateRoots` | T-23..T-31 | OK |
| INS-09a | toml.go | `ValidateRoots` | T-31 | OK |
| INS-09b | toml.go | `ValidateRoots` | T-29, T-30 | OK |
| INS-10 | toml.go | `SetAllowedRoots`, `FormatTomlRoots` | T-32..T-36, T-42..T-45 | OK |
| INS-11 | toml.go | `SetGitPaths` | T-37..T-39, T-41, T-47 | OK |
| INS-12 | toml.go | `SetGitPaths` | T-40, T-47 | OK |
| INS-13 | discover.go | `CheckTarExe` | T-13 | OK |
| INS-14 | cmd/install/main.go | (interactive) | manual | OK |
| INS-14a | cmd/install/main.go | (interactive) | manual | OK |
| INS-15 | cmd/install/main.go | `os.MkdirAll` | T-100 | OK |
| INS-15a | cmd/install/main.go | write+delete temp file | T-100 | OK |
| INS-16 | atomic.go | `BackupFile` | T-75, T-76, T-77 | OK |
| INS-17 | claude_config.go | `ReadClaudeConfig` | T-50 | OK |
| INS-17a | claude_config.go | `ReadClaudeConfig` | T-51, T-52 | OK |
| INS-18 | claude_config.go | `UpdateClaudeConfig` | T-54 | OK |
| INS-19 | claude_config.go | `UpdateClaudeConfig` | T-55 | OK |
| INS-19a | claude_config.go | `UpdateClaudeConfig` | T-56 | OK |
| INS-19b | claude_config.go | `UpdateClaudeConfig` | T-57 | OK |
| INS-20 | claude_config.go | `UpdateClaudeConfig` | T-58, T-59, T-60 | OK |
| INS-21 | atomic.go, claude_config.go | `WriteAtomic`, `MarshalConfig` | T-62, T-70, T-71 | OK |
| INS-22 | claude_config.go | `NewClaudeConfig` | T-61, T-53 | OK |
| INS-23 | cmd/install/main.go | `exec.Command` | T-100 | OK |
| INS-23a | cmd/install/main.go | exit code check | T-100 | OK |
| INS-23b | cmd/install/main.go | stderr capture + rollback | T-102 | OK |
| INS-24 | rollback.go | `UndoStack.Execute` | T-80 | OK |
| INS-24a | rollback.go | `UndoStack.Execute` | T-81 | OK |
| INS-24b | rollback.go | `UndoStack.Execute` | T-82 | OK |
| INS-24c | rollback.go | `UndoStack.Execute` | T-83, T-84, T-102 | OK |
| INS-25 | cmd/install/main.go | summary output | T-100 | OK |
| INS-26 | cmd/install/main.go | restart message | manual | OK |
| UNS-01 | claude_config.go | `ReadClaudeConfig` | T-53 | OK |
| UNS-02 | claude_config.go | `ReadClaudeConfig` | T-51 | OK |
| UNS-03 | claude_config.go | `RemoveShimEntry` | T-65, T-66 | OK |
| UNS-04 | process.go | `FindClaudeProcesses` | T-90 | OK |
| UNS-04a | cmd/uninstall/main.go | (interactive) | manual | OK |
| UNS-04b | process.go | `FindShimProcesses`, `WaitProcessesGone` | T-98, T-99 | OK |
| UNS-05 | atomic.go | `BackupFile` | T-75 | OK |
| UNS-06 | claude_config.go | `RemoveShimEntry` | T-63 | OK |
| UNS-06a | claude_config.go | `RemoveShimEntry` | T-64 | OK |
| UNS-07 | atomic.go | `WriteAtomic` | T-70 | OK |
| UNS-08 | cmd/uninstall/main.go | summary output | manual | OK |
| UNS-09 | cmd/uninstall/main.go | restart message | manual | OK |
| INV-01 | (architectural) | all discover functions | T-101 | OK |
| INV-02 | atomic.go | `WriteAtomic` | T-70..T-74 | OK |
| INV-03 | (all path handling) | `WriteAtomic`, `ValidateRoot`, `SetGitPaths` | T-74, T-38, T-44 | OK |
| INV-04 | rollback.go | `UndoStack.Push` | T-80 | OK |
| INV-05 | cmd/install/main.go | try/recover + `UndoStack.Execute` | T-102 | OK |
| INV-06 | (all state checks) | all | T-101 | OK |

---

## 13. Out of scope

- Silent/unattended mode (future: accept a parameter file).
- Upgrade from previous version (future).
- Non-Windows platforms.
- Deleting winmcpshim.exe, shim.toml, or logs during uninstall.
