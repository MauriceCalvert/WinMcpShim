//go:build !windows

package shared

import (
	"os"
	"os/exec"
)

// SetupChildProcess is a no-op on non-Windows (no WER to suppress).
func SetupChildProcess(cmd *exec.Cmd) {}

// CreateJobObject is a no-op on non-Windows.
func CreateJobObject() (JobHandle, error) { return NoJobHandle, nil }

// AssignToJobObject is a no-op on non-Windows.
func AssignToJobObject(job JobHandle, process *os.Process) error { return nil }

// CloseJobObject is a no-op on non-Windows.
func CloseJobObject(job JobHandle) {}

// JobHandle is a placeholder type on non-Windows.
type JobHandle = uintptr

// NoJobHandle is the zero value indicating no job object.
const NoJobHandle JobHandle = 0
