# rogue.exe Specification

Version: 1.0.0
Date: 2026-04-02
Status: Draft

This document specifies rogue.exe, the adversarial test helper
for WinMcpShim. When the implementation diverges from this
document, the spec is correct and the code is a bug.

---

## 1. Purpose

rogue.exe is a compiled Go binary that deliberately misbehaves
in controlled ways. The shim's tests invoke it via the `run`
tool to exercise every defensive mechanism. It replaces
`testhelpers/crash/` and all Python one-liners in the test
suite, making the project zero external test dependencies.

Source: `testhelpers/rogue/main.go`. Built by `mage build`.

## 2. Design principles

- One binary, multiple modes selected by flag.
- Each mode maps to exactly one spec requirement in
  `winmcpshim-spec.md`.
- Every mode is deterministic — same flag, same behaviour.
- No mode modifies the filesystem except `--grandchild`
  (writes a PID file) and `--lock` (opens a file).
- rogue.exe does NOT create junctions or modify security
  settings. Those operations stay in test setup code where
  `t.Cleanup()` guarantees teardown.

## 3. Modes

### 3.1 --crash

Triggers a null pointer dereference (access violation).
No output. Process terminates with Windows exception code.

Replaces: `testhelpers/crash/main.go`.
Tests: §9.8.2 WER suppression.

### 3.2 --grandchild <pidfile>

Spawns a child process (another instance of rogue.exe with
`--hang`) that sleeps forever. Writes the child's PID to
`<pidfile>`. Then enters its own sleep loop, printing one
dot per second to stdout (to keep the inactivity timer alive).

The spawned child inherits the Job Object. When the shim
kills the parent via Job Object close, both parent and child
must die.

Replaces: Python `subprocess.Popen([...sleep...])` one-liner.
Tests: §9.8.1 Job Object grandchild kill.

### 3.3 --flood-stdout <bytes>

Writes exactly `<bytes>` bytes of 'O' to stdout, then exits 0.

Replaces: Python `sys.stdout.write('O'*N)`.
Tests: §6.5 output truncation.

### 3.4 --flood-both <bytes>

Writes exactly `<bytes>` bytes of 'O' to stdout and `<bytes>`
bytes of 'E' to stderr, then exits 0. Both writes happen
before exit — no interleaving required (stdout first, then
stderr, or concurrent — either is fine since the shim drains
both concurrently).

Replaces: Python `sys.stdout.write(...); sys.stderr.write(...)`.
Tests: §9.5 concurrent pipe draining / deadlock prevention.

### 3.5 --hang

Sleeps forever. No output. The process must be killed
externally (by inactivity timeout or Job Object).

Replaces: Python `time.sleep(30)` and `time.sleep(999)`.
Tests: §9.9.1 inactivity timeout, §6.6 timeout parameter.

### 3.6 --trickle

Prints one character per second to stdout, forever. The output
keeps the inactivity timer alive but the process never
finishes. Must be killed by total timeout.

Replaces: Python `[print(i) or time.sleep(1) for ...]`.
Tests: §9.9.2 total timeout.

### 3.7 --read-stdin

Reads stdin to EOF, then prints "ok" to stdout and exits 0.
When the shim connects stdin to DevNull, EOF arrives
immediately. If stdin were accidentally left open, this mode
would block forever — which is the bug it detects.

Replaces: Python `sys.stdin.read(); print('ok')`.
Tests: §9.6.2 child stdin EOF.

### 3.8 --echo <text...>

Prints all remaining arguments joined by spaces to stdout,
followed by a newline, then exits 0.

Example: `rogue.exe --echo hello world` → `hello world\n`
Example: `rogue.exe --echo "hello & echo injected"` → `hello & echo injected\n`

Replaces: Python `print('hello world')` and injection tests.
Tests: §5.11.2 argument splitting, §5.11 special characters.

### 3.9 --print-args

Prints each argument (after `--print-args`) on its own line
to stdout, then exits 0. For diagnosing how the shim split
the argument string.

Example: `rogue.exe --print-args a "b c" d` →
```
a
b c
d
```

Replaces: nothing directly — diagnostic aid for split debugging.

### 3.10 --lock <path>

Opens `<path>` with exclusive sharing (dwShareMode = 0),
then sleeps forever. The file cannot be read or written by
any other process while rogue holds it. Must be killed
externally.

Replaces: `lockFileExclusive` in `strpatch/lock_test_windows.go`
and `shim/lock_test_windows.go`.
Tests: §9.2 file locking retry.

Note: the lock tests in strpatch run strpatch.exe as a
subprocess. They need the file locked by a separate process,
not by the test process. rogue.exe serves this purpose.

### 3.11 --combo <pidfile>

Combined attack — exercises multiple defences simultaneously:

1. Spawn a grandchild (rogue.exe --hang), write PID to file.
2. Write 200 KB to stdout (triggers output truncation).
3. Trigger access violation (crash).

The shim must handle all three: Job Object kills the
grandchild, output truncation fires, WER suppression catches
the crash. If any defence interferes with another, this test
detects it.

Tests: no single existing requirement — tests interaction
between §9.8.1, §6.5, and §9.8.2.

## 4. Exit codes

| Code | Meaning |
|------|---------|
| 0 | Normal exit (modes that complete: echo, print-args, flood-stdout, flood-both, read-stdin) |
| — | Crash exit code (mode --crash: Windows exception, typically 0xC0000005) |
| — | Killed by parent (modes that never exit: hang, trickle, grandchild, lock, combo) |

Modes that never exit voluntarily have no defined exit code —
they are always terminated externally.

## 5. Build

```
cd testhelpers\rogue
go build -o ..\..\rogue.exe .
```

No dependencies beyond Go standard library plus
`golang.org/x/sys/windows` (for --lock sharing mode and
--crash). Same `golang.org/x/sys` version as the main
project.

Note: rogue.exe lives in the project root alongside
winmcpshim.exe. Tests reference it by building from source
in TestMain (same pattern as strpatch tests).

## 6. What rogue.exe does NOT do

- Create NTFS junctions or symlinks (stays in test setup).
- Modify files (except --grandchild PID file).
- Access the network.
- Modify registry or security settings.
- Anything not described in §3.

## 7. Requirements traceability

| Req | Mode | Shim spec § | Test |
|-----|------|-------------|------|
| RGE-01 | --crash | 9.8.2 | TestRun_WERSuppression |
| RGE-02 | --grandchild | 9.8.1 | TestRun_JobObjectGrandchildKilled |
| RGE-03 | --flood-stdout | 6.5 | TestRun_OutputTruncation |
| RGE-04 | --flood-both | 9.5 | TestRun_ConcurrentPipeDraining |
| RGE-05 | --hang | 9.9.1 | TestRun_InactivityTimeout |
| RGE-06 | --trickle | 9.9.2 | TestRun_TotalTimeout |
| RGE-07 | --read-stdin | 9.6.2 | TestRun_ChildStdinEOF |
| RGE-08 | --echo | 5.11.2 | TestRun_ArgumentSplitting |
| RGE-09 | --echo | 5.11 | TestRun_SpecialCharsNoInjection |
| RGE-10 | --print-args | — | (diagnostic) |
| RGE-11 | --lock | 9.2 | TestRead_FileLocked |
| RGE-12 | --combo | 9.8.1+6.5+9.8.2 | TestRun_ComboAttack (new) |

## 8. Migration

After rogue.exe is implemented and all tests are updated:

1. Delete `testhelpers/crash/` directory.
2. Remove all `python` references from `shim_test.go`.
3. Remove `crashExePath` variable and its setup from TestMain.
4. Add `rogueExePath` to TestMain (build from
   `testhelpers/rogue`).
5. Update each test to use rogue.exe modes instead of Python.
6. Verify `go test ./shim -count=1` passes with no Python
   on PATH.
