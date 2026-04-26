//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// doLock opens path with exclusive sharing and holds it forever (§3.10).
func doLock(path string) {
	pathp, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid path: %v\n", err)
		os.Exit(1)
	}
	h, err := syscall.CreateFile(
		pathp,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		0, // dwShareMode = 0 → exclusive
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open exclusive: %v\n", err)
		os.Exit(1)
	}
	_ = h
	for {
		time.Sleep(time.Hour)
	}
}
