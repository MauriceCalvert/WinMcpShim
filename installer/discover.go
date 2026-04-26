//go:build windows

package installer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
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

// FindGitForWindows locates the Git for Windows installation (INS-05).
// Discovery order: registry, where.exe, common paths.
func FindGitForWindows() (string, error) {
	// Step 1: Registry
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\GitForWindows`, registry.QUERY_VALUE)
	if err == nil {
		defer key.Close()
		val, _, err := key.GetStringValue("InstallPath")
		if err == nil && dirExists(filepath.Join(val, "usr", "bin")) {
			return val, nil
		}
	}
	// Step 2: where.exe grep — derive Git root as grandparent
	out, err := exec.Command("where.exe", "grep").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			lower := strings.ToLower(line)
			if strings.Contains(lower, `\usr\bin\grep.exe`) {
				gitRoot := filepath.Dir(filepath.Dir(filepath.Dir(line)))
				if dirExists(filepath.Join(gitRoot, "usr", "bin")) {
					return gitRoot, nil
				}
			}
		}
	}
	// Step 3: Common paths
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Git"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Git"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Git"),
		`C:\Git`,
	}
	for _, c := range candidates {
		if dirExists(filepath.Join(c, "usr", "bin")) {
			return c, nil
		}
	}
	return "", fmt.Errorf("Git for Windows not found. Install from https://gitforwindows.org/")
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
