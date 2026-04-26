// winmcpshim.exe — MCP stdio server for file operations and external tools.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/MauriceCalvert/WinMcpShim/shared"
	"github.com/MauriceCalvert/WinMcpShim/tools"
)

// shutdownFlag is set to 1 when the shim is shutting down (§9.12).
var shutdownFlag atomic.Int32

func main() {
	os.Exit(shimMain())
}

func shimMain() int {
	flags := parseFlags()
	cfg, err := shared.LoadConfig(shared.ConfigPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 1
	}
	applyEnvOverrides(cfg)
	applyFlagOverrides(cfg, flags)
	sanitizedRoots, rootWarnings := shared.SanitizeAllowedRoots(cfg.Security.AllowedRoots)
	cfg.Security.AllowedRoots = sanitizedRoots
	resolvedCommands, allowlistWarnings := shared.ResolveAllowedCommands(cfg.Run.AllowedCommands)
	cfg.Run.AllowedCommands = resolvedCommands
	if flags.scan {
		runScan(cfg)
		return 0
	}
	logger, err := NewLogger(flags.verbose, flags.logDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logging error: %v\n", err)
		return 1
	}
	defer logger.Close()
	logger.Log("event", "shim started")
	buildResult, err := tools.BuildToolSchemas(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "schema error: %v\n", err)
		return 1
	}
	schemas := buildResult.Schemas
	builtinOverrides := buildResult.BuiltinOverrides
	// Emit startup warnings as notifications (grep fallback + sanitizer drops).
	startupWarnings := append(buildResult.Warnings, rootWarnings...)
	startupWarnings = append(startupWarnings, allowlistWarnings...)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, shared.MaxLineSize), shared.MaxLineSize)
	warningsEmitted := false
	for scanner.Scan() {
		if shutdownFlag.Load() != 0 {
			break
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		logger.Log("in", string(line))
		var req shared.Request
		if err := json.Unmarshal(line, &req); err != nil {
			resp := shared.MakeErrorResponse(nil, -32700, "Parse error")
			if !writeAndLog(os.Stdout, resp, logger) {
				break
			}
			continue
		}
		switch req.Method {
		case "initialize":
			handleInitialize(req, logger)
		case "initialized", "notifications/initialized":
			logger.Log("event", "initialized notification received")
			if !warningsEmitted {
				for _, w := range startupWarnings {
					sendWarningNotification(os.Stdout, w, logger)
				}
				warningsEmitted = true
			}
		case "tools/list":
			handleToolsList(req, schemas, logger)
		case "tools/call":
			handleToolsCall(req, cfg, builtinOverrides, logger)
		default:
			if len(req.ID) == 0 {
				logger.Log("error", fmt.Sprintf("unknown notification: %s", req.Method))
			} else {
				resp := shared.MakeErrorResponse(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
				if !writeAndLog(os.Stdout, resp, logger) {
					break
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		logger.Log("event", fmt.Sprintf("stdin error: %v", err))
	}
	logger.Log("event", "shim exiting (stdin closed)")
	return 0
}

// negotiateProtocolVersion echoes the client's requested version.
func negotiateProtocolVersion(req shared.Request) string {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.ProtocolVersion == "" {
		return shared.MaxProtocolVersion
	}
	return params.ProtocolVersion
}

// handleInitialize responds to the initialize request.
func handleInitialize(req shared.Request, logger *Logger) bool {
	agreed := negotiateProtocolVersion(req)
	result := map[string]interface{}{
		"protocolVersion": agreed,
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "winmcpshim",
			"version": "1.0.0",
		},
	}
	resultJSON, _ := json.Marshal(result)
	resp := shared.MakeSuccessResponse(req.ID, resultJSON)
	return writeAndLog(os.Stdout, resp, logger)
}

// handleToolsCall dispatches a tool call to the appropriate handler (§9.11).
func handleToolsCall(req shared.Request, cfg *shared.Config, builtinOverrides map[string]bool, logger *Logger) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("panic in tool handler: %v", r)
			logger.Log("error", msg)
			sendCriticalNotification(os.Stdout, msg, logger)
			resp := shared.MakeSuccessResponse(req.ID, shared.MakeToolResult(shared.CriticalErrorText(msg), true))
			ok = writeAndLog(os.Stdout, resp, logger)
		}
	}()
	var callParams struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &callParams); err != nil {
		resp := shared.MakeErrorResponse(req.ID, -32602, "Invalid params")
		return writeAndLog(os.Stdout, resp, logger)
	}
	logger.Log("call", fmt.Sprintf("%s %v", callParams.Name, callParams.Arguments))
	roots := cfg.Security.AllowedRoots
	maxTimeout := cfg.Security.MaxTimeout
	var result string
	var toolErr error
	switch callParams.Name {
	case "cat":
		result, toolErr = tools.Cat(callParams.Arguments, roots)
	case "copy":
		result, toolErr = tools.Copy(callParams.Arguments, roots)
	case "delete":
		result, toolErr = tools.Delete(callParams.Arguments, roots)
	case "diff":
		result, toolErr = tools.Diff(callParams.Arguments, roots)
	case "edit":
		result, toolErr = tools.Edit(callParams.Arguments, roots)
	case "head":
		result, toolErr = tools.Head(callParams.Arguments, roots)
	case "info":
		result, toolErr = tools.Info(callParams.Arguments, roots)
	case "list":
		result, toolErr = tools.List(callParams.Arguments, roots)
	case "mkdir":
		result, toolErr = tools.Mkdir(callParams.Arguments, roots)
	case "move":
		result, toolErr = tools.Move(callParams.Arguments, roots)
	case "read":
		result, toolErr = tools.Read(callParams.Arguments, roots)
	case "roots":
		result, toolErr = tools.Roots(cfg)
	case "search":
		result, toolErr = tools.Search(callParams.Arguments, roots)
	case "tail":
		result, toolErr = tools.Tail(callParams.Arguments, roots)
	case "tree":
		result, toolErr = tools.Tree(callParams.Arguments, roots)
	case "wc":
		result, toolErr = tools.Wc(callParams.Arguments, roots)
	case "write":
		result, toolErr = tools.Write(callParams.Arguments, roots)
	case "grep":
		if builtinOverrides["grep"] {
			result, toolErr = tools.Grep(callParams.Arguments, roots)
		} else if toolCfg, ok := cfg.Tools["grep"]; ok {
			logger.Log("spawn", fmt.Sprintf("grep %v", callParams.Arguments))
			result, toolErr = tools.DispatchExternalTool("grep", toolCfg, callParams.Arguments, maxTimeout)
		} else {
			result, toolErr = tools.Grep(callParams.Arguments, roots)
		}
	case "run":
		logger.Log("spawn", fmt.Sprintf("run %v", callParams.Arguments))
		result, toolErr = tools.Run(callParams.Arguments, roots, cfg.Run, maxTimeout)
	default:
		if toolCfg, ok := cfg.Tools[callParams.Name]; ok {
			logger.Log("spawn", fmt.Sprintf("%s %v", callParams.Name, callParams.Arguments))
			result, toolErr = tools.DispatchExternalTool(callParams.Name, toolCfg, callParams.Arguments, maxTimeout)
		} else {
			resp := shared.MakeErrorResponse(req.ID, -32602, fmt.Sprintf("Unknown tool: %s", callParams.Name))
			return writeAndLog(os.Stdout, resp, logger)
		}
	}
	if toolErr != nil {
		msg := toolErr.Error()
		var ce *shared.ConfinementError
		if errors.As(toolErr, &ce) && ce.IsCritical {
			msg = shared.CriticalErrorText(ce.Message)
			sendCriticalNotification(os.Stdout, ce.Message, logger)
		}
		resp := shared.MakeSuccessResponse(req.ID, shared.MakeToolResult(msg, true))
		return writeAndLog(os.Stdout, resp, logger)
	}
	resp := shared.MakeSuccessResponse(req.ID, shared.MakeToolResult(result, false))
	return writeAndLog(os.Stdout, resp, logger)
}

// handleToolsList responds with all registered tool schemas.
func handleToolsList(req shared.Request, schemas []shared.ToolSchema, logger *Logger) bool {
	result := map[string]interface{}{
		"tools": schemas,
	}
	resultJSON, _ := json.Marshal(result)
	resp := shared.MakeSuccessResponse(req.ID, resultJSON)
	return writeAndLog(os.Stdout, resp, logger)
}

// applyEnvOverrides lets non-Claude callers bypass shim.toml by passing
// configuration through environment variables. Any variable that is set (non-empty)
// replaces the corresponding field; unset or empty variables leave shim.toml values
// intact. WINMCPSHIM_ALLOWED_ROOTS is split on the OS path-list separator (';' on
// Windows). WINMCPSHIM_ALLOWED_COMMANDS is split on commas.
func applyEnvOverrides(cfg *shared.Config) {
	if v := os.Getenv("WINMCPSHIM_ALLOWED_ROOTS"); v != "" {
		cfg.Security.AllowedRoots = filepath.SplitList(v)
	}
	if v := os.Getenv("WINMCPSHIM_ALLOWED_COMMANDS"); v != "" {
		cfg.Run.AllowedCommands = splitCSV(v)
	}
	if v := os.Getenv("WINMCPSHIM_MAX_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			cfg.Security.MaxTimeout = n
		}
	}
}

// applyFlagOverrides lets the MCPB user_config reach the shim through CLI args.
// MCPB v0.3 only documents array expansion for args[] (a directory list with
// multiple: true expands into separate positional tokens), so the Claude Desktop
// runtime substitutes those placeholders into args reliably; env substitution for
// arrays is unspecified and currently leaves the literal "${user_config.X}" in
// place. Flags are applied after env so the host-supplied values win.
func applyFlagOverrides(cfg *shared.Config, f cliFlags) {
	if f.rootsSet && len(f.allowedRoots) > 0 {
		cfg.Security.AllowedRoots = f.allowedRoots
	}
	if f.commandsSet && len(f.allowedCommands) > 0 {
		cfg.Run.AllowedCommands = f.allowedCommands
	}
	if f.timeoutSet && f.maxTimeout != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(f.maxTimeout)); err == nil && n > 0 {
			cfg.Security.MaxTimeout = n
		}
	}
}

// splitCSV splits a comma-separated string, trims whitespace, drops empty entries.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}

// cliFlags captures everything parseFlags extracts from os.Args. The *Set fields
// distinguish "flag absent" from "flag present with empty value" so applyFlagOverrides
// can mirror the env-var rule of leaving shim.toml untouched on empty input.
type cliFlags struct {
	verbose         bool
	logDir          string
	scan            bool
	allowedRoots    []string
	rootsSet        bool
	allowedCommands []string
	commandsSet     bool
	maxTimeout      string
	timeoutSet      bool
}

// parseFlags parses command-line flags manually. --allowed-roots and
// --allowed-commands are both greedy: each consumes every following non-flag
// token as a separate value, matching how MCPB expands directory/file user_config
// fields with multiple: true (one token per selection).
func parseFlags() cliFlags {
	var f cliFlags
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--verbose":
			f.verbose = true
		case "--log":
			if i+1 < len(args) {
				i++
				f.logDir = args[i]
			}
		case "--scan":
			f.scan = true
		case "--allowed-roots":
			f.rootsSet = true
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++
				if v := strings.TrimSpace(args[i]); v != "" {
					f.allowedRoots = append(f.allowedRoots, v)
				}
			}
		case "--allowed-commands":
			f.commandsSet = true
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++
				if v := strings.TrimSpace(args[i]); v != "" {
					f.allowedCommands = append(f.allowedCommands, v)
				}
			}
		case "--max-timeout":
			if i+1 < len(args) {
				i++
				f.maxTimeout = args[i]
				f.timeoutSet = true
			}
		}
	}
	return f
}

// runScan lists all .exe files in configured scan directories.
func runScan(cfg *shared.Config) {
	for _, dir := range cfg.ScanDirs.Paths {
		entries, err := os.ReadDir(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scan %s: %v\n", dir, err)
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if filepath.Ext(entry.Name()) == ".exe" {
				fmt.Printf("%s\\%s\n", dir, entry.Name())
			}
		}
	}
}

// sendWarningNotification writes a notifications/message at warning level.
func sendWarningNotification(w io.Writer, msg string, logger *Logger) {
	notification := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/message",
		"params": map[string]interface{}{
			"level":  "warning",
			"logger": "winmcpshim",
			"data":   msg,
		},
	}
	data, _ := json.Marshal(notification)
	logger.Log("out", string(data))
	w.Write(data)
	w.Write([]byte("\n"))
}

// sendCriticalNotification writes a notifications/message at error level (§9.15.3).
// Sent before the tool error response.
func sendCriticalNotification(w io.Writer, msg string, logger *Logger) {
	notification := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/message",
		"params": map[string]interface{}{
			"level":  "error",
			"logger": "winmcpshim",
			"data":   msg,
		},
	}
	data, _ := json.Marshal(notification)
	logger.Log("out", string(data))
	w.Write(data)
	w.Write([]byte("\n"))
}

// writeAndLog writes a response to stdout and logs it (§9.12).
func writeAndLog(w *os.File, resp interface{}, logger *Logger) bool {
	data, _ := json.Marshal(resp)
	logger.Log("out", string(data))
	if _, err := w.Write(data); err != nil {
		logger.Log("event", fmt.Sprintf("stdout write error (broken pipe): %v", err))
		shutdownFlag.Store(1)
		return false
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		logger.Log("event", fmt.Sprintf("stdout write error (broken pipe): %v", err))
		shutdownFlag.Store(1)
		return false
	}
	return true
}
