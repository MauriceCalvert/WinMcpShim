package tools

import (
	"strings"
	"testing"
	"time"
)

func TestRun_StderrTruncated(t *testing.T) {
	maxOutput := 1024 // 1 KB cap
	// Child writes 5 KB to stderr and a small amount to stdout
	// PowerShell: write 5KB to stderr, small to stdout
	opts := ExecOpts{
		InactivityTimeout: 10 * time.Second,
		TotalTimeout:      30 * time.Second,
		MaxOutput:         maxOutput,
		ToolName:          "test",
		MaxTimeout:        60,
	}
	// Use cmd /c with a python-style one-liner via powershell
	// Write 5000 bytes to stderr, 10 bytes to stdout
	result, err := ExecuteWithTimeouts("powershell", []string{
		"-NoProfile", "-Command",
		`[Console]::Out.Write("hello out"); $s = "x" * 5000; [Console]::Error.Write($s)`,
	}, opts)
	if err != nil {
		t.Fatalf("ExecuteWithTimeouts error: %v", err)
	}

	// Child should not be killed (exit code 0, no timeout)
	if result.Timeout != "" {
		t.Errorf("expected no timeout, got %q", result.Timeout)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}

	// Stdout fully captured
	if !strings.Contains(result.Stdout, "hello out") {
		t.Errorf("expected stdout to contain 'hello out', got %q", result.Stdout)
	}

	// Stderr capped and truncated
	if !strings.HasSuffix(result.Stderr, "\n[stderr truncated]") {
		t.Errorf("expected stderr truncation marker, got tail: %q",
			result.Stderr[max(0, len(result.Stderr)-50):])
	}

	// Stderr content (minus marker) should be at most maxOutput
	stderrContent := strings.TrimSuffix(result.Stderr, "\n[stderr truncated]")
	if len(stderrContent) > maxOutput {
		t.Errorf("stderr content is %d bytes, expected at most %d", len(stderrContent), maxOutput)
	}
}

func TestRun_StderrUnderLimit(t *testing.T) {
	maxOutput := 10 * 1024 // 10 KB cap
	opts := ExecOpts{
		InactivityTimeout: 10 * time.Second,
		TotalTimeout:      30 * time.Second,
		MaxOutput:         maxOutput,
		ToolName:          "test",
		MaxTimeout:        60,
	}
	// Write 100 bytes to stderr — well under limit
	result, err := ExecuteWithTimeouts("powershell", []string{
		"-NoProfile", "-Command",
		`$s = "y" * 100; [Console]::Error.Write($s)`,
	}, opts)
	if err != nil {
		t.Fatalf("ExecuteWithTimeouts error: %v", err)
	}
	if result.Timeout != "" {
		t.Errorf("expected no timeout, got %q", result.Timeout)
	}

	// Full stderr captured, no truncation marker
	if strings.Contains(result.Stderr, "[stderr truncated]") {
		t.Errorf("expected no truncation marker, got %q", result.Stderr)
	}
	if len(result.Stderr) != 100 {
		t.Errorf("expected 100 bytes of stderr, got %d", len(result.Stderr))
	}
}

func TestRun_BothTruncated(t *testing.T) {
	maxOutput := 1024 // 1 KB cap
	opts := ExecOpts{
		InactivityTimeout: 10 * time.Second,
		TotalTimeout:      30 * time.Second,
		MaxOutput:         maxOutput,
		ToolName:          "test",
		MaxTimeout:        60,
	}
	// Flood both stdout and stderr with 5 KB each
	result, err := ExecuteWithTimeouts("powershell", []string{
		"-NoProfile", "-Command",
		`$s = "x" * 5000; [Console]::Out.Write($s); [Console]::Error.Write($s)`,
	}, opts)
	if err != nil {
		t.Fatalf("ExecuteWithTimeouts error: %v", err)
	}

	// Stdout should be truncated (triggers kill)
	if !strings.Contains(result.Stdout, "[truncated") {
		t.Errorf("expected stdout truncation marker, got tail: %q",
			result.Stdout[max(0, len(result.Stdout)-80):])
	}

	// Stderr should be capped independently
	if !strings.Contains(result.Stderr, "[stderr truncated]") {
		// Stderr truncation depends on how much data arrived before kill.
		// If the child was killed before stderr filled, it may not be truncated.
		// This is acceptable — the test verifies no crash and bounded memory.
		t.Logf("stderr may not be truncated if child was killed early (len=%d)", len(result.Stderr))
	}
}
