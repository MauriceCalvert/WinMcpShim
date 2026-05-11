//go:build windows

package installer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MauriceCalvert/WinMcpShim/shared"
	"golang.org/x/sys/windows"
)

// GetWindowsBuild returns the Windows build number via RtlGetVersion.
func GetWindowsBuild() int {
	info := windows.RtlGetVersion()
	return int(info.BuildNumber)
}

// CheckWindowsVersion checks the OS build number against the minimum (INS-01).
func CheckWindowsVersion(build int) CheckResult {
	if build >= MinWindowsBuild {
		return CheckResult{
			Req:    "INS-01",
			Name:   "Windows version",
			Status: StatusOK,
			Detail: fmt.Sprintf("Build %d (>= %d)", build, MinWindowsBuild),
		}
	}
	return CheckResult{
		Req:    "INS-01",
		Name:   "Windows version",
		Status: StatusFail,
		Detail: fmt.Sprintf("Build %d is below minimum %d (Windows 10 required)", build, MinWindowsBuild),
	}
}

// CheckShimFiles checks that required files exist in the installer directory (INS-02, INS-03, INS-04).
func CheckShimFiles(dir string) []CheckResult {
	files := []struct {
		name string
		req  string
	}{
		{"winmcpshim.exe", "INS-02"},
		{"strpatch.exe", "INS-03"},
		{"shim.toml.example", "INS-04"},
	}
	var results []CheckResult
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if _, err := os.Stat(path); err != nil {
			results = append(results, CheckResult{
				Req:    f.req,
				Name:   f.name,
				Status: StatusFail,
				Detail: fmt.Sprintf("%s not found in %s", f.name, dir),
			})
		} else {
			results = append(results, CheckResult{
				Req:    f.req,
				Name:   f.name,
				Status: StatusOK,
				Detail: path,
			})
		}
	}
	return results
}

// FindGitForWindows is a thin wrapper preserving the installer-package
// API; the implementation now lives in shared so the shim can use it too.
func FindGitForWindows() (string, error) {
	return shared.FindGitForWindows()
}

// CheckGitTools checks that all 8 required executables exist in gitRoot\usr\bin (INS-05b).
// Returns (present, missing) slices of executable names.
func CheckGitTools(gitRoot string) ([]string, []string) {
	usrBin := filepath.Join(gitRoot, "usr", "bin")
	var present, missing []string
	for _, name := range RequiredGitTools {
		if _, err := os.Stat(filepath.Join(usrBin, name)); err != nil {
			missing = append(missing, name)
		} else {
			present = append(present, name)
		}
	}
	return present, missing
}

// CheckClaudeDesktop checks that %APPDATA%\Claude exists (INS-06).
func CheckClaudeDesktop(appData string) CheckResult {
	claudeDir := filepath.Join(appData, "Claude")
	if dirExists(claudeDir) {
		return CheckResult{
			Req:    "INS-06",
			Name:   "Claude Desktop",
			Status: StatusOK,
			Detail: claudeDir,
		}
	}
	return CheckResult{
		Req:    "INS-06",
		Name:   "Claude Desktop",
		Status: StatusFail,
		Detail: fmt.Sprintf("Directory %s not found. Is Claude Desktop installed?", claudeDir),
	}
}

// CheckTarExe checks that C:\Windows\System32\tar.exe exists (INS-13).
func CheckTarExe() CheckResult {
	tarPath := `C:\Windows\System32\tar.exe`
	if _, err := os.Stat(tarPath); err != nil {
		return CheckResult{
			Req:    "INS-13",
			Name:   "tar.exe",
			Status: StatusWarn,
			Detail: "tar.exe not found in System32 (tar tool will be unavailable)",
		}
	}
	return CheckResult{
		Req:    "INS-13",
		Name:   "tar.exe",
		Status: StatusOK,
		Detail: tarPath,
	}
}

// dirExists returns true if the path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
