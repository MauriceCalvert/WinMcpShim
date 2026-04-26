//go:build windows

package installer

import "testing"

// T-90: FindClaudeProcesses returns empty when no Claude processes running (INS-07).
// Integration test — may find real processes on dev machine; asserts no panic and valid return type.
func TestFindClaudeProcesses_NoPanic(t *testing.T) {
	procs, err := FindClaudeProcesses()
	if err != nil {
		t.Fatalf("FindClaudeProcesses: %v", err)
	}
	// Just verify it returns a valid slice (may be empty or non-empty).
	_ = procs
}

// T-92: isClaudeProcess("Claude.exe") returns true (INS-07).
func TestIsClaudeProcess_MixedCase(t *testing.T) {
	if !isClaudeProcess("Claude.exe") {
		t.Error("expected true for Claude.exe")
	}
}

// T-93: isClaudeProcess("claude.exe") returns true (INS-07).
func TestIsClaudeProcess_LowerCase(t *testing.T) {
	if !isClaudeProcess("claude.exe") {
		t.Error("expected true for claude.exe")
	}
}

// T-94: isClaudeProcess("CLAUDE.EXE") returns true (INS-07).
func TestIsClaudeProcess_UpperCase(t *testing.T) {
	if !isClaudeProcess("CLAUDE.EXE") {
		t.Error("expected true for CLAUDE.EXE")
	}
}

// T-95: isClaudeProcess("explorer.exe") returns false (INS-07).
func TestIsClaudeProcess_Explorer(t *testing.T) {
	if isClaudeProcess("explorer.exe") {
		t.Error("expected false for explorer.exe")
	}
}

// T-96: isClaudeProcess("claude-helper.exe") returns false — exact match, not prefix (INS-07).
func TestIsClaudeProcess_Helper(t *testing.T) {
	if isClaudeProcess("claude-helper.exe") {
		t.Error("expected false for claude-helper.exe")
	}
}

// T-97: isClaudeProcess("claudeNOT.exe") returns false — exact match, not prefix (INS-07).
func TestIsClaudeProcess_ClaudeNot(t *testing.T) {
	if isClaudeProcess("claudeNOT.exe") {
		t.Error("expected false for claudeNOT.exe")
	}
}

// T-98: isShimProcess("winmcpshim.exe") returns true (INS-07c).
func TestIsShimProcess_LowerCase(t *testing.T) {
	if !isShimProcess("winmcpshim.exe") {
		t.Error("expected true for winmcpshim.exe")
	}
}

// T-99: isShimProcess("WinMcpShim.exe") returns true — case-insensitive (INS-07c).
func TestIsShimProcess_MixedCase(t *testing.T) {
	if !isShimProcess("WinMcpShim.exe") {
		t.Error("expected true for WinMcpShim.exe")
	}
}

// T-99a: isShimProcess("claude.exe") returns false (INS-07c).
func TestIsShimProcess_Claude(t *testing.T) {
	if isShimProcess("claude.exe") {
		t.Error("expected false for claude.exe")
	}
}

// T-99b: isClaudeProcess("conhost.exe") returns false (INS-07).
func TestIsClaudeProcess_Conhost(t *testing.T) {
	if isClaudeProcess("conhost.exe") {
		t.Error("expected false for conhost.exe")
	}
}

// T-99c: isClaudeProcess("winmcpshim.exe") returns false (INS-07).
func TestIsClaudeProcess_Shim(t *testing.T) {
	if isClaudeProcess("winmcpshim.exe") {
		t.Error("expected false for winmcpshim.exe")
	}
}
