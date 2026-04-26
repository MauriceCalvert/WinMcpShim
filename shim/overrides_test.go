package main

import (
	"os"
	"reflect"
	"testing"

	"github.com/MauriceCalvert/WinMcpShim/shared"
)

func TestApplyEnvOverrides_AllowedRoots(t *testing.T) {
	t.Setenv("WINMCPSHIM_ALLOWED_ROOTS", `D:\one;C:\Users\bob`)
	cfg := &shared.Config{}
	applyEnvOverrides(cfg)
	want := []string{`D:\one`, `C:\Users\bob`}
	if !reflect.DeepEqual(cfg.Security.AllowedRoots, want) {
		t.Errorf("AllowedRoots = %v, want %v", cfg.Security.AllowedRoots, want)
	}
}

func TestApplyEnvOverrides_AllowedCommands(t *testing.T) {
	t.Setenv("WINMCPSHIM_ALLOWED_COMMANDS", "git, python ,npm")
	cfg := &shared.Config{}
	applyEnvOverrides(cfg)
	want := []string{"git", "python", "npm"}
	if !reflect.DeepEqual(cfg.Run.AllowedCommands, want) {
		t.Errorf("AllowedCommands = %v, want %v", cfg.Run.AllowedCommands, want)
	}
}

func TestApplyEnvOverrides_EmptyLeavesTomlIntact(t *testing.T) {
	os.Unsetenv("WINMCPSHIM_ALLOWED_ROOTS")
	os.Unsetenv("WINMCPSHIM_ALLOWED_COMMANDS")
	os.Unsetenv("WINMCPSHIM_MAX_TIMEOUT")
	cfg := &shared.Config{
		Security: shared.SecurityConfig{
			AllowedRoots: []string{`D:\preserved`},
			MaxTimeout:   42,
		},
		Run: shared.RunConfig{AllowedCommands: []string{"existing"}},
	}
	applyEnvOverrides(cfg)
	if !reflect.DeepEqual(cfg.Security.AllowedRoots, []string{`D:\preserved`}) {
		t.Errorf("AllowedRoots clobbered: %v", cfg.Security.AllowedRoots)
	}
	if !reflect.DeepEqual(cfg.Run.AllowedCommands, []string{"existing"}) {
		t.Errorf("AllowedCommands clobbered: %v", cfg.Run.AllowedCommands)
	}
	if cfg.Security.MaxTimeout != 42 {
		t.Errorf("MaxTimeout clobbered: %d", cfg.Security.MaxTimeout)
	}
}

func TestApplyEnvOverrides_MaxTimeoutValid(t *testing.T) {
	t.Setenv("WINMCPSHIM_MAX_TIMEOUT", "120")
	cfg := &shared.Config{Security: shared.SecurityConfig{MaxTimeout: 60}}
	applyEnvOverrides(cfg)
	if cfg.Security.MaxTimeout != 120 {
		t.Errorf("MaxTimeout = %d, want 120", cfg.Security.MaxTimeout)
	}
}

func TestApplyEnvOverrides_MaxTimeoutInvalid(t *testing.T) {
	t.Setenv("WINMCPSHIM_MAX_TIMEOUT", "not-a-number")
	cfg := &shared.Config{Security: shared.SecurityConfig{MaxTimeout: 60}}
	applyEnvOverrides(cfg)
	if cfg.Security.MaxTimeout != 60 {
		t.Errorf("MaxTimeout = %d, want 60 (unchanged)", cfg.Security.MaxTimeout)
	}
}

func TestApplyFlagOverrides_AllowedRoots(t *testing.T) {
	cfg := &shared.Config{}
	applyFlagOverrides(cfg, cliFlags{
		rootsSet:     true,
		allowedRoots: []string{`D:\one`, `C:\Users\bob`},
	})
	want := []string{`D:\one`, `C:\Users\bob`}
	if !reflect.DeepEqual(cfg.Security.AllowedRoots, want) {
		t.Errorf("AllowedRoots = %v, want %v", cfg.Security.AllowedRoots, want)
	}
}

func TestApplyFlagOverrides_AllowedCommands(t *testing.T) {
	cfg := &shared.Config{}
	applyFlagOverrides(cfg, cliFlags{
		commandsSet:     true,
		allowedCommands: []string{`C:\Windows\System32\cmd.exe`, `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`},
	})
	want := []string{`C:\Windows\System32\cmd.exe`, `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`}
	if !reflect.DeepEqual(cfg.Run.AllowedCommands, want) {
		t.Errorf("AllowedCommands = %v, want %v", cfg.Run.AllowedCommands, want)
	}
}

func TestApplyFlagOverrides_MaxTimeout(t *testing.T) {
	cfg := &shared.Config{Security: shared.SecurityConfig{MaxTimeout: 60}}
	applyFlagOverrides(cfg, cliFlags{timeoutSet: true, maxTimeout: "120"})
	if cfg.Security.MaxTimeout != 120 {
		t.Errorf("MaxTimeout = %d, want 120", cfg.Security.MaxTimeout)
	}
}

func TestApplyFlagOverrides_EmptyLeavesTomlIntact(t *testing.T) {
	cfg := &shared.Config{
		Security: shared.SecurityConfig{
			AllowedRoots: []string{`D:\preserved`},
			MaxTimeout:   42,
		},
		Run: shared.RunConfig{AllowedCommands: []string{"existing"}},
	}
	// Flags present but empty (mcpb passes zero tokens when a multi-value
	// user_config field has its default empty list).
	applyFlagOverrides(cfg, cliFlags{
		rootsSet: true, allowedRoots: nil,
		commandsSet: true, allowedCommands: nil,
		timeoutSet: true, maxTimeout: "",
	})
	if !reflect.DeepEqual(cfg.Security.AllowedRoots, []string{`D:\preserved`}) {
		t.Errorf("AllowedRoots clobbered: %v", cfg.Security.AllowedRoots)
	}
	if !reflect.DeepEqual(cfg.Run.AllowedCommands, []string{"existing"}) {
		t.Errorf("AllowedCommands clobbered: %v", cfg.Run.AllowedCommands)
	}
	if cfg.Security.MaxTimeout != 42 {
		t.Errorf("MaxTimeout clobbered: %d", cfg.Security.MaxTimeout)
	}
}

func TestParseFlags_GreedyMultiValue(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"winmcpshim",
		"--allowed-roots", `D:\one`, `C:\Users\bob`,
		"--allowed-commands", `C:\Windows\System32\cmd.exe`, `C:\Program Files\Git\cmd\git.exe`,
		"--max-timeout", "120",
		"--verbose",
	}
	f := parseFlags()
	wantRoots := []string{`D:\one`, `C:\Users\bob`}
	if !f.rootsSet || !reflect.DeepEqual(f.allowedRoots, wantRoots) {
		t.Errorf("allowedRoots = %v (set=%v), want %v", f.allowedRoots, f.rootsSet, wantRoots)
	}
	wantCmds := []string{`C:\Windows\System32\cmd.exe`, `C:\Program Files\Git\cmd\git.exe`}
	if !f.commandsSet || !reflect.DeepEqual(f.allowedCommands, wantCmds) {
		t.Errorf("allowedCommands = %v (set=%v), want %v", f.allowedCommands, f.commandsSet, wantCmds)
	}
	if !f.timeoutSet || f.maxTimeout != "120" {
		t.Errorf("maxTimeout = %q (set=%v)", f.maxTimeout, f.timeoutSet)
	}
	if !f.verbose {
		t.Errorf("verbose flag not parsed after greedy multi-value flags")
	}
}

func TestParseFlags_SkipsEmptyTokens(t *testing.T) {
	// Reproduces the failure mode where Claude Desktop substitutes a user_config
	// array containing an empty entry: each becomes a token, and the empty ones
	// must be skipped before the parser hands them to applyFlagOverrides.
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"winmcpshim",
		"--allowed-roots", `D:\`, "", `C:\Users\Momo`, "  ",
		"--allowed-commands", "", `C:\Windows\System32\cmd.exe`, "\t",
	}
	f := parseFlags()
	wantRoots := []string{`D:\`, `C:\Users\Momo`}
	if !reflect.DeepEqual(f.allowedRoots, wantRoots) {
		t.Errorf("allowedRoots = %v, want %v", f.allowedRoots, wantRoots)
	}
	wantCmds := []string{`C:\Windows\System32\cmd.exe`}
	if !reflect.DeepEqual(f.allowedCommands, wantCmds) {
		t.Errorf("allowedCommands = %v, want %v", f.allowedCommands, wantCmds)
	}
}

func TestParseFlags_ZeroValues(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"winmcpshim",
		"--allowed-roots",
		"--allowed-commands",
		"--max-timeout", "60",
	}
	f := parseFlags()
	if !f.rootsSet {
		t.Errorf("rootsSet should be true even when no roots follow")
	}
	if len(f.allowedRoots) != 0 {
		t.Errorf("allowedRoots = %v, want empty", f.allowedRoots)
	}
	if !f.commandsSet {
		t.Errorf("commandsSet should be true even when no commands follow")
	}
	if len(f.allowedCommands) != 0 {
		t.Errorf("allowedCommands = %v, want empty", f.allowedCommands)
	}
	if f.maxTimeout != "60" {
		t.Errorf("maxTimeout not parsed after empty greedy --allowed-commands: %q", f.maxTimeout)
	}
}

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b , c ", []string{"a", "b", "c"}},
		{"a,,b", []string{"a", "b"}},
		{"", []string{}},
		{",", []string{}},
	}
	for _, c := range cases {
		got := splitCSV(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitCSV(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
