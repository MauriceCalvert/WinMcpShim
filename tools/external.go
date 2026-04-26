package tools

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/MauriceCalvert/WinMcpShim/shared"
)

// BuildResult holds the schemas and any startup warnings from BuildToolSchemas.
type BuildResult struct {
	Schemas        []shared.ToolSchema
	Warnings       []string
	BuiltinOverrides map[string]bool
}

// BuildToolSchemas generates tool schemas for all built-in and configured tools.
// Returns schemas, startup warnings, and a set of tool names using builtin overrides.
func BuildToolSchemas(cfg *shared.Config) (*BuildResult, error) {
	schemas, err := BuiltinSchemas(cfg.BuiltinDescriptions, cfg.Run.InactivityTimeout, cfg.Security.MaxTimeout)
	if err != nil {
		return nil, err
	}
	result := &BuildResult{
		Schemas:          schemas,
		BuiltinOverrides: make(map[string]bool),
	}
	for name, toolCfg := range cfg.Tools {
		if name == "grep" {
			// External grep is forbidden: GNU grep from Git for Windows is an
			// MSYS2 binary that mangles Windows paths under recursive search.
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("grep: [tools.grep] in config is ignored (exe=%s); built-in grep is always used", toolCfg.Exe))
			continue
		}
		result.Schemas = append(result.Schemas, ExternalToolSchema(name, toolCfg, cfg.Security.MaxTimeout))
	}
	// Always register built-in grep.
	desc, ok := cfg.BuiltinDescriptions["grep"]
	if !ok {
		desc = "Search inside file contents by regex. Uses Go RE2 regex syntax (not GNU grep BRE): unescaped (, ), +, ? are metacharacters. No backreferences. Set recursive=true + include to search a directory tree."
	}
	result.Schemas = append(result.Schemas, BuiltinGrepSchema(desc))
	result.BuiltinOverrides["grep"] = true
	return result, nil
}

// BuiltinSchemas returns the MCP schemas for all built-in tools (§11.2).
func BuiltinSchemas(descriptions map[string]string, runDefaultTimeout int, maxTimeout int) ([]shared.ToolSchema, error) {
	type entry struct {
		name        string
		schema      string
		annotations *shared.ToolAnnotations
	}
	entries := []entry{
		{"read", `{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute file path"},
				"offset": {"type": "integer", "description": "Byte offset to start reading (for large files or partial reads)"},
				"limit": {"type": "integer", "description": "Maximum bytes to return (for large files or partial reads)"}
			},
			"required": ["path"]
		}`, &shared.ToolAnnotations{
			Title:           "Read File",
			ReadOnlyHint:    true,
			DestructiveHint: false,
			IdempotentHint:  true,
			OpenWorldHint:   false,
		}},
		{"write", `{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute file path"},
				"content": {"type": "string", "description": "Text content to write"},
				"append": {"type": "boolean", "description": "Append to existing file instead of overwriting"}
			},
			"required": ["path", "content"]
		}`, &shared.ToolAnnotations{
			Title:           "Write File",
			ReadOnlyHint:    false,
			DestructiveHint: true,
			IdempotentHint:  true,
			OpenWorldHint:   false,
		}},
		{"edit", `{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute file path"},
				"old_text": {"type": "string", "description": "Exact text to find (must appear exactly once in the file)"},
				"new_text": {"type": "string", "description": "Replacement text (empty string to delete)"}
			},
			"required": ["path", "old_text", "new_text"]
		}`, &shared.ToolAnnotations{
			Title:           "Edit File",
			ReadOnlyHint:    false,
			DestructiveHint: true,
			IdempotentHint:  false,
			OpenWorldHint:   false,
		}},
		{"copy", `{
			"type": "object",
			"properties": {
				"source": {"type": "string", "description": "Absolute source path"},
				"destination": {"type": "string", "description": "Absolute destination path (must not already exist)"},
				"recursive": {"type": "boolean", "description": "Required for directories; copies the entire tree"}
			},
			"required": ["source", "destination"]
		}`, &shared.ToolAnnotations{
			Title:           "Copy File or Directory",
			ReadOnlyHint:    false,
			DestructiveHint: false,
			IdempotentHint:  false,
			OpenWorldHint:   false,
		}},
		{"move", `{
			"type": "object",
			"properties": {
				"source": {"type": "string", "description": "Absolute source path"},
				"destination": {"type": "string", "description": "Absolute destination path (must not already exist)"}
			},
			"required": ["source", "destination"]
		}`, &shared.ToolAnnotations{
			Title:           "Move or Rename",
			ReadOnlyHint:    false,
			DestructiveHint: true,
			IdempotentHint:  false,
			OpenWorldHint:   false,
		}},
		{"delete", `{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute path to delete"}
			},
			"required": ["path"]
		}`, &shared.ToolAnnotations{
			Title:           "Delete File or Empty Directory",
			ReadOnlyHint:    false,
			DestructiveHint: true,
			IdempotentHint:  false,
			OpenWorldHint:   false,
		}},
		{"list", `{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute directory path"},
				"pattern": {"type": "string", "description": "Glob pattern to filter entries (e.g. *.py)"}
			},
			"required": ["path"]
		}`, &shared.ToolAnnotations{
			Title:           "List Directory",
			ReadOnlyHint:    true,
			DestructiveHint: false,
			IdempotentHint:  true,
			OpenWorldHint:   false,
		}},
		{"search", `{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Root directory to search from"},
				"pattern": {"type": "string", "description": "Glob pattern matched against filenames (e.g. *.py, test_*)"},
				"max_results": {"type": "integer", "description": "Maximum results to return (default 100)"}
			},
			"required": ["path", "pattern"]
		}`, &shared.ToolAnnotations{
			Title:           "Search Files by Name",
			ReadOnlyHint:    true,
			DestructiveHint: false,
			IdempotentHint:  true,
			OpenWorldHint:   false,
		}},
		{"cat", `{
			"type": "object",
			"properties": {
				"paths": {"type": "string", "description": "Space-separated absolute paths (quote paths with spaces), or a JSON array of strings"}
			},
			"required": ["paths"]
		}`, &shared.ToolAnnotations{
			Title:           "Read/Concatenate Files",
			ReadOnlyHint:    true,
			DestructiveHint: false,
			IdempotentHint:  true,
			OpenWorldHint:   false,
		}},
		{"diff", `{
			"type": "object",
			"properties": {
				"file1": {"type": "string", "description": "Absolute path to first file"},
				"file2": {"type": "string", "description": "Absolute path to second file"},
				"context_lines": {"type": "integer", "description": "Lines of context around each change (default 3)"}
			},
			"required": ["file1", "file2"]
		}`, &shared.ToolAnnotations{
			Title:           "Diff Two Files",
			ReadOnlyHint:    true,
			DestructiveHint: false,
			IdempotentHint:  true,
			OpenWorldHint:   false,
		}},
		{"head", `{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute file path"},
				"lines": {"type": "integer", "description": "Number of lines to return (default 10)"}
			},
			"required": ["path"]
		}`, &shared.ToolAnnotations{
			Title:           "First N Lines",
			ReadOnlyHint:    true,
			DestructiveHint: false,
			IdempotentHint:  true,
			OpenWorldHint:   false,
		}},
		{"info", `{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute path"}
			},
			"required": ["path"]
		}`, &shared.ToolAnnotations{
			Title:           "File Info",
			ReadOnlyHint:    true,
			DestructiveHint: false,
			IdempotentHint:  true,
			OpenWorldHint:   false,
		}},
		{"mkdir", `{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute directory path to create (parent directories created automatically)"}
			},
			"required": ["path"]
		}`, &shared.ToolAnnotations{
			Title:           "Create Directory",
			ReadOnlyHint:    false,
			DestructiveHint: false,
			IdempotentHint:  true,
			OpenWorldHint:   false,
		}},
		{"roots", `{
			"type": "object",
			"properties": {}
		}`, &shared.ToolAnnotations{
			Title:           "Allowed Roots",
			ReadOnlyHint:    true,
			DestructiveHint: false,
			IdempotentHint:  true,
			OpenWorldHint:   false,
		}},
		{"tail", `{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute file path"},
				"lines": {"type": "integer", "description": "Number of lines to return (default 10)"}
			},
			"required": ["path"]
		}`, &shared.ToolAnnotations{
			Title:           "Last N Lines",
			ReadOnlyHint:    true,
			DestructiveHint: false,
			IdempotentHint:  true,
			OpenWorldHint:   false,
		}},
		{"tree", `{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute directory path"},
				"depth": {"type": "integer", "description": "Maximum depth (default 3)"},
				"pattern": {"type": "string", "description": "Glob pattern to filter files (directories always shown)"}
			},
			"required": ["path"]
		}`, &shared.ToolAnnotations{
			Title:           "Directory Tree",
			ReadOnlyHint:    true,
			DestructiveHint: false,
			IdempotentHint:  true,
			OpenWorldHint:   false,
		}},
		{"wc", `{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Absolute file path"}
			},
			"required": ["path"]
		}`, &shared.ToolAnnotations{
			Title:           "Word Count",
			ReadOnlyHint:    true,
			DestructiveHint: false,
			IdempotentHint:  true,
			OpenWorldHint:   false,
		}},
	}
	runSchema := fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "Executable path or name on PATH (e.g. powershell, python, cmd)"},
			"args": {"type": "string", "description": "All arguments as a single string"},
			"timeout": {"type": "integer", "description": "Inactivity timeout in seconds (default: %d for this tool, max: %d)"}
		},
		"required": ["command"]
	}`, runDefaultTimeout, maxTimeout)
	entries = append(entries, entry{"run", runSchema, &shared.ToolAnnotations{
		Title:           "Run Command",
		ReadOnlyHint:    false,
		DestructiveHint: true,
		IdempotentHint:  false,
		OpenWorldHint:   false,
	}})
	schemas := make([]shared.ToolSchema, 0, len(entries))
	for _, e := range entries {
		desc, ok := descriptions[e.name]
		if !ok {
			return nil, fmt.Errorf("missing builtin_descriptions entry for tool %q in config", e.name)
		}
		schemas = append(schemas, shared.ToolSchema{
			Name:        e.name,
			Description: desc,
			InputSchema: shared.RawJSON(e.schema),
			Annotations: e.annotations,
		})
	}
	return schemas, nil
}

// positionalEntry holds a positional argument with its configured order.
type positionalEntry struct {
	position int
	value    string
}

// DispatchExternalTool builds args and executes a configured external tool (§6.2).
func DispatchExternalTool(name string, cfg shared.ToolConfig, params map[string]interface{}, maxTimeout int) (string, error) {
	// Clone params to avoid mutating the caller's map.
	localParams := make(map[string]interface{}, len(params))
	for k, v := range params {
		localParams[k] = v
	}
	inactivityOverride := 0
	if t, ok := shared.OptionalInt(localParams, "timeout"); ok {
		inactivityOverride = ClampTimeout(t, maxTimeout)
		delete(localParams, "timeout")
	}
	effectiveCfg := cfg
	if inactivityOverride > 0 {
		effectiveCfg.InactivityTimeout = inactivityOverride
	}
	var args []string
	var positionals []positionalEntry
	for paramName, paramCfg := range cfg.Params {
		val, exists := localParams[paramName]
		if !exists {
			if paramCfg.Required {
				return "", fmt.Errorf("missing required parameter: %s", paramName)
			}
			val = paramCfg.Default
			if val == nil {
				continue
			}
		}
		if paramCfg.Flag != "" {
			switch paramCfg.Type {
			case "boolean":
				b, ok := val.(bool)
				if !ok {
					continue
				}
				if b {
					args = append(args, paramCfg.Flag)
				}
			case "string":
				s, ok := val.(string)
				if !ok {
					continue
				}
				args = append(args, paramCfg.Flag, s)
			case "integer":
				n, ok := val.(float64)
				if !ok {
					continue
				}
				args = append(args, paramCfg.Flag, fmt.Sprintf("%d", int(n)))
			}
		} else {
			var s string
			switch paramCfg.Type {
			case "string":
				if v, ok := val.(string); ok {
					s = v
				}
			case "integer":
				if v, ok := val.(float64); ok {
					s = fmt.Sprintf("%d", int(v))
				}
			case "boolean":
				continue
			default:
				continue
			}
			if s != "" {
				positionals = append(positionals, positionalEntry{
					position: paramCfg.Position,
					value:    s,
				})
			}
		}
	}
	sort.Slice(positionals, func(i, j int) bool {
		return positionals[i].position < positionals[j].position
	})
	for _, p := range positionals {
		args = append(args, p.value)
	}
	return ExecuteExternal(name, effectiveCfg, args, maxTimeout)
}

// ExternalToolSchema generates an MCP tool schema from a configured tool (§6.6).
func ExternalToolSchema(name string, cfg shared.ToolConfig, maxTimeout int) shared.ToolSchema {
	properties := make(map[string]interface{})
	var required []string
	for paramName, paramCfg := range cfg.Params {
		prop := map[string]interface{}{
			"type":        paramCfg.Type,
			"description": paramCfg.Description,
		}
		properties[paramName] = prop
		if paramCfg.Required {
			required = append(required, paramName)
		}
	}
	defaultTimeout := cfg.InactivityTimeout
	if defaultTimeout == 0 {
		defaultTimeout = shared.DefaultInactivityTimeout
	}
	properties["timeout"] = map[string]interface{}{
		"type":        "integer",
		"description": fmt.Sprintf("Inactivity timeout in seconds (default: %d for this tool, max: %d)", defaultTimeout, maxTimeout),
	}
	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	schemaJSON, _ := json.Marshal(schema)
	title := cfg.Title
	if title == "" {
		title = name
	}
	annotations := &shared.ToolAnnotations{
		Title:           title,
		ReadOnlyHint:    cfg.ReadOnly,
		DestructiveHint: cfg.Destructive,
		IdempotentHint:  cfg.Idempotent,
		OpenWorldHint:   false,
	}
	return shared.ToolSchema{
		Name:        name,
		Description: cfg.Description,
		InputSchema: schemaJSON,
		Annotations: annotations,
	}
}
