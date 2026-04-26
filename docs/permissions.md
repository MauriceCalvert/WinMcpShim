# WinMcpShim Configuration Files

Two separate Anthropic products can use the shim. Each has its own
configuration file in a different location.


## 1. Claude Desktop

### File

    C:\Users\Momo\AppData\Roaming\Claude\claude_desktop_config.json

### Purpose

Tells Claude Desktop which MCP servers to launch and how to reach them.
Also holds UI preferences (sidebar mode, cowork features, etc.).

### MCP server entry

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

| Field     | Meaning                                                        |
|-----------|----------------------------------------------------------------|
| `"files"` | The server name. Claude Desktop shows this in its UI and uses  |
|           | it as the prefix for tool calls (e.g. `files:read`).          |
| `command` | Absolute path to the executable. Claude Desktop spawns this    |
|           | process and connects to it via stdin/stdout pipes.             |
| `args`    | Command-line arguments passed to the executable. Here we use   |
|           | `--log` with a directory so the shim writes timestamped log    |
|           | files (YYMMDDHHMMSS.log) for diagnostics. Other flags:        |
|           | `--verbose` (also write to stderr), `--scan` (list exes and   |
|           | exit).                                                         |

### Behaviour

Claude Desktop launches the shim at startup, performs the MCP
initialize/initialized handshake, calls tools/list to discover
available tools, then keeps the process alive for the duration of
the session. Closing Claude Desktop closes the shim's stdin, which
causes it to exit cleanly.

### Restart required

Changes to this file only take effect after restarting Claude Desktop.


## 2. Claude Code

### File

    C:\Users\Momo\.claude\settings.json

### Purpose

Controls Claude Code's auto-update channel, sandbox mode, and tool
permissions. This file does NOT configure MCP servers — Claude Code
discovers those from `claude_desktop_config.json` or its own MCP
config. This file controls whether Claude Code is allowed to use
them without prompting.

### Current contents

```json
{
    "autoUpdatesChannel": "latest",
    "sandbox": {
        "enabled": false
    },
    "permissions": {
        "defaultMode": "dontAsk",
        "allow": [
            "Bash",
            "Read",
            "Write",
            "Edit",
            "MultiEdit",
            "Glob",
            "Grep",
            "LS",
            "Task",
            "WebFetch",
            "WebSearch",
            "NotebookEdit",
            "TodoRead",
            "TodoWrite",
            "mcp__desktop-commander__*",
            "mcp__files__*"
        ]
    }
}
```

### Fields

| Field              | Meaning                                                    |
|--------------------|------------------------------------------------------------|
| `autoUpdatesChannel` | Which release channel to track: `"latest"` or `"stable"`. |
| `sandbox.enabled`  | Whether to run commands in a sandboxed environment. Set to |
|                    | `false` for full filesystem access.                        |
| `permissions.defaultMode` | What to do when a tool is not in the allow or deny  |
|                    | lists. `"dontAsk"` means auto-approve. Other values:       |
|                    | `"ask"` (prompt each time), `"deny"` (refuse silently).   |
| `permissions.allow` | List of tool names that Claude Code may use without       |
|                    | prompting. Supports wildcards.                             |

### Built-in tool names

| Name           | What it covers                                    |
|----------------|---------------------------------------------------|
| `Bash`         | Shell command execution                           |
| `Read`         | File reading                                      |
| `Write`        | File creation and overwriting                     |
| `Edit`         | Single find-and-replace in a file                 |
| `MultiEdit`    | Multiple edits in one operation                   |
| `Glob`         | File pattern matching / search                    |
| `Grep`         | Content search across files                       |
| `LS`           | Directory listing                                 |
| `Task`         | Background task management                        |
| `WebFetch`     | HTTP fetch                                        |
| `WebSearch`    | Web search                                        |
| `NotebookEdit` | Jupyter notebook editing                          |
| `TodoRead`     | Read todo/task lists                              |
| `TodoWrite`    | Write todo/task lists                             |

### MCP tool name format

MCP tools use the pattern `mcp__<server>__<tool>` where `<server>`
is the name from the mcpServers config. Wildcards are supported:

| Pattern                      | Meaning                              |
|------------------------------|--------------------------------------|
| `mcp__files__*`              | All tools from the "files" server    |
| `mcp__files__read`           | Only the read tool                   |
| `mcp__desktop-commander__*`  | All tools from desktop-commander     |

### Restart required

Changes to this file take effect when Claude Code is next launched.


## 3. The shim's own config

### File

    D:\projects\MCP_tools\WinMcpShim\shim.toml

### Purpose

Controls the shim's behaviour: which directories are accessible,
which external tools are exposed with rich schemas, and timeout/
output limits. See the comments in shim.toml and winmcpshim-spec.md
for full documentation of every field.

### Restart required

The shim reads shim.toml once at startup. Changes take effect after
restarting Claude Desktop (which restarts the shim).


## 4. Log files

    D:\logs\shim\YYMMDDHHMMSS.log

One log file per shim session. Contains every JSON-RPC message in
and out, tool dispatches, spawn commands, and events (startup,
shutdown, timeouts, errors). Invaluable for debugging MCP protocol
issues.
