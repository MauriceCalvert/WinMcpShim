# Contributing to WinMcpShim

## Building

Requires Go 1.25 or later.

```
go build -o winmcpshim.exe ./shim
cd strpatch && go build -o strpatch.exe . && cd ..
```

## Running tests

```
test.bat
```

Or run individual packages:

```
go test -count=1 -timeout 30s ./shared
go test -count=1 -timeout 60s ./tools
go test -count=1 -timeout 180s ./shim
go test -count=1 -timeout 30s ./installer
cd strpatch && go test -count=1 -timeout 30s .
```

## Code style

- One major type per file.
- Methods in alphabetical order within each file.
- Type-hint all function parameters and return values.
- Assert preconditions with `assert`-style checks (not try/catch).
- No blank lines inside function bodies.
- Constants in `shared/constants.go`.
- Frozen dataclasses (Go: no mutable shared state).
- No shell invocation — all child processes via `os/exec.Command`.

## Pull requests

- One feature or fix per PR.
- All tests must pass before merging.
- Update the RTM in `docs/winmcpshim-spec.md` §15 if adding or modifying requirements.
- Update `docs/winmcpshim-spec.md` if adding or changing tool behaviour.
- PR title: short imperative summary (e.g. "Add mkdir built-in tool").

## Spec authority

`docs/winmcpshim-spec.md` is the authoritative specification. If the code
disagrees with the spec, fix the code, not the spec. The only
exception is a dated amendment note in the spec acknowledging
a discovered error.

## Issue reporting

When filing an issue, include:

- Shim log file (from `--log` directory) covering the problem.
- Go version (`go version`).
- Windows version (`winver`).
- Relevant section of `shim.toml` (redact paths if needed).

## Licence

By contributing, you agree that your contributions will be licensed
under the MIT License.
