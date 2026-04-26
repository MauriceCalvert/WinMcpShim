package shared

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the top-level shim configuration.
type Config struct {
	Security            SecurityConfig        `toml:"security"`
	ScanDirs            ScanDirsConfig        `toml:"scan_dirs"`
	Run                 RunConfig             `toml:"run"`
	BuiltinDescriptions map[string]string     `toml:"builtin_descriptions"`
	Tools               map[string]ToolConfig `toml:"tools"`
}

// SecurityConfig holds path confinement and safety limit settings.
type SecurityConfig struct {
	AllowedRoots []string `toml:"allowed_roots"`
	MaxTimeout   int      `toml:"max_timeout"`
}

// ScanDirsConfig holds directories for --scan discovery.
type ScanDirsConfig struct {
	Paths []string `toml:"paths"`
}

// RunConfig holds defaults for the run built-in tool.
type RunConfig struct {
	InactivityTimeout int      `toml:"inactivity_timeout"`
	TotalTimeout      int      `toml:"total_timeout"`
	MaxOutput         int      `toml:"max_output"`
	AllowedCommands   []string `toml:"allowed_commands"`
}

// ToolConfig defines a configured external tool.
type ToolConfig struct {
	Exe               string                 `toml:"exe"`
	Description       string                 `toml:"description"`
	InactivityTimeout int                    `toml:"inactivity_timeout"`
	TotalTimeout      int                    `toml:"total_timeout"`
	MaxOutput         int                    `toml:"max_output"`
	SuccessCodes      []int                  `toml:"success_codes"`
	Params            map[string]ParamConfig `toml:"params"`
	Title             string                 `toml:"title"`
	ReadOnly          bool                   `toml:"read_only"`
	Destructive       bool                   `toml:"destructive"`
	Idempotent        bool                   `toml:"idempotent"`
}

// ParamConfig defines a parameter for a configured tool.
type ParamConfig struct {
	Type        string      `toml:"type"`
	Description string      `toml:"description"`
	Required    bool        `toml:"required"`
	Default     interface{} `toml:"default"`
	Flag        string      `toml:"flag"`
	Position    int         `toml:"position"`
}

// DefaultRunConfig returns default values for the run tool.
func DefaultRunConfig() RunConfig {
	return RunConfig{
		InactivityTimeout: DefaultInactivityTimeout,
		TotalTimeout:      DefaultTotalTimeout,
		MaxOutput:         DefaultMaxOutput,
	}
}

// ConfigPath returns the config file path adjacent to the executable.
func ConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "shim.toml"
	}
	return filepath.Join(filepath.Dir(exe), "shim.toml")
}

// LoadConfig loads the TOML config from the given path.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := &Config{
				Run:                 DefaultRunConfig(),
				Security:            SecurityConfig{MaxTimeout: DefaultMaxTimeout},
				BuiltinDescriptions: DefaultBuiltinDescriptions(),
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if len(data) >= 2 {
		if (data[0] == 0xFF && data[1] == 0xFE) || (data[0] == 0xFE && data[1] == 0xFF) {
			return nil, fmt.Errorf("Config file must be saved as UTF-8")
		}
	}
	var cfg Config
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.Run.InactivityTimeout == 0 {
		cfg.Run.InactivityTimeout = DefaultInactivityTimeout
	}
	if cfg.Run.TotalTimeout == 0 {
		cfg.Run.TotalTimeout = DefaultTotalTimeout
	}
	if cfg.Run.MaxOutput == 0 {
		cfg.Run.MaxOutput = DefaultMaxOutput
	}
	if cfg.Security.MaxTimeout == 0 {
		cfg.Security.MaxTimeout = DefaultMaxTimeout
	}
	if len(cfg.BuiltinDescriptions) == 0 {
		cfg.BuiltinDescriptions = DefaultBuiltinDescriptions()
	}
	if err := ValidateToolConfigs(cfg.Tools); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ValidateToolConfigs checks that every external tool parameter has exactly
// one of flag or position, not both and not neither (§6.1).
func ValidateToolConfigs(tools map[string]ToolConfig) error {
	for name, tc := range tools {
		for pname, pc := range tc.Params {
			hasFlag := pc.Flag != ""
			hasPos := pc.Position != 0
			if hasFlag && hasPos {
				return fmt.Errorf("tool %q param %q has both flag and position", name, pname)
			}
			if !hasFlag && !hasPos {
				return fmt.Errorf("tool %q param %q has neither flag nor position", name, pname)
			}
		}
	}
	return nil
}

// DefaultBuiltinDescriptions returns hardcoded descriptions for when no config file exists (§9.13).
func DefaultBuiltinDescriptions() map[string]string {
	return map[string]string{
		"cat":    "Read and concatenate one or more files. Pass multiple space-separated paths or a JSON array.",
		"copy":   "Copy a file or directory tree.",
		"delete": "Delete a single file or empty directory.",
		"diff":   "Compare two text files and show unified diff.",
		"edit":   "Find-and-replace a unique string in a text file.",
		"head":   "Show the first N lines of a file (default 10).",
		"info":   "Get file or directory metadata: size, timestamps, type.",
		"list":   "List directory contents with optional glob filter.",
		"mkdir":  "Create a directory (and parent directories).",
		"move":   "Move or rename a file or directory.",
		"read":   "Read a text file. Returns content as a string. Use offset/limit for large files.",
		"roots":  "Return the list of allowed root directories.",
		"run":    "Execute a command. Executables inside allowed_roots may always be invoked. If run.allowed_commands is configured, listed commands resolved at startup to absolute PATH locations are also permitted (exact path match).",
		"search": "Recursively search for files matching a glob pattern.",
		"tail":   "Show the last N lines of a file (default 10).",
		"tree":   "Recursive indented directory listing with depth limit.",
		"wc":     "Count lines, words, and bytes in a file.",
		"write":  "Create or overwrite a text file. Atomic write via temp file + rename.",
		"grep":   "Search inside file contents by regex. Uses Go RE2 regex syntax (not GNU grep BRE): unescaped (, ), +, ? are metacharacters. No backreferences. Set recursive=true + include to search a directory tree.",
	}
}
