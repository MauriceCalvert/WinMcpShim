package main

import (
	"os"
	"syscall"
	"testing"
	"unsafe"
)

var (
	modkernel32     = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx  = modkernel32.NewProc("LockFileEx")
)

const (
	lockfileExclusiveLock = 0x00000002
	lockfileFailImmediately = 0x00000001
)

// lockFileWindows applies an exclusive lock on a file handle.
func lockFileWindows(t *testing.T, f *os.File) {
	t.Helper()
	h := syscall.Handle(f.Fd())
	ol := new(syscall.Overlapped)
	r1, _, err := procLockFileEx.Call(
		uintptr(h),
		uintptr(lockfileExclusiveLock|lockfileFailImmediately),
		0,
		1, 0,
		uintptr(unsafe.Pointer(ol)),
	)
	if r1 == 0 {
		t.Fatalf("LockFileEx failed: %v", err)
	}
}
