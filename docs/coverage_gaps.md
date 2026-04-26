# Coverage Gap Justification

Project: WinMcpShim
Date: 2026-04-02
Merged coverage: 88.0% (post-improvement, was 80.6%)

This document justifies every function below 100% that is expected to
remain uncovered. Each gap is categorised as:

- **T** — Tested but not instrumented. Exercised by integration tests
  that spawn the shim as a subprocess. The unit coverage tool cannot
  instrument code in a child process. Evidence: named integration test
  and/or binary coverage profile (cover-shim-binary.out).
- **D** — Defensive OS-level code. Error handling for Windows API calls
  that fail only under conditions not reproducible in tests (kernel
  resource exhaustion, driver-level errors). The code follows the
  standard check-return-value-and-return-error pattern.
- **A** — Accepted gap. Could theoretically be tested but effort is
  disproportionate to risk. Justification provided per item.

---

## shim/main.go — server main loop (T)

All functions in the main loop are exercised by integration tests in
shim_test.go. Unit coverage cannot see them because they run in a
subprocess. The binary coverage profile (cover-shim-binary.out) shows
these are hit.

| Function | Coverage | Evidence |
|----------|----------|----------|
| shimMain | 80.0% | TestProtocol_Initialize, TestShutdown_StdinEOF, TestShutdown_BrokenStdoutPipe, TestConfig_MalformedConfigRefusesStart |
| handleToolsCall | 80.7% | Every TestXxx_HappyPath and TestXxx_Error test in shim_test.go dispatches through this function |
| writeAndLog | 72.7% | TestShutdown_BrokenStdoutPipe (broken pipe path). Remaining gap: newline write error after JSON write succeeds — sub-millisecond race window |
| sendWarningNotification | 0.0% | Fires when grep fallback activates. TestGrep_FallbackRegistration triggers this indirectly. 8 lines of JSON marshalling |
| runScan | 70.0% | TestDiag_Scan. Error branch: scan directory doesn't exist. 2 lines of stderr output |
| negotiateProtocolVersion | 83.3% | TestProtocol_Initialize. Uncovered: client omits version field |
| NewLogger | 87.5% | TestDiag_Verbose, TestDiag_LogFile. Uncovered: log directory creation error |

## shared/ — Windows platform functions (D)

| Function | Coverage | Cat | Justification |
|----------|----------|-----|---------------|
| CreateJobObject error branch | 66.7% | D | Fails only on kernel resource exhaustion. Cannot trigger from user space. |
| AssignToJobObject error branch | 80.0% | D | Fails only on invalid handle. Cannot occur — Job Object created immediately before assignment. |
| VerifyPathByHandle API error | 81.2% | D | GetFinalPathNameByHandle fails on unmounted volume or corrupt NTFS. |
| FileCreationTime error branch | 71.4% | D | FindFirstFile fails on invalid path after os.Stat succeeded — race between two calls. |
| IsSharingViolation | 0.0% | T | Called by retry helpers. Exercised by TestRead_FileLocked (integration). Cannot unit-test without generating a real Windows sharing violation. |
| CheckResolvedPathConfinement | 87.5% | D | Uncovered branch: filepath.Abs fails on a valid-looking path. Defensive only. |

## shared/helpers.go — retry and atomic operations (T/D)

| Function | Coverage | Cat | Justification |
|----------|----------|-----|---------------|
| OpenFileWithRetry retry loop | 63.6% | T | Retry-on-sharing-violation exercised by TestRead_FileLocked (integration). Unit test covers happy path. Retry loop requires a file locked by another process with precise timing. |
| ReadFileWithRetry retry loop | 63.6% | T | Same mechanism. Used by Write CRLF detection, Edit file read. |
| StatWithRetry retry loop | 63.6% | T | Same mechanism. Used by Info. |
| AtomicWrite error paths | 63.2% | D | Uncovered: temp file creation failure, rename failure (cross-device). Temp file is created in same directory as target (eliminates cross-device). Rename failure requires disk-full or permission change during write. |
| CopyFile error path | 71.4% | A | Uncovered: os.Create on destination fails. Defensive — caller already checked destination doesn't exist. |
| CopyDir nested error | 77.8% | A | Uncovered: WalkDir encounters permission error mid-tree. |
| ConfigPath error branch | 75.0% | D | os.Executable() fails. Only possible if /proc/self/exe is unreadable (Linux) or the binary is deleted while running (Windows). |

## tools/ — uncovered branches in tool handlers (A)

| Function | Coverage | Cat | Justification |
|----------|----------|-----|---------------|
| readRange | 81.8% | A | Uncovered: Seek or ReadAt error on a successfully-opened file. Defensive against mid-read disk failure. |
| readUTF16 | 76.7% | A | Uncovered: UTF-16BE path (no BOM detection hit for BE in integration tests). Low risk — mirrors the UTF-16LE path which is tested. |
| tailSeek | 77.8% | A | Uncovered: Seek error on large file. Defensive against mid-read disk failure. |
| tailUTF16 | 67.6% | A | Uncovered: UTF-16BE tail path, plus error branches in decode. Same logic as the LE path. |
| FindStrpatch beside-exe and PATH | 54.5% | A | Requires controlling executable location and PATH. Integration tests cover the happy path (strpatch found and used). 15-line file search function. |
| ExecuteExternal working_dir | 83.9% | A | Uncovered: working directory set on command. Low risk — delegates to os/exec. |

---

## Summary

| Category | Description | Functions | Approx. % of total |
|----------|-------------|-----------|---------------------|
| T | Tested by integration tests, not instrumented | 9 | 5% |
| D | Defensive OS-level / disk-failure code | 8 | 4% |
| A | Accepted — disproportionate effort vs risk | 7 | 3% |
| **Total** | | **24** | **12%** |

## Security-critical function coverage

These are the functions that enforce the security boundary. All are
above 80%, with uncovered branches being exclusively Category D.

| Function | Coverage |
|----------|----------|
| CheckPathConfinement | 100.0% |
| CheckPathConfinementFull | 100.0% |
| CheckResolvedPathConfinement | 87.5% (D: filepath.Abs error) |
| CheckCommandConfinement | 100.0% |
| VerifyPathByHandle | 81.2% (D: GetFinalPathNameByHandle kernel error) |
| VerifyCommandByHandle | 100.0% |

**No uncovered code in security-critical paths is due to missing tests.**
All gaps are either OS kernel error returns (D) or integration test
instrumentation limits (T).
