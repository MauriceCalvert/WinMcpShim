package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger handles --verbose and --log output.
type Logger struct {
	mu      sync.Mutex
	writers []io.Writer
	file    *os.File
	bw      *bufio.Writer
}

// NewLogger creates a logger based on the verbose and logDir flags.
func NewLogger(verbose bool, logDir string) (*Logger, error) {
	l := &Logger{}
	if verbose {
		l.writers = append(l.writers, os.Stderr)
	}
	if logDir != "" {
		info, err := os.Stat(logDir)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("log directory does not exist: %s", logDir)
		}
		name := time.Now().Format("060102150405") + ".log"
		path := filepath.Join(logDir, name)
		f, err := os.Create(path)
		if err != nil {
			return nil, fmt.Errorf("create log file %s: %w", path, err)
		}
		l.file = f
		l.bw = bufio.NewWriter(f)
		l.writers = append(l.writers, l.bw)
	}
	return l, nil
}

// Close flushes and closes the log file if open.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.bw != nil {
		l.bw.Flush()
	}
	if l.file != nil {
		l.file.Close()
	}
}

// Enabled returns true if any logging output is configured.
func (l *Logger) Enabled() bool {
	return len(l.writers) > 0
}

// Log writes a tagged message to all configured outputs.
func (l *Logger) Log(tag, msg string) {
	if !l.Enabled() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	line := fmt.Sprintf("[shim:%s] %s\n", tag, msg)
	for _, w := range l.writers {
		w.Write([]byte(line))
	}
	if l.bw != nil {
		l.bw.Flush()
	}
}
