package shared

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// ===== CheckCommandConfinement =====

func TestCheckCommandConfinement_AbsoluteInside(t *testing.T) {
	dir := t.TempDir()
	// Any absolute path inside the root should be allowed (file need not exist for pre-check)
	err := CheckCommandConfinement(dir+"\\allowed.exe", []string{dir})
	if err != nil {
		t.Errorf("expected no error for absolute path inside root, got %v", err)
	}
}

func TestCheckCommandConfinement_AbsoluteOutside(t *testing.T) {
	dir := t.TempDir()
	err := CheckCommandConfinement("C:\\Windows\\System32\\notepad.exe", []string{dir})
	if err == nil {
		t.Fatal("expected error for absolute path outside root")
	}
}

func TestCheckCommandConfinement_Unqualified(t *testing.T) {
	// "cmd.exe" is on PATH, should be allowed
	err := CheckCommandConfinement("cmd.exe", []string{"C:\\SomeRoot"})
	if err != nil {
		t.Errorf("expected no error for unqualified command on PATH, got %v", err)
	}
}

// ===== CheckPathConfinement =====

func TestCheckPathConfinement_EmptyPath(t *testing.T) {
	// Empty string resolves to cwd, which may or may not be inside roots
	err := CheckPathConfinement("", []string{"C:\\NonexistentRoot"})
	if err == nil {
		t.Log("empty path resolved inside root (cwd happens to match)")
	}
}

func TestCheckPathConfinement_InsideRoot(t *testing.T) {
	dir := t.TempDir()
	err := CheckPathConfinement(dir+"\\subdir\\file.txt", []string{dir})
	if err != nil {
		t.Errorf("expected no error for path inside root, got %v", err)
	}
}

func TestCheckPathConfinement_OutsideRoot(t *testing.T) {
	dir := t.TempDir()
	err := CheckPathConfinement("C:\\Windows\\System32\\config", []string{dir})
	if err == nil {
		t.Fatal("expected error for path outside root")
	}
	if !strings.Contains(err.Error(), "not within") {
		t.Errorf("expected 'not within' in error, got %q", err.Error())
	}
}

func TestCheckPathConfinement_RelativePath(t *testing.T) {
	dir := t.TempDir()
	// Relative path gets resolved to abs by filepath.Abs
	err := CheckPathConfinement("relative/path.txt", []string{dir})
	// This may or may not error depending on whether cwd is inside dir
	// Just ensure no panic
	_ = err
}

// ===== CheckResolvedPathConfinement =====

func TestCheckResolvedPath_Inside(t *testing.T) {
	dir := t.TempDir()
	err := CheckResolvedPathConfinement(dir+"\\inner\\file.txt", []string{dir})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCheckResolvedPath_Outside(t *testing.T) {
	dir := t.TempDir()
	err := CheckResolvedPathConfinement("C:\\Windows\\System32", []string{dir})
	if err == nil {
		t.Fatal("expected error for path outside root")
	}
}

// ===== RawJSON =====

func TestRawJSON_ValidJSON(t *testing.T) {
	got := RawJSON(`{"key": "value"}`)
	var parsed map[string]string
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("failed to parse RawJSON output: %v", err)
	}
	if parsed["key"] != "value" {
		t.Errorf("expected 'value', got %q", parsed["key"])
	}
}

func TestRawJSON_Nil(t *testing.T) {
	// RawJSON with valid but minimal JSON
	got := RawJSON(`null`)
	if string(got) != "null" {
		t.Errorf("expected 'null', got %q", string(got))
	}
}

// ===== CheckPathConfinement empty roots =====

func TestCheckPathConfinement_EmptyRoots(t *testing.T) {
	// With no roots, everything is allowed
	err := CheckPathConfinement("C:\\anything\\path", []string{})
	if err != nil {
		t.Errorf("expected no error with empty roots, got %v", err)
	}
}

// ===== CheckCommandConfinement empty roots =====

func TestCheckCommandConfinement_EmptyRoots(t *testing.T) {
	err := CheckCommandConfinement("C:\\anything\\cmd.exe", []string{})
	if err != nil {
		t.Errorf("expected no error with empty roots, got %v", err)
	}
}

// ===== CheckCommandConfinement unqualified not on path =====

func TestCheckCommandConfinement_NotOnPath(t *testing.T) {
	err := CheckCommandConfinement("nonexistent_binary_xyz_123", []string{"C:\\SomeRoot"})
	if err == nil {
		t.Fatal("expected error for command not on PATH and not in roots")
	}
}

// ===== CheckResolvedPathConfinement exact root =====

func TestCheckResolvedPath_ExactRoot(t *testing.T) {
	dir := t.TempDir()
	err := CheckResolvedPathConfinement(dir, []string{dir})
	if err != nil {
		t.Errorf("expected no error for exact root match, got %v", err)
	}
}

// ===== CheckPathConfinementFull =====

func TestCheckPathConfinementFull_InsideRoot(t *testing.T) {
	dir := t.TempDir()
	path := dir + "\\subdir\\file.txt"
	err := CheckPathConfinementFull(path, []string{dir})
	// May error because file doesn't exist (VerifyPathByHandle), but should not panic
	_ = err
}

// ===== ConfinementError =====

func TestConfinementError_Message(t *testing.T) {
	e := &ConfinementError{Message: "test breach", IsCritical: true}
	if e.Error() != "test breach" {
		t.Errorf("expected 'test breach', got %q", e.Error())
	}
	if !e.IsCritical {
		t.Error("expected IsCritical=true")
	}
}

// ===== MakeToolResult =====

func TestMakeToolResult_Success(t *testing.T) {
	got := MakeToolResult("hello", false)
	var tr ToolResult
	json.Unmarshal(got, &tr)
	if tr.IsError {
		t.Error("expected IsError=false")
	}
	if len(tr.Content) != 1 || tr.Content[0].Text != "hello" {
		t.Errorf("expected content 'hello', got %v", tr.Content)
	}
}

func TestMakeToolResult_Error(t *testing.T) {
	got := MakeToolResult("fail", true)
	var tr ToolResult
	json.Unmarshal(got, &tr)
	if !tr.IsError {
		t.Error("expected IsError=true")
	}
}

// ===== MakeErrorResponse =====

func TestMakeErrorResponse(t *testing.T) {
	id := json.RawMessage(`1`)
	resp := MakeErrorResponse(id, -32600, "Invalid Request")
	if resp.Error.Code != -32600 {
		t.Errorf("expected code -32600, got %d", resp.Error.Code)
	}
	if resp.Error.Message != "Invalid Request" {
		t.Errorf("expected 'Invalid Request', got %q", resp.Error.Message)
	}
}

// ===== MakeSuccessResponse =====

func TestMakeSuccessResponse(t *testing.T) {
	id := json.RawMessage(`1`)
	result := json.RawMessage(`{"ok":true}`)
	resp := MakeSuccessResponse(id, result)
	if resp.JSONRPC != "2.0" {
		t.Errorf("expected '2.0', got %q", resp.JSONRPC)
	}
	if string(resp.Result) != `{"ok":true}` {
		t.Errorf("expected result, got %q", string(resp.Result))
	}
}

// ===== RawJSON panic on invalid =====

func TestRawJSON_PanicsOnInvalid(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic for invalid JSON")
		}
	}()
	RawJSON(`{invalid json}`)
}

// ===== SanitizeAllowedRoots =====

func TestSanitizeAllowedRoots_DropsEmptyAndWhitespace(t *testing.T) {
	dir := t.TempDir()
	got, warns := SanitizeAllowedRoots([]string{dir, "", "   ", "\t"})
	if len(got) != 1 {
		t.Fatalf("expected 1 entry kept, got %v (warns=%v)", got, warns)
	}
	if len(warns) != 3 {
		t.Errorf("expected 3 warnings (one per dropped empty/whitespace), got %v", warns)
	}
	for _, w := range warns {
		if !strings.Contains(w, "empty/whitespace") {
			t.Errorf("warning should mention empty/whitespace, got %q", w)
		}
	}
}

func TestSanitizeAllowedRoots_DedupCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	got, _ := SanitizeAllowedRoots([]string{dir, strings.ToUpper(dir), dir})
	if len(got) != 1 {
		t.Errorf("expected dedup to 1 entry, got %v", got)
	}
}

func TestSanitizeAllowedRoots_NormalisesToAbsolute(t *testing.T) {
	dir := ToLongPath(t.TempDir())
	got, _ := SanitizeAllowedRoots([]string{"  " + dir + "  "})
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %v", got)
	}
	if !strings.EqualFold(got[0], dir) {
		t.Errorf("expected normalised %q, got %q", dir, got[0])
	}
}

func TestSanitizeAllowedRoots_PreservesOrder(t *testing.T) {
	a := ToLongPath(t.TempDir())
	b := ToLongPath(t.TempDir())
	got, _ := SanitizeAllowedRoots([]string{b, "", a})
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %v", got)
	}
	if !strings.EqualFold(got[0], b) || !strings.EqualFold(got[1], a) {
		t.Errorf("order not preserved: got %v, expected [%s %s]", got, b, a)
	}
}

func TestSanitizeAllowedRoots_RejectsUnsubstitutedPlaceholder(t *testing.T) {
	got, warns := SanitizeAllowedRoots([]string{"${user_config.allowed_roots}"})
	if len(got) != 0 {
		t.Errorf("expected 0 entries, got %v", got)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "placeholder") {
		t.Errorf("expected placeholder warning, got %v", warns)
	}
}

func TestSanitizeAllowedRoots_RejectsControlChars(t *testing.T) {
	// Trailing whitespace control chars are legitimately stripped by TrimSpace
	// before the control-char check runs; the dangerous case is a control char
	// EMBEDDED inside the path (NUL truncation, mid-path newline injection).
	dir := t.TempDir()
	cases := []string{
		dir + "\x00\\System32",         // NUL truncation: path appears to be dir but Windows API stops at NUL
		dir + "\\foo\nbar",             // embedded newline
		dir + "\\foo\rbar",             // embedded CR
		"\x00" + dir,                   // leading NUL
		dir[:3] + "\x01" + dir[3:],     // arbitrary C0 byte mid-path
	}
	for _, bad := range cases {
		got, warns := SanitizeAllowedRoots([]string{bad})
		if len(got) != 0 {
			t.Errorf("expected entry %q to be dropped, got %v", bad, got)
		}
		if len(warns) != 1 || !strings.Contains(warns[0], "control characters") {
			t.Errorf("expected control-char warning for %q, got %v", bad, warns)
		}
	}
}

func TestSanitizeAllowedRoots_RejectsRelativePaths(t *testing.T) {
	cases := []string{`..\Documents`, `subdir`, `~/Documents`, `C:foo`, `\foo`, `/foo`}
	for _, rel := range cases {
		got, warns := SanitizeAllowedRoots([]string{rel})
		if len(got) != 0 {
			t.Errorf("expected relative path %q to be dropped, got %v", rel, got)
		}
		if len(warns) != 1 || !strings.Contains(warns[0], "absolute") {
			t.Errorf("expected 'absolute' warning for %q, got %v", rel, warns)
		}
	}
}

func TestResolveCommandPath_RejectsPlaceholder(t *testing.T) {
	if _, err := ResolveCommandPath("${user_config.allowed_commands}"); err == nil {
		t.Error("expected rejection for unsubstituted placeholder")
	}
}

func TestResolveCommandPath_RejectsControlChars(t *testing.T) {
	if _, err := ResolveCommandPath("cmd\x00.exe"); err == nil {
		t.Error("expected rejection for NUL in command")
	}
	if _, err := ResolveCommandPath("cmd\n"); err == nil {
		t.Error("expected rejection for newline in command")
	}
}

func TestResolveCommandPath_RejectsRelativeWithSeparator(t *testing.T) {
	if _, err := ResolveCommandPath(`bin\foo.exe`); err == nil {
		t.Error(`expected rejection for relative path with separator`)
	}
	if _, err := ResolveCommandPath(`bin/foo.exe`); err == nil {
		t.Error("expected rejection for relative path with forward slash")
	}
}

func TestResolveCommandPath_RejectsNonExistent(t *testing.T) {
	dir := t.TempDir()
	if _, err := ResolveCommandPath(dir + `\does_not_exist.exe`); err == nil {
		t.Error("expected rejection for non-existent absolute path")
	}
}

func TestResolveCommandPath_RejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := ResolveCommandPath(dir); err == nil {
		t.Error("expected rejection for directory passed as command")
	}
}

// ===== ResolveCommandPath =====

func TestResolveCommandPath_Absolute(t *testing.T) {
	dir := ToLongPath(t.TempDir())
	exe := dir + `\foo.exe`
	if err := os.WriteFile(exe, []byte("MZ"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	p, err := ResolveCommandPath(exe)
	if err != nil {
		t.Fatalf("absolute path should resolve, got %v", err)
	}
	if !strings.EqualFold(p, exe) {
		t.Errorf("resolved = %q, want %q", p, exe)
	}
}

func TestResolveCommandPath_Bare_OnPath(t *testing.T) {
	// cmd.exe is always on PATH on Windows test runners.
	p, err := ResolveCommandPath("cmd")
	if err != nil {
		t.Fatalf("expected cmd to resolve via LookPath, got %v", err)
	}
	if !strings.HasSuffix(strings.ToLower(p), `\cmd.exe`) {
		t.Errorf("resolved = %q, expected to end with \\cmd.exe", p)
	}
}

func TestResolveCommandPath_Bare_NotOnPath(t *testing.T) {
	if _, err := ResolveCommandPath("nonexistent_binary_xyz_123"); err == nil {
		t.Error("expected error for binary not on PATH")
	}
}

// ===== ResolveAllowedCommands =====

func TestResolveAllowedCommands_BareNameResolved(t *testing.T) {
	got, warns := ResolveAllowedCommands([]string{"cmd"})
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %v (warns=%v)", got, warns)
	}
	if !strings.HasSuffix(strings.ToLower(got[0]), `\cmd.exe`) {
		t.Errorf("entry = %q, expected to end with \\cmd.exe", got[0])
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
}

func TestResolveAllowedCommands_DropsUnresolvable(t *testing.T) {
	got, warns := ResolveAllowedCommands([]string{"cmd", "nonexistent_binary_xyz_123"})
	if len(got) != 1 {
		t.Errorf("expected 1 entry kept, got %v", got)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "nonexistent_binary_xyz_123") {
		t.Errorf("expected warning about dropped entry, got %v", warns)
	}
}

func TestResolveAllowedCommands_Dedup(t *testing.T) {
	got, _ := ResolveAllowedCommands([]string{"cmd", "CMD", "cmd"})
	if len(got) != 1 {
		t.Errorf("expected dedup to 1 entry, got %v", got)
	}
}

func TestResolveAllowedCommands_TrimsAndSkipsEmpty(t *testing.T) {
	got, _ := ResolveAllowedCommands([]string{"  ", "", "  cmd  "})
	if len(got) != 1 {
		t.Errorf("expected 1 entry after trim/skip, got %v", got)
	}
}

// ===== CheckRunPermission =====

func TestCheckRunPermission_InsideAllowedRoots(t *testing.T) {
	dir := t.TempDir()
	exe := dir + `\my-tool.exe`
	if err := os.WriteFile(exe, []byte("MZ"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// Allowlist is non-empty but does not include the command. Roots should grant.
	cmdPath, _ := ResolveCommandPath(`C:\Windows\System32\cmd.exe`)
	if err := CheckRunPermission(exe, []string{dir}, []string{cmdPath}); err != nil {
		t.Errorf("expected exe inside roots to be allowed, got %v", err)
	}
}

func TestCheckRunPermission_AllowlistExactMatch(t *testing.T) {
	dir := t.TempDir()
	cmdPath, err := ResolveCommandPath("cmd")
	if err != nil {
		t.Skipf("cmd not on PATH: %v", err)
	}
	// Caller invokes the bare name; resolved path matches the allowlist entry.
	if err := CheckRunPermission("cmd", []string{dir}, []string{cmdPath}); err != nil {
		t.Errorf("expected bare 'cmd' to match resolved allowlist entry, got %v", err)
	}
}

func TestCheckRunPermission_AbsolutePathMatchesAllowlist(t *testing.T) {
	dir := t.TempDir()
	cmdPath, err := ResolveCommandPath("cmd")
	if err != nil {
		t.Skipf("cmd not on PATH: %v", err)
	}
	if err := CheckRunPermission(cmdPath, []string{dir}, []string{cmdPath}); err != nil {
		t.Errorf("expected absolute cmd.exe to match allowlist entry, got %v", err)
	}
}

func TestCheckRunPermission_NotListedAndOutsideRoots(t *testing.T) {
	dir := t.TempDir()
	// notepad.exe lives in System32, not inside dir, and is not on the allowlist.
	allowlist, _ := ResolveAllowedCommands([]string{"cmd"})
	err := CheckRunPermission("notepad.exe", []string{dir}, allowlist)
	if err == nil {
		t.Fatal("expected rejection for command outside roots and not on allowlist")
	}
	if !strings.Contains(err.Error(), "not permitted") {
		t.Errorf("error should mention 'not permitted', got %v", err)
	}
}

func TestCheckRunPermission_UnresolvableCommand(t *testing.T) {
	allowlist, _ := ResolveAllowedCommands([]string{"cmd"})
	err := CheckRunPermission("nonexistent_binary_xyz_123", nil, allowlist)
	if err == nil {
		t.Fatal("expected error for unresolvable command")
	}
}
