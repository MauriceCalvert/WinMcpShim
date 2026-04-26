package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/MauriceCalvert/WinMcpShim/shared"
)

// ExecOpts carries all parameters for ExecuteWithTimeouts.
type ExecOpts struct {
	InactivityTimeout time.Duration
	TotalTimeout      time.Duration
	MaxOutput         int
	ToolName          string // for diagnostic messages (§6.3)
	MaxTimeout        int    // max allowed timeout, for diagnostic messages (§6.6)
}

// ExecResult holds the outcome of a child process execution.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Timeout  string // non-empty if killed by timeout
}

// Run executes an arbitrary command (§5.11).
func Run(params map[string]interface{}, allowedRoots []string, runCfg shared.RunConfig, maxTimeout int) (string, error) {
	command, err := shared.RequireString(params, "command")
	if err != nil {
		return "", err
	}
	if len(runCfg.AllowedCommands) > 0 {
		if err := shared.CheckRunPermission(command, allowedRoots, runCfg.AllowedCommands); err != nil {
			return "", err
		}
	} else {
		if err := shared.CheckCommandConfinement(command, allowedRoots); err != nil {
			return "", err
		}
	}
	argsStr, _ := shared.OptionalString(params, "args")
	var args []string
	if argsStr != "" {
		args = SplitArgs(argsStr)
	}
	inactivitySec := runCfg.InactivityTimeout
	if t, ok := shared.OptionalInt(params, "timeout"); ok {
		inactivitySec = ClampTimeout(t, maxTimeout)
	}
	opts := ExecOpts{
		InactivityTimeout: time.Duration(inactivitySec) * time.Second,
		TotalTimeout:      time.Duration(runCfg.TotalTimeout) * time.Second,
		MaxOutput:         runCfg.MaxOutput,
		ToolName:          "run",
		MaxTimeout:        maxTimeout,
	}
	result, err := ExecuteWithTimeouts(command, args, opts)
	if err != nil {
		return "", err
	}
	if result.Timeout != "" {
		return "", fmt.Errorf("%s", result.Timeout)
	}
	return FormatRunResult(result), nil
}

// FormatRunResult formats stdout, stderr, and exit code for the run tool (§6.4).
func FormatRunResult(r ExecResult) string {
	var sb strings.Builder
	if r.Stdout != "" {
		sb.WriteString(r.Stdout)
	}
	if r.Stderr != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n--- stderr ---\n")
		}
		sb.WriteString(r.Stderr)
	}
	if r.ExitCode != 0 {
		fmt.Fprintf(&sb, "\n[exit code: %d]", r.ExitCode)
	}
	return sb.String()
}

// ClampTimeout clamps a timeout value to [1, maxTimeout] (§6.6).
func ClampTimeout(t int, maxTimeout int) int {
	if t < 1 {
		return 1
	}
	if t > maxTimeout {
		return maxTimeout
	}
	return t
}

// ExecuteWithTimeouts runs a command with concurrent pipe draining (§9.5),
// inactivity and total timeouts (§6.3), Job Objects and WER suppression (§9.8),
// output size limit (§6.5), and proper kill/cleanup sequence (§9.8).
func ExecuteWithTimeouts(command string, args []string, opts ExecOpts) (ExecResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), opts.TotalTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, args...)
	shared.SetupChildProcess(cmd)
	cmd.Stdin = nil
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return ExecResult{}, fmt.Errorf("create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return ExecResult{}, fmt.Errorf("create stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return ExecResult{}, fmt.Errorf("start %s: %w", command, err)
	}
	job, _ := shared.CreateJobObject()
	if job != shared.NoJobHandle {
		if err := shared.AssignToJobObject(job, cmd.Process); err != nil {
			shared.CloseJobObject(job)
			job = shared.NoJobHandle
		}
	}
	defer func() { shared.CloseJobObject(job) }()
	var stdoutBuf, stderrBuf bytes.Buffer
	var mu sync.Mutex
	truncated := false
	stderrTruncated := false
	truncChan := make(chan struct{}, 1)
	timer := time.NewTimer(opts.InactivityTimeout)
	defer timer.Stop()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, readErr := stdoutPipe.Read(buf)
			if n > 0 {
				mu.Lock()
				timer.Reset(opts.InactivityTimeout)
				remaining := opts.MaxOutput - stdoutBuf.Len()
				if remaining > 0 {
					writeN := n
					if writeN > remaining {
						writeN = remaining
					}
					stdoutBuf.Write(buf[:writeN])
					if writeN < n {
						truncated = true
					}
				} else {
					truncated = true
				}
				wasTrunc := truncated
				mu.Unlock()
				if wasTrunc {
					select {
					case truncChan <- struct{}{}:
					default:
					}
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, readErr := stderrPipe.Read(buf)
			if n > 0 {
				mu.Lock()
				timer.Reset(opts.InactivityTimeout)
				remaining := opts.MaxOutput - stderrBuf.Len()
				if remaining > 0 {
					writeN := n
					if writeN > remaining {
						writeN = remaining
					}
					stderrBuf.Write(buf[:writeN])
					if writeN < n {
						stderrTruncated = true
					}
				} else {
					stderrTruncated = true
				}
				mu.Unlock()
			}
			if readErr != nil {
				return
			}
		}
	}()
	pipeDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(pipeDone)
	}()
	type termCause int
	const (
		causeNormal termCause = iota
		causeInactivity
		causeTotal
		causeTruncation
	)
	cause := causeNormal
	killTree := func() {
		shared.CloseJobObject(job)
		job = shared.NoJobHandle
		cmd.Process.Kill()
	}
	select {
	case <-pipeDone:
		// Normal completion.
	case <-timer.C:
		cause = causeInactivity
		killTree()
		<-pipeDone
	case <-ctx.Done():
		cause = causeTotal
		killTree()
		<-pipeDone
	case <-truncChan:
		cause = causeTruncation
		killTree()
		<-pipeDone
	}
	switch cause {
	case causeInactivity:
		return ExecResult{
			Timeout: fmt.Sprintf("winmcpshim: %s produced no output for %d seconds.\nYou can retry with a higher timeout (max %d).",
				opts.ToolName, int(opts.InactivityTimeout.Seconds()), opts.MaxTimeout),
		}, nil
	case causeTotal:
		return ExecResult{
			Timeout: fmt.Sprintf("winmcpshim: %s exceeded total timeout of %d seconds.",
				opts.ToolName, int(opts.TotalTimeout.Seconds())),
		}, nil
	}
	waitErr := cmd.Wait()
	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() != nil {
			return ExecResult{
				Timeout: fmt.Sprintf("winmcpshim: %s exceeded total timeout of %d seconds.",
					opts.ToolName, int(opts.TotalTimeout.Seconds())),
			}, nil
		} else {
			return ExecResult{}, fmt.Errorf("wait %s: %w", command, waitErr)
		}
	}
	mu.Lock()
	stdout := stdoutBuf.String()
	wasTrunc := truncated
	stderr := stderrBuf.String()
	wasStderrTrunc := stderrTruncated
	mu.Unlock()
	if wasTrunc {
		stdout += fmt.Sprintf("\n[truncated -- output exceeded %d KB]", opts.MaxOutput/1024)
	}
	if wasStderrTrunc {
		stderr += "\n[stderr truncated]"
	}
	return ExecResult{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
	}, nil
}

// SplitArgs splits a command-line argument string into individual arguments (§5.11).
func SplitArgs(s string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote {
			if c == quoteChar {
				inQuote = false
			} else {
				current.WriteByte(c)
			}
		} else if c == '"' || c == '\'' {
			inQuote = true
			quoteChar = c
		} else if c == ' ' || c == '\t' {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

// ExecuteExternal runs a configured external tool with proper lifecycle management.
func ExecuteExternal(name string, toolCfg shared.ToolConfig, args []string, maxTimeout int) (string, error) {
	inactivity := time.Duration(toolCfg.InactivityTimeout) * time.Second
	if inactivity == 0 {
		inactivity = time.Duration(shared.DefaultInactivityTimeout) * time.Second
	}
	total := time.Duration(toolCfg.TotalTimeout) * time.Second
	if total == 0 {
		total = time.Duration(shared.DefaultTotalTimeout) * time.Second
	}
	maxOutput := toolCfg.MaxOutput
	if maxOutput == 0 {
		maxOutput = shared.DefaultMaxOutput
	}
	opts := ExecOpts{
		InactivityTimeout: inactivity,
		TotalTimeout:      total,
		MaxOutput:         maxOutput,
		ToolName:          name,
		MaxTimeout:        maxTimeout,
	}
	result, err := ExecuteWithTimeouts(toolCfg.Exe, args, opts)
	if err != nil {
		return "", err
	}
	if result.Timeout != "" {
		return "", fmt.Errorf("%s", result.Timeout)
	}
	isSuccess := false
	if len(toolCfg.SuccessCodes) == 0 {
		isSuccess = result.ExitCode == 0
	} else {
		for _, code := range toolCfg.SuccessCodes {
			if result.ExitCode == code {
				isSuccess = true
				break
			}
		}
	}
	var sb strings.Builder
	sb.WriteString(result.Stdout)
	if result.Stderr != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n--- stderr ---\n")
		}
		sb.WriteString(result.Stderr)
	}
	if !isSuccess {
		return "", fmt.Errorf("exit code %d: %s", result.ExitCode, sb.String())
	}
	return sb.String(), nil
}
