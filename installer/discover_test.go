//go:build windows

package installer

import (
	"os"
	"path/filepath"
	"testing"
)

// T-01: CheckWindowsVersion returns FAIL for build 9600 (Windows 8.1) (INS-01).
func TestCheckWindowsVersion_Win81(t *testing.T) {
	r := CheckWindowsVersion(9600)
	if r.Status != StatusFail {
		t.Errorf("status = %d, want StatusFail for build 9600", r.Status)
	}
}

// T-02: CheckWindowsVersion returns OK for build 10240 (Windows 10 RTM) (INS-01).
func TestCheckWindowsVersion_Win10RTM(t *testing.T) {
	r := CheckWindowsVersion(10240)
	if r.Status != StatusOK {
		t.Errorf("status = %d, want StatusOK for build 10240", r.Status)
	}
}

// T-03: CheckWindowsVersion returns OK for build 19045 (Windows 10 22H2) (INS-01).
func TestCheckWindowsVersion_Win10_22H2(t *testing.T) {
	r := CheckWindowsVersion(19045)
	if r.Status != StatusOK {
		t.Errorf("status = %d, want StatusOK for build 19045", r.Status)
	}
}

// T-04: CheckShimFiles returns FAIL when winmcpshim.exe absent (INS-02).
func TestCheckShimFiles_MissingShim(t *testing.T) {
	dir := t.TempDir()
	createDummy(t, dir, "strpatch.exe")
	createDummy(t, dir, "shim.toml.example")
	results := CheckShimFiles(dir)
	found := findByReq(results, "INS-02")
	if found.Status != StatusFail {
		t.Errorf("INS-02 status = %d, want StatusFail", found.Status)
	}
}

// T-05: CheckShimFiles returns FAIL when strpatch.exe absent (INS-03).
func TestCheckShimFiles_MissingStrpatch(t *testing.T) {
	dir := t.TempDir()
	createDummy(t, dir, "winmcpshim.exe")
	createDummy(t, dir, "shim.toml.example")
	results := CheckShimFiles(dir)
	found := findByReq(results, "INS-03")
	if found.Status != StatusFail {
		t.Errorf("INS-03 status = %d, want StatusFail", found.Status)
	}
}

// T-06: CheckShimFiles returns FAIL when shim.toml.example absent (INS-04).
func TestCheckShimFiles_MissingExample(t *testing.T) {
	dir := t.TempDir()
	createDummy(t, dir, "winmcpshim.exe")
	createDummy(t, dir, "strpatch.exe")
	results := CheckShimFiles(dir)
	found := findByReq(results, "INS-04")
	if found.Status != StatusFail {
		t.Errorf("INS-04 status = %d, want StatusFail", found.Status)
	}
}

// T-07: CheckShimFiles returns three OKs when all present (INS-02, INS-03, INS-04).
func TestCheckShimFiles_AllPresent(t *testing.T) {
	dir := t.TempDir()
	createDummy(t, dir, "winmcpshim.exe")
	createDummy(t, dir, "strpatch.exe")
	createDummy(t, dir, "shim.toml.example")
	results := CheckShimFiles(dir)
	for _, r := range results {
		if r.Status != StatusOK {
			t.Errorf("%s status = %d, want StatusOK", r.Req, r.Status)
		}
	}
}

// T-08: CheckGitTools with all 8 exes present returns 8 present, 0 missing (INS-05b).
func TestCheckGitTools_AllPresent(t *testing.T) {
	gitRoot := t.TempDir()
	usrBin := filepath.Join(gitRoot, "usr", "bin")
	os.MkdirAll(usrBin, 0755)
	for _, name := range RequiredGitTools {
		createDummy(t, usrBin, name)
	}
	present, missing := CheckGitTools(gitRoot)
	if len(present) != 8 {
		t.Errorf("present = %d, want 8", len(present))
	}
	if len(missing) != 0 {
		t.Errorf("missing = %v, want empty", missing)
	}
}

// T-09: CheckGitTools with gawk.exe removed returns 7 present, 1 missing (INS-05b).
func TestCheckGitTools_MissingGawk(t *testing.T) {
	gitRoot := t.TempDir()
	usrBin := filepath.Join(gitRoot, "usr", "bin")
	os.MkdirAll(usrBin, 0755)
	for _, name := range RequiredGitTools {
		if name != "gawk.exe" {
			createDummy(t, usrBin, name)
		}
	}
	present, missing := CheckGitTools(gitRoot)
	if len(present) != 7 {
		t.Errorf("present = %d, want 7", len(present))
	}
	if len(missing) != 1 || missing[0] != "gawk.exe" {
		t.Errorf("missing = %v, want [gawk.exe]", missing)
	}
}

// T-10: CheckGitTools with empty usr\bin returns 0 present, 8 missing (INS-05b).
func TestCheckGitTools_EmptyUsrBin(t *testing.T) {
	gitRoot := t.TempDir()
	usrBin := filepath.Join(gitRoot, "usr", "bin")
	os.MkdirAll(usrBin, 0755)
	present, missing := CheckGitTools(gitRoot)
	if len(present) != 0 {
		t.Errorf("present = %d, want 0", len(present))
	}
	if len(missing) != 8 {
		t.Errorf("missing = %d, want 8", len(missing))
	}
}

// T-11: CheckClaudeDesktop returns FAIL when dir does not exist (INS-06).
func TestCheckClaudeDesktop_Missing(t *testing.T) {
	r := CheckClaudeDesktop(filepath.Join(t.TempDir(), "nonexistent"))
	if r.Status != StatusFail {
		t.Errorf("status = %d, want StatusFail", r.Status)
	}
}

// T-12: CheckClaudeDesktop returns OK when dir exists (INS-06).
func TestCheckClaudeDesktop_Exists(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "Claude"), 0755)
	r := CheckClaudeDesktop(dir)
	if r.Status != StatusOK {
		t.Errorf("status = %d, want StatusOK", r.Status)
	}
}

// T-13: CheckTarExe on a real Windows 10 machine returns OK (INS-13).
func TestCheckTarExe_RealSystem(t *testing.T) {
	r := CheckTarExe()
	// On Windows 10+, tar.exe should exist. If it doesn't, this is just a WARN.
	if r.Status == StatusFail {
		t.Errorf("CheckTarExe returned FAIL, expected OK or WARN")
	}
}

// createDummy creates an empty file in dir.
func createDummy(t *testing.T, dir string, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0644); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
}

// findByReq finds a CheckResult by requirement ID.
func findByReq(results []CheckResult, req string) CheckResult {
	for _, r := range results {
		if r.Req == req {
			return r
		}
	}
	return CheckResult{Req: req, Status: StatusFail, Detail: "not found in results"}
}
