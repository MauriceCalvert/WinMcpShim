//go:build windows

package shared

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAugmentAllowedCommandsWithGit_NotInstalled verifies that when Git for
// Windows cannot be discovered, the input list is returned unchanged. This
// runs the real discovery, so we cannot guarantee Git is absent on every
// build host; the test is therefore weak — it only asserts no panic and
// that the returned slice contains everything the input did. The
// idempotency case below is the strict assertion.
func TestAugmentAllowedCommandsWithGit_PreservesInput(t *testing.T) {
	in := []string{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`}
	out := AugmentAllowedCommandsWithGit(in)
	if len(out) < len(in) {
		t.Fatalf("augment shrank list: in=%v out=%v", in, out)
	}
	for _, want := range in {
		if !contains(out, want) {
			t.Errorf("input entry %q lost from output %v", want, out)
		}
	}
}

// TestAugmentAllowedCommandsWithGit_Idempotent verifies that running the
// augmentation twice produces the same list — the second call must not
// duplicate git.exe even though it is now present from the first.
func TestAugmentAllowedCommandsWithGit_Idempotent(t *testing.T) {
	in := []string{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`}
	once := AugmentAllowedCommandsWithGit(in)
	twice := AugmentAllowedCommandsWithGit(once)
	if len(twice) != len(once) {
		t.Fatalf("idempotency violated: once=%v twice=%v", once, twice)
	}
	for i := range once {
		if !strings.EqualFold(once[i], twice[i]) {
			t.Errorf("entry %d differs: once=%q twice=%q", i, once[i], twice[i])
		}
	}
}

// TestAugmentAllowedCommandsWithGit_AddsGitExe verifies that when Git is
// present on the system, the function adds the absolute git.exe path to
// the allowlist. Skipped if Git is not installed.
func TestAugmentAllowedCommandsWithGit_AddsGitExe(t *testing.T) {
	gitRoot, err := FindGitForWindows()
	if err != nil {
		t.Skip("Git for Windows not installed on this host")
	}
	expected := filepath.Join(gitRoot, "cmd", "git.exe")
	if _, err := os.Stat(expected); err != nil {
		t.Skipf("git.exe not present at %s: %v", expected, err)
	}
	out := AugmentAllowedCommandsWithGit(nil)
	if !contains(out, expected) {
		t.Errorf("expected %q in output, got %v", expected, out)
	}
}

// TestAugmentAllowedCommandsWithGit_CaseInsensitiveSkip verifies that an
// entry differing only in case from the discovered git.exe path does not
// cause a duplicate to be appended. Skipped if Git is not installed.
func TestAugmentAllowedCommandsWithGit_CaseInsensitiveSkip(t *testing.T) {
	gitRoot, err := FindGitForWindows()
	if err != nil {
		t.Skip("Git for Windows not installed on this host")
	}
	expected := filepath.Join(gitRoot, "cmd", "git.exe")
	if _, err := os.Stat(expected); err != nil {
		t.Skipf("git.exe not present at %s: %v", expected, err)
	}
	mixed := strings.ToUpper(expected[:3]) + strings.ToLower(expected[3:])
	in := []string{mixed}
	out := AugmentAllowedCommandsWithGit(in)
	if len(out) != 1 {
		t.Errorf("expected 1 entry (case-insensitive dedupe), got %d: %v", len(out), out)
	}
}

func contains(items []string, want string) bool {
	for _, it := range items {
		if strings.EqualFold(it, want) {
			return true
		}
	}
	return false
}
