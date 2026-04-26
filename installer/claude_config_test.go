package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// T-50: ReadClaudeConfig parses valid JSON (INS-17).
func TestReadClaudeConfig_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte(`{"mcpServers":{}}`), 0644)
	cfg, err := ReadClaudeConfig(path)
	if err != nil {
		t.Fatalf("ReadClaudeConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("config is nil")
	}
}

// T-51: ReadClaudeConfig returns error with detail for malformed JSON (INS-17a).
func TestReadClaudeConfig_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte(`{bad json`), 0644)
	_, err := ReadClaudeConfig(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

// T-52: ReadClaudeConfig returns error for JSON with trailing comma (INS-17a).
func TestReadClaudeConfig_TrailingComma(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte(`{"a": 1,}`), 0644)
	_, err := ReadClaudeConfig(path)
	if err == nil {
		t.Fatal("expected error for trailing comma")
	}
}

// T-53: ReadClaudeConfig returns specific error for non-existent file (INS-22, UNS-01).
func TestReadClaudeConfig_NonExistent(t *testing.T) {
	_, err := ReadClaudeConfig(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	if !IsNotExistError(err) {
		t.Errorf("expected not-exist error, got: %v", err)
	}
}

// T-54: UpdateClaudeConfig adds mcpServers when absent (INS-18).
func TestUpdateClaudeConfig_AddsMcpServers(t *testing.T) {
	cfg := map[string]interface{}{"preferences": map[string]interface{}{"foo": true}}
	result, action := UpdateClaudeConfig(cfg, `D:\shim\winmcpshim.exe`, `D:\logs\shim`)
	if action != ActionAdded {
		t.Errorf("action = %d, want ActionAdded", action)
	}
	servers := result["mcpServers"].(map[string]interface{})
	if _, ok := servers["WinMcpShim"]; !ok {
		t.Error("WinMcpShim entry not added")
	}
}

// T-55: UpdateClaudeConfig adds WinMcpShim when mcpServers exists but entry absent (INS-19).
func TestUpdateClaudeConfig_AddsEntry(t *testing.T) {
	cfg := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"other": map[string]interface{}{"command": "other.exe"},
		},
	}
	result, action := UpdateClaudeConfig(cfg, `D:\shim\winmcpshim.exe`, `D:\logs`)
	if action != ActionAdded {
		t.Errorf("action = %d, want ActionAdded", action)
	}
	servers := result["mcpServers"].(map[string]interface{})
	if _, ok := servers["WinMcpShim"]; !ok {
		t.Error("WinMcpShim entry not added")
	}
	if _, ok := servers["other"]; !ok {
		t.Error("other entry was removed")
	}
}

// T-56: UpdateClaudeConfig returns ActionSkipped when entry exists with correct path (INS-19a).
func TestUpdateClaudeConfig_Skipped(t *testing.T) {
	shimExe := `D:\shim\winmcpshim.exe`
	cfg := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"WinMcpShim": map[string]interface{}{"command": shimExe},
		},
	}
	_, action := UpdateClaudeConfig(cfg, shimExe, `D:\logs`)
	if action != ActionSkipped {
		t.Errorf("action = %d, want ActionSkipped", action)
	}
}

// T-57: UpdateClaudeConfig returns ActionUpdated when entry exists with different path (INS-19b).
func TestUpdateClaudeConfig_Updated(t *testing.T) {
	cfg := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"WinMcpShim": map[string]interface{}{"command": `C:\old\shim.exe`},
		},
	}
	_, action := UpdateClaudeConfig(cfg, `D:\new\winmcpshim.exe`, `D:\logs`)
	if action != ActionUpdated {
		t.Errorf("action = %d, want ActionUpdated", action)
	}
}

// T-58: UpdateClaudeConfig preserves other mcpServers entries (INS-20).
func TestUpdateClaudeConfig_PreservesOtherServers(t *testing.T) {
	cfg := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"filesystem": map[string]interface{}{"command": "fs.exe"},
		},
	}
	result, _ := UpdateClaudeConfig(cfg, `D:\shim\winmcpshim.exe`, `D:\logs`)
	servers := result["mcpServers"].(map[string]interface{})
	if _, ok := servers["filesystem"]; !ok {
		t.Error("filesystem entry was removed")
	}
}

// T-59: UpdateClaudeConfig preserves non-mcpServers properties (INS-20).
func TestUpdateClaudeConfig_PreservesOtherProperties(t *testing.T) {
	cfg := map[string]interface{}{
		"preferences": map[string]interface{}{"theme": "dark"},
		"mcpServers":  map[string]interface{}{},
	}
	result, _ := UpdateClaudeConfig(cfg, `D:\shim\winmcpshim.exe`, `D:\logs`)
	prefs := result["preferences"].(map[string]interface{})
	if prefs["theme"] != "dark" {
		t.Error("preferences.theme was modified")
	}
}

// T-60: UpdateClaudeConfig preserves nested object depth (INS-20).
func TestUpdateClaudeConfig_PreservesNestedDepth(t *testing.T) {
	cfg := map[string]interface{}{
		"deep": map[string]interface{}{
			"nested": map[string]interface{}{
				"value": float64(42),
			},
		},
	}
	result, _ := UpdateClaudeConfig(cfg, `D:\shim\winmcpshim.exe`, `D:\logs`)
	deep := result["deep"].(map[string]interface{})
	nested := deep["nested"].(map[string]interface{})
	if nested["value"] != float64(42) {
		t.Error("nested value was modified")
	}
}

// T-61: NewClaudeConfig produces valid JSON with correct structure (INS-22).
func TestNewClaudeConfig_Structure(t *testing.T) {
	cfg := NewClaudeConfig(`D:\shim\winmcpshim.exe`, `D:\logs`)
	data, err := MarshalConfig(cfg)
	if err != nil {
		t.Fatalf("MarshalConfig: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	servers := parsed["mcpServers"].(map[string]interface{})
	entry := servers["WinMcpShim"].(map[string]interface{})
	if entry["command"] != `D:\shim\winmcpshim.exe` {
		t.Errorf("command = %v", entry["command"])
	}
}

// T-62: MarshalConfig output round-trips through json.Unmarshal (INS-21).
func TestMarshalConfig_RoundTrip(t *testing.T) {
	cfg := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"WinMcpShim": map[string]interface{}{
				"command": `D:\shim\winmcpshim.exe`,
				"args":    []interface{}{"--log", `D:\logs`},
			},
		},
	}
	data, err := MarshalConfig(cfg)
	if err != nil {
		t.Fatalf("MarshalConfig: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("round-trip Unmarshal: %v", err)
	}
}

// T-63: RemoveShimEntry removes WinMcpShim and preserves other entries (UNS-06).
func TestRemoveShimEntry_PreservesOthers(t *testing.T) {
	cfg := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"WinMcpShim": map[string]interface{}{"command": "shim.exe"},
			"other":      map[string]interface{}{"command": "other.exe"},
		},
	}
	result, found := RemoveShimEntry(cfg)
	if !found {
		t.Error("expected found=true")
	}
	servers := result["mcpServers"].(map[string]interface{})
	if _, ok := servers["WinMcpShim"]; ok {
		t.Error("WinMcpShim still present")
	}
	if _, ok := servers["other"]; !ok {
		t.Error("other entry was removed")
	}
}

// T-64: RemoveShimEntry removes mcpServers key when it becomes empty (UNS-06a).
func TestRemoveShimEntry_RemovesEmptyMcpServers(t *testing.T) {
	cfg := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"WinMcpShim": map[string]interface{}{"command": "shim.exe"},
		},
	}
	result, found := RemoveShimEntry(cfg)
	if !found {
		t.Error("expected found=true")
	}
	if _, ok := result["mcpServers"]; ok {
		t.Error("mcpServers should have been removed when empty")
	}
}

// T-65: RemoveShimEntry returns false when entry not present (UNS-06).
func TestRemoveShimEntry_NotPresent(t *testing.T) {
	cfg := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"other": map[string]interface{}{"command": "other.exe"},
		},
	}
	_, found := RemoveShimEntry(cfg)
	if found {
		t.Error("expected found=false")
	}
}

// T-66: RemoveShimEntry on config with no mcpServers returns false (UNS-03).
func TestRemoveShimEntry_NoMcpServers(t *testing.T) {
	cfg := map[string]interface{}{
		"preferences": map[string]interface{}{"theme": "dark"},
	}
	_, found := RemoveShimEntry(cfg)
	if found {
		t.Error("expected found=false")
	}
}
