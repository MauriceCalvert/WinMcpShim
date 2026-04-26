// Package shared provides types, constants, and helpers used by both shim and tools.
package shared

import "time"

const (
	// MaxReadSize is the maximum bytes read returns without offset/limit (§5.2).
	MaxReadSize = 512 * 1024 // 512 KB
	// BinaryScanSize is the number of bytes checked for null bytes (§8.4).
	BinaryScanSize = 8 * 1024 // 8 KB
	// CRLFScanSize is the number of bytes scanned for CRLF detection (§5.3).
	CRLFScanSize = 4 * 1024 // 4 KB
	// MaxRetries for file locking (§9.2).
	MaxRetries = 3
	// DefaultMaxResults is the default cap for search results (§5.9).
	DefaultMaxResults = 100
	// DefaultInactivityTimeout in seconds (§6.3).
	DefaultInactivityTimeout = 10
	// DefaultTotalTimeout in seconds (§6.3).
	DefaultTotalTimeout = 300
	// DefaultMaxOutput in bytes (§6.5).
	DefaultMaxOutput = 100 * 1024 // 100 KB
	// DefaultMaxTimeout is the maximum inactivity timeout Claude can request (§6.6).
	DefaultMaxTimeout = 60
	// MaxLineSize is the maximum JSON-RPC line size (10 MB).
	MaxLineSize = 10 * 1024 * 1024
	// MaxProtocolVersion is the highest MCP protocol version this server supports.
	MaxProtocolVersion = "2025-06-18"
)

// RetryBackoffs are the durations for file lock retries (§9.2).
var RetryBackoffs = [MaxRetries]time.Duration{
	50 * time.Millisecond,
	200 * time.Millisecond,
	500 * time.Millisecond,
}
