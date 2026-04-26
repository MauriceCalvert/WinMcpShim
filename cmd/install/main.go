package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MauriceCalvert/WinMcpShim/installer"
)

func main() {
	os.Exit(run())
}

func run() int {
	fmt.Println("WinMcpShim Installer")
	fmt.Println("====================")
	fmt.Println()
	shimDir := getShimDir()
	// === Pass 1: Discover ===
	fmt.Println("[Pass 1] Discovering environment...")
	fmt.Println()
	checks := runDiscovery(shimDir)
	printChecks(checks)
	if hasFail(checks) {
		fmt.Println()
		fmt.Println("One or more prerequisites failed. Fix the issues above and re-run.")
		return 1
	}
	// Build plan from discovery results.
	plan := buildPlan(shimDir, checks)
	// === Pass 2: Interact ===
	fmt.Println()
	fmt.Println("[Pass 2] Collecting configuration...")
	fmt.Println()
	// Git not found — offer download.
	if plan.GitRoot == "" {
		gitRoot, ok := handleGitMissing()
		if !ok {
			return 1
		}
		plan.GitRoot = gitRoot
		plan.GitUsrBin = filepath.Join(gitRoot, "usr", "bin")
	}
	// Allowed roots (if shim.toml is missing or unconfigured).
	if plan.TomlState != installer.TomlConfigured {
		roots := promptAllowedRoots()
		if len(roots) == 0 {
			fmt.Println("No valid roots provided. Aborting.")
			return 2
		}
		plan.AllowedRoots = roots
	} else {
		fmt.Println("shim.toml is already configured — skipping root prompts.")
	}
	// Log directory.
	plan.LogDir = promptLogDir()
	// Claude process handling.
	claudeProcs, _ := installer.FindClaudeProcesses()
	if len(claudeProcs) > 0 {
		fmt.Printf("\nDetected %d running Claude process(es):\n", len(claudeProcs))
		for _, p := range claudeProcs {
			fmt.Printf("  PID %d  %s\n", p.PID, p.Name)
		}
		fmt.Println()
		fmt.Println("WARNING: Closing the Claude Desktop window is NOT sufficient.")
		fmt.Println("All background Claude processes must be terminated.")
		if !confirm("Kill all Claude processes?") {
			fmt.Println("Cannot proceed with Claude running. Aborting.")
			return 2
		}
		pids := extractPIDs(claudeProcs)
		if err := installer.KillProcesses(pids); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR killing processes: %v\n", err)
			return 1
		}
		fmt.Println("Waiting for processes to exit...")
		remaining, _ := installer.WaitProcessesGone(pids, 2*time.Second)
		if len(remaining) > 0 {
			fmt.Fprintf(os.Stderr, "ERROR: %d Claude process(es) still running after kill.\n", len(remaining))
			return 1
		}
		// Wait for shim to exit too (INS-07c).
		shimProcs, _ := installer.FindShimProcesses()
		if len(shimProcs) > 0 {
			shimPIDs := extractPIDs(shimProcs)
			stillAlive, _ := installer.WaitProcessesGone(shimPIDs, 5*time.Second)
			if len(stillAlive) > 0 {
				fmt.Println("WARNING: winmcpshim.exe still running. shim.toml may be briefly locked.")
			}
		}
	}
	// Display plan and confirm.
	fmt.Println()
	printPlan(plan)
	fmt.Println()
	if !confirm("Proceed with installation?") {
		fmt.Println("Aborted by user.")
		return 2
	}
	// === Pass 3: Execute ===
	fmt.Println()
	fmt.Println("[Pass 3] Installing...")
	fmt.Println()
	undo := &installer.UndoStack{}
	if err := execute(plan, undo); err != nil {
		fmt.Fprintf(os.Stderr, "\nERROR: %v\n", err)
		fmt.Println("Rolling back changes...")
		log := undo.Execute()
		for _, entry := range log {
			fmt.Println("  " + entry)
		}
		return 3
	}
	// Verification (INS-23).
	fmt.Println("Verifying installation...")
	if err := verify(plan.ShimExe); err != nil {
		fmt.Fprintf(os.Stderr, "Verification failed: %v\n", err)
		fmt.Println("Rolling back changes...")
		log := undo.Execute()
		for _, entry := range log {
			fmt.Println("  " + entry)
		}
		return 3
	}
	// Success (INS-25, INS-26).
	fmt.Println()
	fmt.Println("=== Installation Complete ===")
	fmt.Printf("  Shim:   %s\n", plan.ShimExe)
	fmt.Printf("  Config: %s\n", plan.ConfigPath)
	fmt.Printf("  Logs:   %s\n", plan.LogDir)
	if len(plan.AllowedRoots) > 0 {
		fmt.Printf("  Roots:  %d configured\n", len(plan.AllowedRoots))
	}
	fmt.Println()
	fmt.Println("Please restart Claude Desktop to activate WinMcpShim.")
	fmt.Println()
	fmt.Println("Press Enter to exit...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
	return 0
}

// getShimDir returns the directory containing install.exe.
func getShimDir() string {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: cannot determine installer location: %v\n", err)
		os.Exit(1)
	}
	return filepath.Dir(exe)
}

// runDiscovery performs all Pass 1 checks.
func runDiscovery(shimDir string) []installer.CheckResult {
	var checks []installer.CheckResult
	checks = append(checks, installer.CheckWindowsVersion(installer.GetWindowsBuild()))
	checks = append(checks, installer.CheckShimFiles(shimDir)...)
	// Git discovery — not a hard fail here, handled in Pass 2.
	gitRoot, gitErr := installer.FindGitForWindows()
	if gitErr == nil {
		checks = append(checks, installer.CheckResult{
			Req: "INS-05", Name: "Git for Windows", Status: installer.StatusOK,
			Detail: gitRoot,
		})
		present, missing := installer.CheckGitTools(gitRoot)
		if len(missing) > 0 {
			checks = append(checks, installer.CheckResult{
				Req: "INS-05b", Name: "Git tools", Status: installer.StatusFail,
				Detail: fmt.Sprintf("Missing: %s (found %d/8)", strings.Join(missing, ", "), len(present)),
			})
		} else {
			checks = append(checks, installer.CheckResult{
				Req: "INS-05b", Name: "Git tools", Status: installer.StatusOK,
				Detail: fmt.Sprintf("All %d tools found", len(present)),
			})
		}
	} else {
		checks = append(checks, installer.CheckResult{
			Req: "INS-05", Name: "Git for Windows", Status: installer.StatusWarn,
			Detail: "Not found (will prompt for installation)",
		})
	}
	appData := os.Getenv("APPDATA")
	checks = append(checks, installer.CheckClaudeDesktop(appData))
	checks = append(checks, installer.CheckTarExe())
	return checks
}

// buildPlan creates a Plan from discovery results.
func buildPlan(shimDir string, checks []installer.CheckResult) installer.Plan {
	appData := os.Getenv("APPDATA")
	claudeDir := filepath.Join(appData, "Claude")
	configPath := filepath.Join(claudeDir, "claude_desktop_config.json")
	tomlPath := filepath.Join(shimDir, "shim.toml")
	plan := installer.Plan{
		ShimDir:    shimDir,
		ShimExe:    filepath.Join(shimDir, "winmcpshim.exe"),
		ClaudeDir:  claudeDir,
		ConfigPath: configPath,
		TomlPath:   tomlPath,
		TomlState:  installer.GetTomlState(tomlPath),
		Checks:     checks,
	}
	// Git root from checks.
	for _, c := range checks {
		if c.Req == "INS-05" && c.Status == installer.StatusOK {
			plan.GitRoot = c.Detail
			plan.GitUsrBin = filepath.Join(c.Detail, "usr", "bin")
		}
		if c.Req == "INS-13" {
			plan.TarAvailable = c.Status == installer.StatusOK
		}
	}
	// Config existence.
	if _, err := os.Stat(configPath); err == nil {
		plan.ConfigExists = true
	}
	return plan
}

// execute performs all Pass 3 steps with undo tracking.
func execute(plan installer.Plan, undo *installer.UndoStack) error {
	// Step 1: Create shim.toml from example if missing.
	if plan.TomlState == installer.TomlMissing {
		fmt.Println("  Creating shim.toml from template...")
		examplePath := filepath.Join(plan.ShimDir, "shim.toml.example")
		data, err := os.ReadFile(examplePath)
		if err != nil {
			return fmt.Errorf("read %s: %w", examplePath, err)
		}
		undo.Push("remove created shim.toml", func() error {
			return os.Remove(plan.TomlPath)
		})
		if err := installer.WriteAtomic(plan.TomlPath, data); err != nil {
			return fmt.Errorf("create shim.toml: %w", err)
		}
	}
	// Step 2: Update allowed_roots if needed.
	if len(plan.AllowedRoots) > 0 {
		fmt.Println("  Updating allowed_roots in shim.toml...")
		content, err := os.ReadFile(plan.TomlPath)
		if err != nil {
			return fmt.Errorf("read shim.toml: %w", err)
		}
		bakPath, err := installer.BackupFile(plan.TomlPath)
		if err != nil {
			return fmt.Errorf("backup shim.toml: %w", err)
		}
		undo.Push("restore shim.toml from backup", func() error {
			data, err := os.ReadFile(bakPath)
			if err != nil {
				return err
			}
			return installer.WriteAtomic(plan.TomlPath, data)
		})
		modified, err := installer.SetAllowedRoots(string(content), plan.AllowedRoots)
		if err != nil {
			return fmt.Errorf("set allowed_roots: %w", err)
		}
		if err := installer.ValidateToml(modified); err != nil {
			return fmt.Errorf("toml validation after SetAllowedRoots: %w", err)
		}
		if err := installer.WriteAtomic(plan.TomlPath, []byte(modified)); err != nil {
			return fmt.Errorf("write shim.toml: %w", err)
		}
	}
	// Step 3: Update Git paths if discovered path differs from default.
	if plan.GitUsrBin != "" && plan.GitUsrBin != `C:\Program Files\Git\usr\bin` {
		fmt.Println("  Updating Git paths in shim.toml...")
		content, err := os.ReadFile(plan.TomlPath)
		if err != nil {
			return fmt.Errorf("read shim.toml: %w", err)
		}
		// Backup if step 2 didn't already do so.
		if len(plan.AllowedRoots) == 0 {
			bakPath, err := installer.BackupFile(plan.TomlPath)
			if err != nil {
				return fmt.Errorf("backup shim.toml: %w", err)
			}
			undo.Push("restore shim.toml from backup", func() error {
				data, err := os.ReadFile(bakPath)
				if err != nil {
					return err
				}
				return installer.WriteAtomic(plan.TomlPath, data)
			})
		}
		modified, err := installer.SetGitPaths(string(content), plan.GitUsrBin)
		if err != nil {
			return fmt.Errorf("set git paths: %w", err)
		}
		if err := installer.ValidateToml(modified); err != nil {
			return fmt.Errorf("toml validation after SetGitPaths: %w", err)
		}
		if err := installer.WriteAtomic(plan.TomlPath, []byte(modified)); err != nil {
			return fmt.Errorf("write shim.toml: %w", err)
		}
	}
	// Step 4: Create log directory (INS-15).
	logDirCreated := false
	if _, err := os.Stat(plan.LogDir); os.IsNotExist(err) {
		fmt.Printf("  Creating log directory: %s\n", plan.LogDir)
		if err := os.MkdirAll(plan.LogDir, 0755); err != nil {
			return fmt.Errorf("create log directory: %w", err)
		}
		logDirCreated = true
		undo.Push("remove created log directory", func() error {
			return os.Remove(plan.LogDir)
		})
	}
	_ = logDirCreated
	// Step 5: Verify log dir writable (INS-15a).
	testFile := filepath.Join(plan.LogDir, ".install-write-test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("log directory %s is not writable: %w", plan.LogDir, err)
	}
	os.Remove(testFile)
	// Step 6 & 7: Claude Desktop config.
	fmt.Println("  Configuring Claude Desktop...")
	if plan.ConfigExists {
		// Read, backup, update.
		cfg, err := installer.ReadClaudeConfig(plan.ConfigPath)
		if err != nil {
			if installer.IsNotExistError(err) {
				// Shouldn't happen since ConfigExists is true, but handle gracefully.
				return createFreshConfig(plan, undo)
			}
			return fmt.Errorf("read Claude config: %w", err)
		}
		bakPath, err := installer.BackupFile(plan.ConfigPath)
		if err != nil {
			return fmt.Errorf("backup Claude config: %w", err)
		}
		fmt.Printf("  Backed up config to: %s\n", filepath.Base(bakPath))
		undo.Push("restore Claude config from backup", func() error {
			data, err := os.ReadFile(bakPath)
			if err != nil {
				return err
			}
			return installer.WriteAtomic(plan.ConfigPath, data)
		})
		updated, action := installer.UpdateClaudeConfig(cfg, plan.ShimExe, plan.LogDir)
		switch action {
		case installer.ActionSkipped:
			fmt.Println("  WinMcpShim entry already correct — skipped.")
		case installer.ActionAdded:
			fmt.Println("  Added WinMcpShim to Claude config.")
		case installer.ActionUpdated:
			fmt.Println("  Updated WinMcpShim entry in Claude config.")
		}
		if action != installer.ActionSkipped {
			data, err := installer.MarshalConfig(updated)
			if err != nil {
				return fmt.Errorf("marshal Claude config: %w", err)
			}
			if err := installer.WriteAtomic(plan.ConfigPath, data); err != nil {
				return fmt.Errorf("write Claude config: %w", err)
			}
		}
	} else {
		// INS-22: Create fresh config.
		return createFreshConfig(plan, undo)
	}
	return nil
}

// createFreshConfig creates a new claude_desktop_config.json.
func createFreshConfig(plan installer.Plan, undo *installer.UndoStack) error {
	fmt.Println("  Creating new Claude Desktop config...")
	cfg := installer.NewClaudeConfig(plan.ShimExe, plan.LogDir)
	data, err := installer.MarshalConfig(cfg)
	if err != nil {
		return fmt.Errorf("marshal new config: %w", err)
	}
	undo.Push("remove created Claude config", func() error {
		return os.Remove(plan.ConfigPath)
	})
	if err := installer.WriteAtomic(plan.ConfigPath, data); err != nil {
		return fmt.Errorf("write Claude config: %w", err)
	}
	return nil
}

// verify runs winmcpshim.exe --scan (INS-23).
func verify(shimExe string) error {
	cmd := exec.Command(shimExe, "--scan")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("winmcpshim.exe --scan failed: %v\n%s", err, string(output))
	}
	fmt.Println("  Verification passed (--scan ok).")
	return nil
}

// handleGitMissing prompts user to install Git and re-probes.
func handleGitMissing() (string, bool) {
	fmt.Println("Git for Windows was not found.")
	fmt.Println("Download from: https://gitforwindows.org/")
	fmt.Println()
	fmt.Println("After installing Git, press Enter to re-check, or type 'q' to quit.")
	for {
		input := readLine()
		if strings.TrimSpace(strings.ToLower(input)) == "q" {
			return "", false
		}
		gitRoot, err := installer.FindGitForWindows()
		if err == nil {
			present, missing := installer.CheckGitTools(gitRoot)
			if len(missing) > 0 {
				fmt.Printf("Git found at %s but missing tools: %s (found %d/8)\n",
					gitRoot, strings.Join(missing, ", "), len(present))
				fmt.Println("Press Enter to re-check, or 'q' to quit.")
				continue
			}
			fmt.Printf("Git found at: %s\n", gitRoot)
			return gitRoot, true
		}
		fmt.Println("Git still not found. Press Enter to re-check, or 'q' to quit.")
	}
}

// promptAllowedRoots collects and validates allowed root directories.
func promptAllowedRoots() []string {
	fmt.Println("Enter directories the shim is allowed to access (allowed roots).")
	fmt.Println("Enter one path per line. Enter an empty line when done.")
	fmt.Println()
	for {
		var paths []string
		for {
			fmt.Print("  Root path (empty to finish): ")
			line := readLine()
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			paths = append(paths, line)
		}
		if len(paths) == 0 {
			fmt.Println("No paths entered. At least one root is required.")
			continue
		}
		valid, rejected := installer.ValidateRoots(paths)
		for _, r := range rejected {
			fmt.Printf("  REJECTED: %s\n", r)
		}
		if len(valid) == 0 {
			fmt.Println("No valid paths. Please try again.")
			continue
		}
		fmt.Printf("\nValid roots (%d):\n", len(valid))
		for _, v := range valid {
			fmt.Printf("  %s\n", v)
		}
		if confirm("Use these roots?") {
			return valid
		}
	}
}

// promptLogDir collects the log directory path.
func promptLogDir() string {
	defaultDir := filepath.Join(os.Getenv("USERPROFILE"), "logs", "shim")
	fmt.Printf("Log directory [%s]: ", defaultDir)
	input := strings.TrimSpace(readLine())
	if input == "" {
		return defaultDir
	}
	abs, err := filepath.Abs(input)
	if err != nil {
		fmt.Printf("  Invalid path, using default: %s\n", defaultDir)
		return defaultDir
	}
	return abs
}

// printChecks displays all check results.
func printChecks(checks []installer.CheckResult) {
	for _, c := range checks {
		var icon string
		switch c.Status {
		case installer.StatusOK:
			icon = "OK  "
		case installer.StatusWarn:
			icon = "WARN"
		case installer.StatusFail:
			icon = "FAIL"
		}
		fmt.Printf("  [%s] %s: %s — %s\n", icon, c.Req, c.Name, c.Detail)
	}
}

// printPlan displays the installation plan.
func printPlan(plan installer.Plan) {
	fmt.Println("Installation Plan:")
	fmt.Printf("  Shim directory: %s\n", plan.ShimDir)
	fmt.Printf("  Shim binary:    %s\n", plan.ShimExe)
	if plan.GitRoot != "" {
		fmt.Printf("  Git root:       %s\n", plan.GitRoot)
	}
	fmt.Printf("  Claude config:  %s\n", plan.ConfigPath)
	fmt.Printf("  Log directory:  %s\n", plan.LogDir)
	if plan.TomlState == installer.TomlMissing {
		fmt.Println("  shim.toml:      will be created from template")
	} else if plan.TomlState == installer.TomlUnconfigured {
		fmt.Println("  shim.toml:      will be updated with allowed roots")
	} else {
		fmt.Println("  shim.toml:      already configured")
	}
	if len(plan.AllowedRoots) > 0 {
		fmt.Printf("  Allowed roots:  %d path(s)\n", len(plan.AllowedRoots))
		for _, r := range plan.AllowedRoots {
			fmt.Printf("                  %s\n", r)
		}
	}
	if !plan.TarAvailable {
		fmt.Println("  Note: tar.exe not found — tar tool will be unavailable")
	}
}

// hasFail returns true if any check has StatusFail.
func hasFail(checks []installer.CheckResult) bool {
	for _, c := range checks {
		if c.Status == installer.StatusFail {
			return true
		}
	}
	return false
}

// confirm prompts for Y/N.
func confirm(prompt string) bool {
	fmt.Printf("%s [Y/n]: ", prompt)
	input := strings.TrimSpace(strings.ToLower(readLine()))
	return input == "" || input == "y" || input == "yes"
}

// readLine reads a line from stdin.
func readLine() string {
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return scanner.Text()
	}
	return ""
}

// extractPIDs extracts PIDs from ProcessInfo slices.
func extractPIDs(procs []installer.ProcessInfo) []uint32 {
	pids := make([]uint32, len(procs))
	for i, p := range procs {
		pids[i] = p.PID
	}
	return pids
}
