package installer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ErrConfigNotFound is returned when claude_desktop_config.json does not exist.
var ErrConfigNotFound = errors.New("config file not found")

// ReadClaudeConfig reads and parses claude_desktop_config.json (INS-17, INS-17a).
// Returns ErrConfigNotFound (wrapped) for non-existent file vs parse error.
func ReadClaudeConfig(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrConfigNotFound, path)
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

// IsNotExistError returns true if the error wraps ErrConfigNotFound.
func IsNotExistError(err error) bool {
	return errors.Is(err, ErrConfigNotFound)
}

// UpdateClaudeConfig adds or updates the WinMcpShim entry in the config (INS-18..INS-20).
// Pure function: returns modified config and the action taken.
func UpdateClaudeConfig(cfg map[string]interface{}, shimExe string, logDir string) (map[string]interface{}, ConfigAction) {
	servers, ok := cfg["mcpServers"]
	if !ok {
		// INS-18: add mcpServers
		cfg["mcpServers"] = map[string]interface{}{
			"WinMcpShim": shimEntry(shimExe, logDir),
		}
		return cfg, ActionAdded
	}
	serversMap, ok := servers.(map[string]interface{})
	if !ok {
		cfg["mcpServers"] = map[string]interface{}{
			"WinMcpShim": shimEntry(shimExe, logDir),
		}
		return cfg, ActionAdded
	}
	existing, ok := serversMap["WinMcpShim"]
	if !ok {
		// INS-19: add entry
		serversMap["WinMcpShim"] = shimEntry(shimExe, logDir)
		return cfg, ActionAdded
	}
	// INS-19a / INS-19b: check existing command path
	existingMap, ok := existing.(map[string]interface{})
	if ok {
		if cmd, ok := existingMap["command"].(string); ok && cmd == shimExe {
			return cfg, ActionSkipped
		}
	}
	serversMap["WinMcpShim"] = shimEntry(shimExe, logDir)
	return cfg, ActionUpdated
}

// RemoveShimEntry removes the WinMcpShim entry from mcpServers (UNS-06, UNS-06a).
// Returns the modified config and whether the entry was found.
func RemoveShimEntry(cfg map[string]interface{}) (map[string]interface{}, bool) {
	servers, ok := cfg["mcpServers"]
	if !ok {
		return cfg, false
	}
	serversMap, ok := servers.(map[string]interface{})
	if !ok {
		return cfg, false
	}
	if _, ok := serversMap["WinMcpShim"]; !ok {
		return cfg, false
	}
	delete(serversMap, "WinMcpShim")
	// UNS-06a: remove mcpServers if empty
	if len(serversMap) == 0 {
		delete(cfg, "mcpServers")
	}
	return cfg, true
}

// NewClaudeConfig creates a minimal config with just the WinMcpShim entry (INS-22).
func NewClaudeConfig(shimExe string, logDir string) map[string]interface{} {
	return map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"WinMcpShim": shimEntry(shimExe, logDir),
		},
	}
}

// MarshalConfig serialises the config as indented JSON (INS-21).
func MarshalConfig(cfg map[string]interface{}) ([]byte, error) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	data = append(data, '\n')
	return data, nil
}

// shimEntry builds the WinMcpShim server entry.
func shimEntry(shimExe string, logDir string) map[string]interface{} {
	return map[string]interface{}{
		"command": shimExe,
		"args":    []interface{}{"--log", logDir},
	}
}
