package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MauriceCalvert/WinMcpShim/installer"
)

func main() {
	os.Exit(run())
}

func run() int {
	fmt.Println("WinMcpShim Uninstaller")
	fmt.Println("======================")
	fmt.Println()
	appData := os.Getenv("APPDATA")
	configPath := filepath.Join(appData, "Claude", "claude_desktop_config.json")
	// === Discover ===
	cfg, err := installer.ReadClaudeConfig(configPath)
	if err != nil {
		if installer.IsNotExistError(err) {
			fmt.Println("Claude Desktop config not found. Nothing to uninstall.")
			return 0
		}
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return 1
	}
	// Check if WinMcpShim entry exists (dry run).
	_, found := installer.RemoveShimEntry(deepCopy(cfg))
	if !found {
		fmt.Println("WinMcpShim is not configured in Claude Desktop. Nothing to remove.")
		return 0
	}
	// Detect Claude processes (UNS-04).
	claudeProcs, _ := installer.FindClaudeProcesses()
	if len(claudeProcs) > 0 {
		fmt.Printf("Detected %d running Claude process(es):\n", len(claudeProcs))
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
			fmt.Fprintf(os.Stderr, "ERROR: %d Claude process(es) still running.\n", len(remaining))
			return 1
		}
		// Wait for shim to exit (UNS-04b).
		shimProcs, _ := installer.FindShimProcesses()
		if len(shimProcs) > 0 {
			shimPIDs := extractPIDs(shimProcs)
			stillAlive, _ := installer.WaitProcessesGone(shimPIDs, 5*time.Second)
			if len(stillAlive) > 0 {
				fmt.Println("WARNING: winmcpshim.exe still running.")
			}
		}
	}
	// === Confirm ===
	fmt.Println()
	fmt.Println("This will remove the WinMcpShim entry from Claude Desktop config.")
	fmt.Println()
	fmt.Println("The following will NOT be deleted:")
	fmt.Println("  - winmcpshim.exe and strpatch.exe")
	fmt.Println("  - shim.toml configuration")
	fmt.Println("  - Log files")
	fmt.Println()
	if !confirm("Proceed with uninstall?") {
		fmt.Println("Aborted by user.")
		return 2
	}
	// === Execute ===
	fmt.Println()
	// Backup (UNS-05).
	bakPath, err := installer.BackupFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR creating backup: %v\n", err)
		return 1
	}
	fmt.Printf("Backed up config to: %s\n", filepath.Base(bakPath))
	// Remove entry (UNS-06).
	modified, _ := installer.RemoveShimEntry(cfg)
	// Marshal and write (UNS-07).
	data, err := installer.MarshalConfig(modified)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR marshalling config: %v\n", err)
		restoreFromBackup(bakPath, configPath)
		return 1
	}
	if err := installer.WriteAtomic(configPath, data); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR writing config: %v\n", err)
		restoreFromBackup(bakPath, configPath)
		return 1
	}
	// Summary (UNS-08, UNS-09).
	fmt.Println()
	fmt.Println("=== Uninstall Complete ===")
	fmt.Println("  Removed WinMcpShim from Claude Desktop config.")
	fmt.Println()
	fmt.Println("  NOT removed (manual cleanup if desired):")
	fmt.Println("    - winmcpshim.exe, strpatch.exe, shim.toml")
	fmt.Println("    - Log files")
	fmt.Println()
	fmt.Println("Please restart Claude Desktop for changes to take effect.")
	fmt.Println()
	fmt.Println("Press Enter to exit...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
	return 0
}

// restoreFromBackup copies the backup back to the original path.
func restoreFromBackup(bakPath string, configPath string) {
	data, err := os.ReadFile(bakPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "CRITICAL: cannot read backup %s: %v\n", bakPath, err)
		return
	}
	if err := installer.WriteAtomic(configPath, data); err != nil {
		fmt.Fprintf(os.Stderr, "CRITICAL: cannot restore from backup: %v\n", err)
		fmt.Fprintf(os.Stderr, "Your backup is at: %s\n", bakPath)
	} else {
		fmt.Println("Restored config from backup.")
	}
}

// deepCopy creates a deep copy of a config map for dry-run checks.
func deepCopy(cfg map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range cfg {
		switch val := v.(type) {
		case map[string]interface{}:
			result[k] = deepCopy(val)
		case []interface{}:
			cp := make([]interface{}, len(val))
			copy(cp, val)
			result[k] = cp
		default:
			result[k] = v
		}
	}
	return result
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
