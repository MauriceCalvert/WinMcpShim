// rogue.exe — adversarial test helper for WinMcpShim.
// Deliberately misbehaves on demand so shim tests can verify
// every defensive mechanism without external dependencies.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: rogue.exe --<mode> [args]")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "--crash":
		doCrash()
	case "--grandchild":
		doGrandchild()
	case "--flood-stdout":
		doFloodStdout()
	case "--flood-both":
		doFloodBoth()
	case "--hang":
		doHang()
	case "--trickle":
		doTrickle()
	case "--read-stdin":
		doReadStdin()
	case "--echo":
		doEcho()
	case "--print-args":
		doPrintArgs()
	case "--lock":
		doLock(os.Args[2])
	case "--combo":
		doCombo()
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", os.Args[1])
		os.Exit(1)
	}
}

// doCrash triggers a null pointer dereference (§3.1).
func doCrash() {
	var p *int
	println(*p) // deliberate nil deref — triggers access violation for WER suppression tests
}

// doGrandchild spawns a hanging child and writes its PID to file (§3.2).
func doGrandchild() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "--grandchild requires <pidfile>")
		os.Exit(1)
	}
	pidFile := os.Args[2]
	cmd := exec.Command(os.Args[0], "--hang")
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "spawn grandchild: %v\n", err)
		os.Exit(1)
	}
	os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0644)
	for {
		fmt.Print(".")
		os.Stdout.Sync()
		time.Sleep(time.Second)
	}
}

// doFloodStdout writes N bytes to stdout (§3.3).
func doFloodStdout() {
	n := requireByteCount()
	os.Stdout.Write(bytes.Repeat([]byte("O"), n))
}

// doFloodBoth writes N bytes to stdout and N bytes to stderr (§3.4).
func doFloodBoth() {
	n := requireByteCount()
	os.Stdout.Write(bytes.Repeat([]byte("O"), n))
	os.Stderr.Write(bytes.Repeat([]byte("E"), n))
}

// doHang blocks forever with no output (§3.5).
func doHang() {
	// time.Sleep avoids Go's deadlock detector (unlike select{} or bare channel read).
	for {
		time.Sleep(time.Hour)
	}
}

// doTrickle prints one dot per second forever (§3.6).
func doTrickle() {
	for {
		fmt.Print(".")
		os.Stdout.Sync()
		time.Sleep(time.Second)
	}
}

// doReadStdin reads stdin to EOF, then prints "ok" (§3.7).
func doReadStdin() {
	io.ReadAll(os.Stdin)
	fmt.Println("ok")
}

// doEcho prints remaining args joined by spaces (§3.8).
func doEcho() {
	fmt.Println(strings.Join(os.Args[2:], " "))
}

// doPrintArgs prints each remaining arg on its own line (§3.9).
func doPrintArgs() {
	for _, a := range os.Args[2:] {
		fmt.Println(a)
	}
}

// doCombo exercises multiple defences simultaneously (§3.11).
func doCombo() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "--combo requires <pidfile>")
		os.Exit(1)
	}
	pidFile := os.Args[2]
	// 1. Spawn grandchild.
	cmd := exec.Command(os.Args[0], "--hang")
	cmd.Start()
	if cmd.Process != nil {
		os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0644)
	}
	// 2. Flood stdout.
	os.Stdout.Write(bytes.Repeat([]byte("O"), 204800))
	// 3. Crash.
	var p *int
	println(*p) // deliberate nil deref
}

// requireByteCount parses os.Args[2] as an integer byte count.
func requireByteCount() int {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "%s requires <bytes>\n", os.Args[1])
		os.Exit(1)
	}
	n, err := strconv.Atoi(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid byte count: %s\n", os.Args[2])
		os.Exit(1)
	}
	return n
}
