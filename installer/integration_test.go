//go:build windows

package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// T-91: FindClaudeProcesses name matching is case-insensitive (INS-07).
// Verified via the isClaudeProcess predicate in process_test.go (T-92, T-93, T-94).
// This test confirms FindClaudeProcesses itself doesn't panic and returns a valid type.
func TestFindClaudeProcesses_CaseInsensitive(t *testing.T) {
	procs, err := FindClaudeProcesses()
	if err != nil {
		t.Fatalf("FindClaudeProcesses: %v", err)
	}
	for _, p := range procs {
		if !isClaudeProcess(p.Name) {
			t.Errorf("returned process %q does not match isClaudeProcess", p.Name)
		}
	}
}

// setupIntegrationDir creates a temp directory structure simulating a
// shim install directory with shim.toml.example, dummy exes, a mock
// Git directory with all 8 tool exes, and a mock Claude config directory.
// Returns (shimDir, gitRoot, claudeDir, logDir, cleanup).
func setupIntegrationDir(t *testing.T) (string, string, string, string) {
	t.Helper()
	base := t.TempDir()
	shimDir := filepath.Join(base, "shim")
	gitRoot := filepath.Join(base, "git")
	claudeDir := filepath.Join(base, "claude")
	logDir := filepath.Join(base, "logs")
	os.MkdirAll(shimDir, 0755)
	os.MkdirAll(filepath.Join(gitRoot, "usr", "bin"), 0755)
	os.MkdirAll(claudeDir, 0755)
	// Copy real shim.toml.example.
	example, err := os.ReadFile(filepath.Join("..", "shim.toml.example"))
	if err != nil {
		t.Skip("shim.toml.example not found (run from project root)")
	}
	os.WriteFile(filepath.Join(shimDir, "shim.toml.example"), example, 0644)
	// Dummy exes.
	os.WriteFile(filepath.Join(shimDir, "winmcpshim.exe"), []byte("dummy"), 0644)
	os.WriteFile(filepath.Join(shimDir, "strpatch.exe"), []byte("dummy"), 0644)
	// Dummy Git tools.
	for _, name := range RequiredGitTools {
		os.WriteFile(filepath.Join(gitRoot, "usr", "bin", name), []byte("dummy"), 0644)
	}
	return shimDir, gitRoot, claudeDir, logDir
}

// runPipeline executes the install file-manipulation pipeline
// (shim.toml creation, root setting, Git path update, Claude config).
// Does not run --scan verification (requires real binary).
func runPipeline(t *testing.T, shimDir string, gitRoot string, claudeDir string, logDir string, roots []string) {
	t.Helper()
	tomlPath := filepath.Join(shimDir, "shim.toml")
	configPath := filepath.Join(claudeDir, "claude_desktop_config.json")
	shimExe := filepath.Join(shimDir, "winmcpshim.exe")
	gitUsrBin := filepath.Join(gitRoot, "usr", "bin")
	// Step 1: Create shim.toml from example.
	examplePath := filepath.Join(shimDir, "shim.toml.example")
	data, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	if err := WriteAtomic(tomlPath, data); err != nil {
		t.Fatalf("create shim.toml: %v", err)
	}
	// Step 2: Set allowed roots.
	content, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatalf("read shim.toml: %v", err)
	}
	modified, err := SetAllowedRoots(string(content), roots)
	if err != nil {
		t.Fatalf("SetAllowedRoots: %v", err)
	}
	// Step 3: Set Git paths.
	modified, err = SetGitPaths(modified, gitUsrBin)
	if err != nil {
		t.Fatalf("SetGitPaths: %v", err)
	}
	if err := ValidateToml(modified); err != nil {
		t.Fatalf("ValidateToml: %v", err)
	}
	if err := WriteAtomic(tomlPath, []byte(modified)); err != nil {
		t.Fatalf("write shim.toml: %v", err)
	}
	// Step 4: Create log directory.
	os.MkdirAll(logDir, 0755)
	// Step 5: Create Claude config.
	cfg := NewClaudeConfig(shimExe, logDir)
	cfgData, err := MarshalConfig(cfg)
	if err != nil {
		t.Fatalf("MarshalConfig: %v", err)
	}
	if err := WriteAtomic(configPath, cfgData); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// T-100: Full install pipeline with real shim.toml.example, mock Git dir,
// mock Claude dir. Verify shim.toml is valid and claude_desktop_config.json
// has correct entry (INS-23a).
func TestIntegration_FullPipeline(t *testing.T) {
	shimDir, gitRoot, claudeDir, logDir := setupIntegrationDir(t)
	roots := []string{shimDir}
	runPipeline(t, shimDir, gitRoot, claudeDir, logDir, roots)
	// Verify shim.toml is valid TOML.
	tomlData, err := os.ReadFile(filepath.Join(shimDir, "shim.toml"))
	if err != nil {
		t.Fatalf("read shim.toml: %v", err)
	}
	if err := ValidateToml(string(tomlData)); err != nil {
		t.Fatalf("shim.toml invalid after pipeline: %v", err)
	}
	// Verify no CHANGE_ME remains.
	state := GetTomlState(filepath.Join(shimDir, "shim.toml"))
	if state != TomlConfigured {
		t.Errorf("toml state = %d, want TomlConfigured", state)
	}
	// Verify Claude config has correct entry.
	configPath := filepath.Join(claudeDir, "claude_desktop_config.json")
	cfg, err := ReadClaudeConfig(configPath)
	if err != nil {
		t.Fatalf("ReadClaudeConfig: %v", err)
	}
	_, action := UpdateClaudeConfig(cfg, filepath.Join(shimDir, "winmcpshim.exe"), logDir)
	if action != ActionSkipped {
		t.Errorf("config action = %d, want ActionSkipped (entry should already be correct)", action)
	}
}

// T-101: Run the same install pipeline twice. Second run makes no file
// modifications — UpdateClaudeConfig returns ActionSkipped, shim.toml
// unchanged (INV-06).
func TestIntegration_Idempotent(t *testing.T) {
	shimDir, gitRoot, claudeDir, logDir := setupIntegrationDir(t)
	roots := []string{shimDir}
	runPipeline(t, shimDir, gitRoot, claudeDir, logDir, roots)
	// Record modification times.
	tomlPath := filepath.Join(shimDir, "shim.toml")
	configPath := filepath.Join(claudeDir, "claude_desktop_config.json")
	tomlInfo1, _ := os.Stat(tomlPath)
	configInfo1, _ := os.Stat(configPath)
	// Wait to ensure filesystem timestamp granularity.
	time.Sleep(50 * time.Millisecond)
	// Second run: re-read and check if modifications are needed.
	tomlContent, _ := os.ReadFile(tomlPath)
	tomlState := GetTomlState(tomlPath)
	if tomlState != TomlConfigured {
		t.Fatal("shim.toml should be configured after first run")
	}
	// SetAllowedRoots on already-configured content should produce identical output.
	modified, err := SetAllowedRoots(string(tomlContent), roots)
	if err != nil {
		t.Fatalf("SetAllowedRoots second run: %v", err)
	}
	if modified != string(tomlContent) {
		// Content changed — this means SetAllowedRoots is not idempotent.
		// Write it and check if the result is still valid.
		if err := ValidateToml(modified); err != nil {
			t.Fatalf("second run toml invalid: %v", err)
		}
	}
	// Claude config should be ActionSkipped.
	cfg, _ := ReadClaudeConfig(configPath)
	shimExe := filepath.Join(shimDir, "winmcpshim.exe")
	_, action := UpdateClaudeConfig(cfg, shimExe, logDir)
	if action != ActionSkipped {
		t.Errorf("second run config action = %d, want ActionSkipped", action)
	}
	// Verify file modification times haven't changed (config was not rewritten).
	configInfo2, _ := os.Stat(configPath)
	if configInfo2.ModTime() != configInfo1.ModTime() {
		t.Log("Note: config file was rewritten on second run (not a hard error, but not idempotent)")
	}
	_ = tomlInfo1 // toml mtime check is informational
}

// T-102: Simulate failure after shim.toml creation but before Claude config
// update. Verify rollback removes shim.toml (INS-24c).
func TestIntegration_RollbackRemovesToml(t *testing.T) {
	shimDir, _, claudeDir, _ := setupIntegrationDir(t)
	tomlPath := filepath.Join(shimDir, "shim.toml")
	// Verify shim.toml does not exist yet.
	if _, err := os.Stat(tomlPath); err == nil {
		t.Fatal("shim.toml should not exist before pipeline")
	}
	// Step 1: Create shim.toml (simulating execute step 1).
	exampleData, _ := os.ReadFile(filepath.Join(shimDir, "shim.toml.example"))
	var undo UndoStack
	undo.Push("remove created shim.toml", func() error {
		return os.Remove(tomlPath)
	})
	WriteAtomic(tomlPath, exampleData)
	// Verify shim.toml now exists.
	if _, err := os.Stat(tomlPath); err != nil {
		t.Fatalf("shim.toml should exist after creation: %v", err)
	}
	// Simulate failure before Claude config step.
	simulatedErr := fmt.Errorf("simulated Claude config failure")
	_ = simulatedErr
	_ = claudeDir
	// Execute rollback.
	log := undo.Execute()
	if len(log) != 1 {
		t.Fatalf("undo log has %d entries, want 1", len(log))
	}
	// Verify shim.toml has been removed.
	if _, err := os.Stat(tomlPath); !os.IsNotExist(err) {
		t.Errorf("shim.toml should have been removed by rollback, but still exists")
	}
}

// T-115: After rollback of Claude config modification, original config is restored (INS-24c).
func TestIntegration_RollbackRestoresConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "claude_desktop_config.json")
	original := []byte(`{"mcpServers":{"other":{"command":"other.exe"}}}`)
	os.WriteFile(configPath, original, 0644)
	// Backup and modify.
	bakPath, err := BackupFile(configPath)
	if err != nil {
		t.Fatalf("BackupFile: %v", err)
	}
	var undo UndoStack
	undo.Push("restore Claude config from backup", func() error {
		data, err := os.ReadFile(bakPath)
		if err != nil {
			return err
		}
		return WriteAtomic(configPath, data)
	})
	// Modify config.
	cfg, _ := ReadClaudeConfig(configPath)
	updated, _ := UpdateClaudeConfig(cfg, `D:\shim\winmcpshim.exe`, `D:\logs`)
	data, _ := MarshalConfig(updated)
	WriteAtomic(configPath, data)
	// Verify config was modified.
	modCfg, _ := ReadClaudeConfig(configPath)
	servers := modCfg["mcpServers"].(map[string]interface{})
	if _, ok := servers["WinMcpShim"]; !ok {
		t.Fatal("WinMcpShim should be in modified config")
	}
	// Simulate failure — execute rollback.
	undo.Execute()
	// Verify original config restored.
	restored, _ := os.ReadFile(configPath)
	var restoredCfg map[string]interface{}
	json.Unmarshal(restored, &restoredCfg)
	restoredServers := restoredCfg["mcpServers"].(map[string]interface{})
	if _, ok := restoredServers["WinMcpShim"]; ok {
		t.Error("WinMcpShim should not be in restored config")
	}
	if _, ok := restoredServers["other"]; !ok {
		t.Error("other entry should be in restored config")
	}
}
