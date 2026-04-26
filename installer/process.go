//go:build windows

package installer

import (
	"fmt"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// isClaudeProcess returns true for exact match on "claude.exe" (case-insensitive).
func isClaudeProcess(name string) bool {
	return strings.EqualFold(name, "claude.exe")
}

// isShimProcess returns true for exact match on "winmcpshim.exe" (case-insensitive).
func isShimProcess(name string) bool {
	return strings.EqualFold(name, "winmcpshim.exe")
}

// findProcessesByName enumerates all processes and returns those matching the predicate.
func findProcessesByName(match func(string) bool) ([]ProcessInfo, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("CreateToolhelp32Snapshot: %w", err)
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	err = windows.Process32First(snap, &entry)
	if err != nil {
		return nil, fmt.Errorf("Process32First: %w", err)
	}
	var results []ProcessInfo
	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if match(name) {
			results = append(results, ProcessInfo{PID: entry.ProcessID, Name: name})
		}
		err = windows.Process32Next(snap, &entry)
		if err != nil {
			break
		}
	}
	return results, nil
}

// FindClaudeProcesses returns all running claude.exe processes (INS-07).
func FindClaudeProcesses() ([]ProcessInfo, error) {
	return findProcessesByName(isClaudeProcess)
}

// FindShimProcesses returns all running winmcpshim.exe processes (INS-07c).
func FindShimProcesses() ([]ProcessInfo, error) {
	return findProcessesByName(isShimProcess)
}

// KillProcesses terminates the given PIDs (INS-07a).
// Only claude.exe processes should be passed; child processes exit naturally.
func KillProcesses(pids []uint32) error {
	for _, pid := range pids {
		h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
		if err != nil {
			return fmt.Errorf("OpenProcess(%d): %w", pid, err)
		}
		err = windows.TerminateProcess(h, 1)
		windows.CloseHandle(h)
		if err != nil {
			return fmt.Errorf("TerminateProcess(%d): %w", pid, err)
		}
	}
	return nil
}

// WaitProcessesGone polls until none of the given PIDs are alive or timeout expires (INS-07b, INS-07c).
// Returns any PIDs still alive after the timeout.
func WaitProcessesGone(pids []uint32, timeout time.Duration) ([]uint32, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var alive []uint32
		for _, pid := range pids {
			h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
			if err != nil {
				continue // process gone or access denied — treat as gone
			}
			var exitCode uint32
			err = windows.GetExitCodeProcess(h, &exitCode)
			windows.CloseHandle(h)
			if err != nil {
				continue
			}
			if exitCode == 259 { // STILL_ACTIVE
				alive = append(alive, pid)
			}
		}
		if len(alive) == 0 {
			return nil, nil
		}
		pids = alive
		time.Sleep(500 * time.Millisecond)
	}
	return pids, nil
}
