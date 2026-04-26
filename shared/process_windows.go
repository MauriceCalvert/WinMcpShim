//go:build windows

package shared

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// CreateDefaultErrorMode prevents WER dialogs from appearing when a child crashes (§9.8).
const CreateDefaultErrorMode = 0x04000000

// SetupChildProcess configures WER suppression on the child (§9.8).
func SetupChildProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: CreateDefaultErrorMode,
	}
}

// CreateJobObject creates a Job Object with KILL_ON_JOB_CLOSE (§9.8).
func CreateJobObject() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("CreateJobObject: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		windows.CloseHandle(job)
		return 0, fmt.Errorf("SetInformationJobObject: %w", err)
	}
	return job, nil
}

// AssignToJobObject assigns a process to the job object (§9.8).
func AssignToJobObject(job windows.Handle, process *os.Process) error {
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(process.Pid))
	if err != nil {
		return fmt.Errorf("OpenProcess for job assignment: %w", err)
	}
	defer windows.CloseHandle(ph)
	return windows.AssignProcessToJobObject(job, ph)
}

// CloseJobObject closes the job object handle, killing all processes in the job (§9.8).
func CloseJobObject(job windows.Handle) {
	if job != 0 {
		windows.CloseHandle(job)
	}
}

// JobHandle is the platform-specific type for a job object handle.
type JobHandle = windows.Handle

// NoJobHandle is the zero value indicating no job object.
const NoJobHandle JobHandle = 0
