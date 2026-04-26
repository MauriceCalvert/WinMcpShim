# WinMcpShim

[![CI](https://github.com/MauriceCalvert/WinMcpShim/actions/workflows/test.yml/badge.svg)](https://github.com/MauriceCalvert/WinMcpShim/actions/workflows/test.yml)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev/)
[![Platform](https://img.shields.io/badge/Platform-Windows%2010+-0078D6?logo=windows)](https://www.microsoft.com/windows)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A safe, fast, stall-proof MCP server for Windows providing a complete set of file, directory,
text, and command-execution tools.

**Fast.** On Windows 10, WinMcpShim is **41× faster to cold-start** and
**168× faster per call** than Desktop Commander, and **7.8× / 3.1×** faster
than MCP Filesystem on the same measures. Full benchmark table in
[Performance](#performance).

**Safe.** Built to [DO-178C](https://en.wikipedia.org/wiki/DO-178C) avionics
safety practices: 503 tests, 88% merged coverage, TOCTOU-hardened
path confinement, and an explicit allowlist for the `run` tool. Every
line of uncovered code is individually justified.

**Note:** WinMcpShim is designed for **Claude Desktop**. Claude Code
already has equivalent operations built into its CLI and has no use for this shim.

## Installation

### Prerequisites

- **Windows 10** or later
- Optionally **[Git for Windows](https://gitforwindows.org/)** for the
  configured external tools (sed, awk, sort, xxd, dos2unix, cut, uniq,
  tar). The built-in tools (cat, read, write, edit, copy, move, delete,
  list, tree, search, grep, head, tail, info, diff, mkdir, wc, roots,
  run) do not require Git.

### Configuration is required

Whichever install method you pick, WinMcpShim stays locked down until
you tell it which directories it may access. Every file operation
returns `Path not within allowed directories` otherwise. The three
ways to supply that list:

- **Claude Desktop's extensions dialog** — for the MCPB install path.
  **Settings → Extensions → WinMcpShim** has an **Allowed root
  directories** field.
- **`install.exe`** — for the installer path. Prompts interactively
  and writes the answers to `shim.toml`.
- **`config.cmd`** — a small WinForms GUI shipped alongside the
  binaries (wraps `config.ps1`). Add/remove allowed roots, add/remove
  `run` allowlist entries, then offers to restart Claude Desktop.
  Locates `shim.toml` via `claude_desktop_config.json`, so it edits
  the file the installed shim actually reads — you can run it from
  anywhere. Use this for manual installs, and for changing settings
  after any install.

### Install as a Claude Desktop extension (recommended)

Once WinMcpShim is published in the Claude Desktop directory:

1. Open Claude Desktop → **Settings → Extensions**.
2. Click **Browse extensions**, find WinMcpShim, click **Install**.
3. In the configuration dialog, set:
   - **Allowed root directories** — one or more folders the shim may
     read or write under.
   - **Allowed commands for the run tool** (optional) — comma-separated
     list of executable names (e.g. `git,python,npm`). When set, the
     `run` tool accepts only these commands and may invoke them from
     anywhere on `PATH`. Leave blank to restrict `run` to executables
     inside the allowed roots.
   - **Maximum tool-call timeout** — upper bound in seconds (default 60).
4. Restart Claude Desktop.

To install a local `.mcpb` build before directory listing, use
**Settings → Extensions → Advanced → "Install Extension…"** and pick
the `.mcpb` file. The rest is identical.

No `shim.toml` or `claude_desktop_config.json` editing is required;
Claude Desktop passes your choices to the shim via environment
variables.

The extension install path ships only the 19 built-in tools (cat,
copy, delete, diff, edit, grep, head, info, list, mkdir, move, read,
roots, run, search, tail, tree, wc, write). If you need the
configured external tools (sed, awk, sort, xxd, dos2unix, cut, uniq,
tar — all of which require Git for Windows), use the installer path
below, which ships a pre-populated `shim.toml`.

### Install as a custom MCP server (installer)

Use this path if you want local `shim.toml` control, per-run log
files, or a deployment that doesn't rely on the Extensions UI.

1. Download the latest release zip.
2. Extract to any directory (e.g. `C:\WinMcpShim`).
3. Run `install.exe`.

The installer will:
- Verify prerequisites (winmcpshim.exe, strpatch.exe, Git for Windows).
- Create `shim.toml` from the template if it doesn't exist.
- Ask which directories to allow (your project folders).
- Create a log directory at `%USERPROFILE%\logs\shim`.
- Add the MCP server entry to `claude_desktop_config.json`.
- The installer is idempotent — safe to run multiple times.

After installation, **restart Claude Desktop**.

### Manual install (advanced)

If you prefer to configure everything yourself:

1. Place `winmcpshim.exe` and `strpatch.exe` in the same directory.

2. Copy `shim.toml.example` to `shim.toml` and edit `allowed_roots`
   to list your working directories:

   ```toml
   [security]
   allowed_roots = [
       "C:\\Users\\YourName\\Projects",
       "C:\\Users\\YourName\\Documents",
   ]
   ```

3. Add this to `%APPDATA%\Claude\claude_desktop_config.json`:

   ```json
   {
     "mcpServers": {
       "WinMcpShim": {
         "command": "C:\\path\\to\\winmcpshim.exe",
         "args": ["--log", "C:\\Users\\YourName\\logs\\shim"]
       }
     }
   }
   ```

4. Create the log directory and restart Claude Desktop.

### Tool permissions

After installing and restarting Claude Desktop, find WinMcpShim in
**Settings → Extensions** (if installed via the MCPB path) or
**Settings → Connectors** (if installed via `install.exe` or manually)
and set per-tool permissions. Every tool carries a read-only or
destructive hint that Claude honours by default; use this screen to
tighten them further.

### Uninstallation

- **Extension (MCPB):** Settings → Extensions → WinMcpShim →
  Uninstall. Restart Claude Desktop.
- **Custom server (installer):** run `uninstall.exe`. This removes
  the WinMcpShim entry from `claude_desktop_config.json` but leaves
  `winmcpshim.exe`, `shim.toml`, and log files in place. Restart
  Claude Desktop. To fully remove, delete the installation directory.
- **Manual:** remove the `"WinMcpShim"` block from
  `claude_desktop_config.json` and delete the installation directory.

### Build from source

Requires **Go 1.25+** (tested with Go 1.26.1).

```
cd WinMcpShim
go build -o winmcpshim.exe ./shim
cd strpatch
go build -o strpatch.exe .
```

Both produce statically linked executables with no runtime dependencies.
`make.bat` automates this (tidies, vets, and builds all executables).

### Run tests

```
test.bat
```

This runs all tests across shim, tools, shared, installer, and strpatch.
Requires Go on PATH.

## Configuration

WinMcpShim takes its configuration from two sources, evaluated in this
order:

1. `shim.toml`, loaded from the same directory as `winmcpshim.exe` if
   present (written by `install.exe` or by hand for the custom-server
   and manual install paths).
2. Environment variables, which override the corresponding TOML fields
   when set (populated by Claude Desktop's extension host from the
   MCPB user_config dialog, or set by hand for ad-hoc deployments).

If neither source supplies a setting, a safe default is used. Full
specification: [docs/winmcpshim-spec.md](docs/winmcpshim-spec.md).

### Security

`allowed_roots` declares which directories the shim may access. All
path arguments are verified against these roots. Symlink and NTFS
junction escapes are detected via `GetFinalPathNameByHandle` on the
open file handle.

```toml
[security]
allowed_roots = ["D:\\projects", "C:\\Users\\YourName"]
max_timeout = 60
```

### Changing allowed paths after install

How you change the allowed roots depends on which install path you used:

- **Extension (MCPB):** open Claude Desktop → **Settings → Extensions →
  WinMcpShim**, edit **Allowed root directories**, then **restart
  Claude Desktop**. Changes take effect on restart — the shim reads
  `WINMCPSHIM_ALLOWED_ROOTS` once at startup.

- **Installer or manual:** run `config.cmd` (ships in the release)
  for a GUI, or edit the `allowed_roots` array in `shim.toml` (in
  the same directory as `winmcpshim.exe`) by hand. Either way, save
  and **restart Claude Desktop**. Re-running `install.exe` also
  works and is idempotent.

- **Any install path** can also be overridden at launch by setting the
  `WINMCPSHIM_ALLOWED_ROOTS` environment variable (semicolon-separated
  list, e.g. `D:\projects;C:\Users\Name\Documents`). When set,
  this overrides the value in `shim.toml`.

Paths must be absolute. Forward slashes and backslashes both work; in
TOML, backslashes must be doubled (`C:\\Users\\Name`). The shim does
not expand `~` or environment variables — write full paths.

### Allowed run commands

The `run` tool executes arbitrary command lines. The permission gate is
additive:

- **Allowed roots always grant execute.** Any executable that resolves
  to a path inside `allowed_roots` may be invoked, in every mode. The
  roots you declared for read/write also serve as an execute permission.

- **Allowlist (opt-in widening).** Set `run.allowed_commands` (in
  `shim.toml`) or `WINMCPSHIM_ALLOWED_COMMANDS` (env var) to a list of
  bare names or absolute paths. At startup the shim resolves each bare
  name via PATH and stores the absolute path; matching at call time is
  exact-path, case-insensitive on Windows. Entries that fail to resolve
  are dropped with a `notifications/message` warning. Listed commands
  may be invoked from anywhere; they do not need to be inside
  `allowed_roots`.

- **No allowlist (default).** When `allowed_commands` is empty, the run
  tool falls back to directory confinement: absolute paths must resolve
  inside `allowed_roots`, and unqualified names are accepted only if
  `exec.LookPath` finds them.

```toml
[run]
# Bare names get rewritten to absolute paths at startup. Equivalent:
# allowed_commands = ["C:\\Program Files\\Git\\cmd\\git.exe"]
allowed_commands = ["git", "python", "npm"]
```

`config.cmd` performs the same PATH lookup at edit time and stores the
resolved absolute path directly in `shim.toml`, so what you see in the
GUI list is what the runtime will compare against.

**Interpreter trap.** Adding a shell or scripting interpreter to the
allowlist makes the allowlist meaningless. Once `powershell`, `cmd`,
`bash`, `wsl`, `python`, `node`, `ruby`, `perl`, `dotnet`, `java`,
`cscript`, `wscript`, or similar is on the list, Claude can pipe
**any** command through the interpreter (`powershell -Command 'systeminfo'`)
and bypass the gate you thought you were setting. `config.cmd` shows
a warning when you try to add one of these names; the MCPB extensions
dialog and hand-edited `shim.toml` have no such check, so only add
interpreters if you genuinely want unrestricted `run`.

How to change the run allowlist depends on the install path:

- **Extension (MCPB):** Claude Desktop → **Settings → Extensions →
  WinMcpShim**, click **Allowed executables for the run tool**, and
  pick each `.exe` with the native file dialog. Each pick stores an
  absolute path, so there is no PATH-shadowing risk and no bare-name
  ambiguity. Restart Claude Desktop after saving.

- **Installer or manual:** run `config.cmd` (GUI — typed names are
  resolved on PATH at edit time and saved as absolute paths) or edit
  `run.allowed_commands` in `shim.toml` by hand. Bare names typed by
  hand are resolved on PATH at server startup; entries that fail to
  resolve are dropped with a warning.

- **Env-var override:** set `WINMCPSHIM_ALLOWED_COMMANDS` to a
  comma-separated list of names or absolute paths. Non-empty value
  overrides `shim.toml`.

Leaving the allowlist empty keeps `run` restricted to executables
inside `allowed_roots` — the recommended default for untrusted use.

### External tools

External tools are declared with explicit descriptions and typed
parameter schemas. These tools require Git for Windows to be
installed. See `shim.toml` for the full set of configured tools
and their parameters.

### Command-line flags

- `--verbose` — write diagnostics to stderr
- `--log <folder>` — write diagnostics to a timestamped log file
- `--scan` — list discovered executables and exit

### Environment variable overrides

For deployment as a Claude Desktop extension (MCPB), configuration can
be supplied via environment variables instead of editing `shim.toml`.
Any variable that is set (non-empty) replaces the corresponding
`shim.toml` field; unset or empty variables leave the TOML values
intact.

- `WINMCPSHIM_ALLOWED_ROOTS` — semicolon-separated list of allowed
  directories (e.g. `D:\projects;C:\Users\Name\Documents`).
- `WINMCPSHIM_ALLOWED_COMMANDS` — comma-separated list of commands
  permitted by the `run` tool (e.g. `git,python,npm`).
- `WINMCPSHIM_MAX_TIMEOUT` — integer ceiling on per-tool timeouts
  (seconds).

## Example prompts

1. **Find all Python files containing "TODO" in a project:**

   > Search my D:\projects\myapp directory for all Python files that
   > contain TODO comments, and list them with line numbers.

   Claude will use `grep` with `recursive=true`, `include=*.py`,
   and `line_numbers=true`.

2. **Read, edit, and verify a configuration file:**

   > Read the file D:\projects\myapp\config.yaml, change the
   > database host from "localhost" to "db.prod.internal", then
   > show me the result.

   Claude will use `read` to inspect the file, `edit` to make the
   replacement, then `read` again to confirm.

3. **Compare two versions of a file and create a backup:**

   > Compare D:\projects\myapp\main.py with
   > D:\projects\myapp\main.py.bak, show me the differences,
   > then copy the current version to a new backup with today's
   > date in the filename.

   Claude will use `diff` to show changes, then `copy` to create
   the dated backup.

## Performance

Benchmark results comparing WinMcpShim against two popular MCP file servers
(median of 20 calls per operation, Windows 10, 24-core Threadripper):

| Operation | WinMcpShim | MCP Filesystem | Desktop Commander |
|---|--:|--:|--:|
| Cold start | 40 ms | 308 ms (7.8×) | 1606 ms (41×) |
| Read 1 KB | 518 µs | 1.1 ms (2.0×) | 39.9 ms (77×) |
| Read 100 KB | 2.0 ms | 3.0 ms (1.5×) | 40.1 ms (20×) |
| List directory | 999 µs | 1.0 ms (1.0×) | 20.8 ms (21×) |
| File info | 1.0 ms | 530 µs (0.5×) | 20.4 ms (20×) |
| Write file | 1.4 ms | 2.0 ms (1.4×) | 38.6 ms (28×) |
| Throughput | 232 µs/call | 728 µs/call (3.1×) | 38.9 ms/call (168×) |

**Grep** is always the built-in Go RE2 implementation. External GNU
grep (Git for Windows) is forbidden: its MSYS2 binary mangles Windows
paths under recursive search.

Reproduce the server benchmarks with `bench.bat` (requires all three
servers installed).

## Troubleshooting

**Shim does not appear in Claude Desktop:**
If installed as an extension, check **Settings → Extensions** —
WinMcpShim should be listed and enabled. If installed via
`install.exe` or manually, check that `claude_desktop_config.json`
has the correct absolute path to `winmcpshim.exe`. Restart Claude
Desktop after any configuration change.

**"Path not within allowed directories" error:**
The requested file is outside the configured allowed roots. Add the
required directory — via **Settings → Extensions → WinMcpShim** for
the MCPB install path, or by editing `allowed_roots` in `shim.toml`
for the installer and manual paths — then restart Claude Desktop.

**Tool call times out:**
The default inactivity timeout is 10 seconds. For long-running
commands, Claude can pass a higher `timeout` parameter (up to
`max_timeout`, default 60 seconds). If the command genuinely needs
longer, raise the ceiling — via **Settings → Extensions → WinMcpShim →
Maximum tool-call timeout** for the MCPB install path, or by
increasing `max_timeout` in `shim.toml` for the installer and
manual paths.

**External tools not working:**
The configured external tools (sed, awk, sort, xxd, etc.) require
[Git for Windows](https://gitforwindows.org/). Verify it is
installed and that `C:\Program Files\Git\usr\bin\` contains the
expected binaries. (`grep` is always served by the built-in
implementation — any `[tools.grep]` config is ignored.)

**Log files:**
When started with `--log D:\logs\shim`, the shim writes a timestamped
log file (e.g. `260401140533.log`) containing every JSON-RPC message,
tool dispatch, and event. These are the primary debugging resource.

## Safety

All file operations are confined to the configured `allowed_roots`
(declared in `shim.toml` or supplied by the Claude Desktop extension
host via the `WINMCPSHIM_ALLOWED_ROOTS` environment variable). The
following defences are in place:

1. **TOCTOU (Time Of Check to Time Of Use)** — a race condition
   where a path is valid at check time but has been redirected (e.g.
   junction swapped) by the time the file is opened. Defeated by a
   two-stage check: a pre-check validates the path string against
   allowed roots, then a post-check calls `GetFinalPathNameByHandle`
   on the already-open file handle to verify the resolved path.
   If the resolved path is outside allowed roots, the operation is
   aborted and a critical error is raised.

2. **Symlink and junction escape** — an attacker places a symlink or
   NTFS junction inside an allowed root that points outside it. The
   post-check (see above) resolves through symlinks and junctions
   to the real path, catching this regardless of how deep the
   indirection goes.

3. **Command injection** — passing shell metacharacters (`;`, `&`,
   `|`, backticks) to execute unintended commands. Defeated by
   never invoking a shell: all child processes are spawned via
   `os/exec.Command` directly. Arguments are passed as an array,
   not interpolated into a command string.

4. **Path traversal** — using `..` sequences to escape allowed
   roots (e.g. `allowed_root/../../etc/passwd`). The pre-check
   canonicalises the path before comparing it against allowed roots.

5. **Unbounded execution** — a malicious or runaway child process
   consuming resources indefinitely. Claude-requested timeouts are
   clamped to `max_timeout` (configurable, default 60s). Both an
   inactivity timeout and a total wall-clock timeout are enforced.

6. **Accidental data destruction** — delete uses `os.Remove`, not
   `os.RemoveAll`, so it cannot delete a directory that contains
   files. Copy and move refuse to overwrite an existing destination.
   Edit requires the search string to appear exactly once.

7. **Partial write corruption** — writes go to a temporary file in
   the same directory, then `os.Rename` atomically replaces the
   target. A crash at any point leaves either the original or the
   complete new file, never a truncated one.

8. **Binary file misinterpretation** — the first 8 KB is scanned for
   null bytes. Binary files are refused rather than returned as
   garbled text.

9. **Command confinement** — the `run` tool gates execution on a
   resolved absolute path. Allowed roots always grant execute permission
   (any file inside them may be invoked). When `run.allowed_commands` is
   non-empty, each entry is resolved on PATH at startup and matched by
   absolute path at call time (case-insensitive on Windows); bare names
   like `git` are rewritten to e.g. `C:\Program Files\Git\cmd\git.exe`,
   so a later PATH change cannot retarget the gate. Entries that fail to
   resolve are dropped with a startup warning. When the allowlist is
   empty, the run tool falls back to directory confinement plus
   PATH-resolvable unqualified names, prevening Claude from invoking an
   arbitrary binary by absolute path outside the roots.

10. **Critical error protocol** — confinement breaches emit a
   `notifications/message` at error level before the tool response,
   with a `🛑 CRITICAL:` prefix instructing Claude to alert the
   user and not retry. This ensures the user is informed even if
   Claude would otherwise silently retry.

11. **Interpreter allowlist trap** — a `run` allowlist containing
    `powershell`, `cmd`, `bash`, `wsl`, `python`, `node`, `ruby`,
    `perl`, `dotnet`, `java`, `cscript`, `wscript`, or similar is
    equivalent to an empty allowlist: Claude can pipe any command
    through the interpreter. `config.cmd` detects these names on
    add and shows a warning dialog (default button: *No*) before
    the entry is accepted, so the trade-off is visible. The shim
    itself does not refuse interpreters — some users legitimately
    want `powershell` — but the GUI makes the implication explicit.


## Reliability

1. **Orphan process survival** — when a child process spawns
   grandchildren, killing the child leaves the grandchildren running
   as orphans. Defeated by assigning every child to a Windows Job
   Object configured to kill on close. When the parent is killed or
   times out, the Job Object terminates all descendants.

2. **WER dialog hang** — when a child crashes on Windows, the
   Windows Error Reporting dialog can block indefinitely waiting for
   user input. Defeated by setting `SEM_NOGPFAULTERRORBOX` on child
   processes, so crashes return immediately to the shim.

3. **Pipe deadlock** — when a child floods both stdout and stderr
   simultaneously, a single-threaded reader blocks on one pipe while
   the other fills its OS buffer and blocks the child. Defeated by
   reading stdout and stderr in separate goroutines.

4. **Unbounded output** — a child producing gigabytes of output
   would exhaust memory. Stdout exceeding `max_output` triggers
   child termination with a truncation message. Stderr is capped
   independently without killing the child, appending
   `[stderr truncated]`.

5. **Panic in tool handler** — a nil pointer dereference or other
   panic in a tool handler would crash the shim, killing the
   connection. Defeated by `defer recover()` in the dispatch
   function, which catches panics, returns an error to Claude,
   and continues processing the next request.

6. **Tool error cascading** — a tool failure must not bring down
   the server. All tool errors are returned as `isError: true`
   tool results within the JSON-RPC success envelope. The shim
   never exits on a tool failure.

7. **Client disconnection** — clean exit on stdin EOF (Claude
   Desktop closed the pipe). On broken stdout (pipe write failure),
   `shutdownFlag` is set and the main loop exits on the next
   iteration.

8. **File locking conflicts** — Windows sharing violations from
   antivirus scanners or editors holding files. File operations
   retry with backoff before returning an error.

9. **Line ending corruption** — writing LF to a CRLF file silently
   corrupts it. The shim detects existing line endings and preserves
   them. New files use LF.

10. **Encoding hazards** — UTF-8 BOM is stripped transparently.
    UTF-16LE/BE files (common from PowerShell 5.1, `reg export`,
    `wmic`) are detected by BOM and decoded to UTF-8 for read-only
    tools.

11. **Configuration errors** — malformed TOML, UTF-16 config files,
    and missing required fields are rejected at startup rather than
    running with partial configuration. If `shim.toml` is absent,
    the shim starts with built-in tools only.

12. **Handle leaks** — Job Objects and process handles are closed
    after each tool call. Validated by a 200-sequential-calls stress
    test.

13. **Large message truncation** — the scanner buffer is sized to
    `MaxLineSize` to handle large JSON-RPC messages without silent
    truncation.

## Quality Assurance

**100% of code is either tested or justified.** Merged test coverage
is 88%, comprising integration tests (spawning the real binary over
JSON-RPC) and unit tests (calling functions directly). The remaining
12% is individually justified in
[docs/coverage_gaps.md](docs/coverage_gaps.md) as: code tested by
integration tests but invisible to the coverage tool (5%), defensive
error handling for Windows kernel API failures that cannot be triggered
from user space (4%), and accepted low-risk gaps with per-function
rationale (3%). No uncovered code in security-critical paths is due
to missing tests.

1. **Specification-first design** — `docs/winmcpshim-spec.md` is the authoritative source. When code diverges from spec, the code is the bug. The spec is versioned and dated.

2. **Requirements Traceability Matrix** — spec §15 maps every requirement to its test. Test function names reference spec sections (e.g. `TestSecurity_JunctionEscape` cites `§8.1.2 / 14.2.4`).

3. **Integration test harness** — shim_test.go spawns the real binary as a subprocess, communicates via JSON-RPC over stdin/stdout, and validates responses. Tests exercise the complete code path from protocol parsing to file I/O and back.

4. **Binary coverage instrumentation** — `go build -cover` with `GOCOVERDIR` captures real code coverage from integration tests, not just unit test coverage. Produces per-function and line-by-line HTML reports.

5. **Adversarial test helper** — `rogue.exe` is a purpose-built hostile child process that floods pipes, spawns grandchildren, crashes deliberately, hangs, and reads stdin forever. Tests confirm the shim handles each attack correctly.

6. **Combo attack test** — `TestRun_ComboAttack` exercises grandchild spawning, pipe flooding, and crashing simultaneously to verify they don't interact badly.

7. **Stress test** — `TestRun_HandleLeak` runs 200 sequential command executions to detect handle leaks or resource exhaustion.

8. **Automated quality script** — `quality.ps1` runs ten checks in one pass: test coverage, race detection, go vet, staticcheck, govulncheck, cyclomatic complexity, spec-to-test traceability, security surface review, dead code audit, and build consistency. Results are written to the `quality-report` folder.

9. **Race detector** — `go test -race` confirms no data races in concurrent pipe draining, timeout management, or shutdown coordination.

10. **Static analysis pipeline** — staticcheck (50+ lint rules), go vet (misuse detection), govulncheck (dependency CVEs), gocyclo (complexity thresholds).

11. **CI on every push** — GitHub Actions runs `go test` for both the shim and strpatch on `windows-latest` with a 180s timeout.

12. **Config validation at startup** — malformed TOML, UTF-16 config files, missing builtin descriptions, and invalid parameter declarations (both flag+position, neither flag nor position) are all detected and rejected before the shim accepts any requests.

13. **Benchmark suite** — `cmd/bench` measures cold start, per-operation latency, and throughput against two competitor MCP servers, with median-of-20 methodology and formatted comparison output.

14. **Diagnostic logging** — `--verbose` and `--log` flags capture every JSON-RPC message, tool dispatch, and event with timestamps, providing full replay capability for post-incident analysis.

15. **Idempotent installer** — `install.exe` can be re-run safely. It checks prerequisites, creates configs only if absent, and validates state before modifying anything.

16. **Structured documentation** — separate specs for the shim, installer, strpatch, and rogue helper, each with their own requirements and prescribed tests.

## Statistics

SLOC


| Metric | SLOC (non-blank, non-comment) |
|--------|-------------------------------|
| Production code | 5,708 |
| Test code | 8,027 |
| Total | 13,735 |
| Test:code ratio | 1.41:1 |

Test-to-code ratios by package:

| Package | Production | Tests | Ratio |
|---------|------------|-------|-------|
| tools | 2,111 | 2,826 | 1.34:1 |
| shim | 416 | 2,565 | 6.16:1 |
| shared | 853 | 873 | 1.02:1 |
| installer + cmd | 1,283 | 1,301 | 1.01:1 |
| strpatch | 276 | 462 | 1.67:1 |
| cmd/bench | 580 | — | — |
| testhelpers | 189 | — | — |
| **Total** | **5,708** | **8,027** | **1.41:1** |

## Documentation

Full technical documentation is in the [docs/](docs/) folder:

- [docs/winmcpshim-spec.md](docs/winmcpshim-spec.md) — shim specification
- [docs/install-spec.md](docs/install-spec.md) — installer specification
- [docs/strpatch-spec.md](docs/strpatch-spec.md) — strpatch.exe specification
- [docs/rogue-spec.md](docs/rogue-spec.md) — adversarial test helper specification
- [docs/permissions.md](docs/permissions.md) — config file reference

## Privacy Policy

WinMcpShim is a local MCP server. It runs entirely on your machine
and communicates only with the Claude Desktop process that launched it
via stdio pipes.

**Data collected:** None. The shim does not transmit any data over
the network. It has no network access, no telemetry, and no
phone-home capability.

**Local logging:** When started with the `--log` flag, the shim
writes diagnostic logs to a local directory you specify. These logs
contain the full JSON-RPC traffic between Claude Desktop and the shim,
which includes file paths and file contents that Claude reads or
writes. Log files are stored only on your local filesystem and are
never transmitted anywhere. You control their location and retention.

**File access:** The shim reads and writes files only within the
configured `allowed_roots` (declared in `shim.toml` or supplied by
the Claude Desktop extension host via the
`WINMCPSHIM_ALLOWED_ROOTS` environment variable). It does not
access the Windows registry, clipboard, network, or any data outside
the configured directories.

**Third-party data sharing:** None. No data is shared with any third
party. The shim is a standalone binary with no external dependencies
at runtime.

## Support and Contact

- **Author:** Maurice Calvert
- **Location:** Geneva, Switzerland
- **Issues:** File issues or questions via the project repository.
- **Email:** For direct enquiries, contact via the repository.

## Contributing

Contributions are welcome. Build with `make.bat`, run tests with
`test.bat`. Code style: Go standard formatting, explicit error
handling, no external dependencies beyond BurntSushi/toml and
golang.org/x/sys.

## Licence

MIT. See [LICENSE](LICENSE) for the full text.
