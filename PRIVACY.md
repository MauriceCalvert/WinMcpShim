# Privacy Policy

**WinMcpShim** is a local MCP server that runs entirely on the user's
machine. It is not a hosted service and has no network capability.

## Data collected

**None.** WinMcpShim does not transmit any data over the network. It
has no telemetry, no analytics, no phone-home, and no third-party SDKs
of any kind. Its only inputs are JSON-RPC messages received on stdin
from the Claude Desktop process that launched it; its only outputs are
JSON-RPC responses on stdout and (when invoked with `--log`)
diagnostic log files written to a local directory the user specifies.

## Data processing

WinMcpShim reads and writes files **only** within the directories
declared in `allowed_roots` (configured either in `shim.toml` or via
the `WINMCPSHIM_ALLOWED_ROOTS` environment variable set by the Claude
Desktop extension user_config). All path arguments are verified
against these roots before any file operation is attempted. Symlink
and NTFS-junction escapes are detected via
`GetFinalPathNameByHandle`.

The `run` tool executes external programs. Executables inside
`allowed_roots` may always be invoked. When `run.allowed_commands` is
configured, listed commands are resolved on PATH at startup and
matched by absolute path; entries that fail to resolve are dropped
with a startup warning.

## Local storage

- **JSON-RPC messages** exchanged with Claude Desktop are processed in
  memory and discarded after the response is sent.
- **Log files** are written only if the user supplies `--log <folder>`
  on the command line. These contain the full JSON-RPC traffic,
  including any file paths and file contents read or written during
  the session. The log directory is chosen by the user and stored only
  on the user's local filesystem. WinMcpShim never transmits log files
  anywhere. The user controls their location and retention.

## Third-party data sharing

**None.** WinMcpShim shares no data with any third party. It has no
external dependencies at runtime and makes no outbound network
connections.

## Retention

WinMcpShim retains no data itself. Log files, if enabled, persist
until the user deletes them.

## Contact

- **Author:** Maurice Calvert
- **Location:** Geneva, Switzerland
- **Issues and security reports:**
  <https://github.com/MauriceCalvert/WinMcpShim/issues>

## Changes to this policy

Material changes will be published as commits to this file in the
project repository. Users can watch the repository to receive
notifications.

Last updated: 2026-04-24.
