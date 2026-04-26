// bench — MCP server performance comparison tool.
//
// Benchmarks WinMcpShim against MCP filesystem server and Desktop Commander
// by sending identical JSON-RPC tool calls to each and timing the responses.
//
// Usage: go run ./cmd/bench
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// serverConfig describes how to spawn and talk to an MCP server.
type serverConfig struct {
	Name    string
	Command string
	Args    []string
	// Tool name mappings (canonical -> server-specific)
	ReadFile  string
	WriteFile string
	ListDir   string
	FileInfo  string
	GrepTool  string // empty = server has no grep
	// Parameter builders
	ReadParams  func(path string) map[string]interface{}
	WriteParams func(path, content string) map[string]interface{}
	ListParams  func(path string) map[string]interface{}
	InfoParams  func(path string) map[string]interface{}
	GrepParams  func(path, pattern string) map[string]interface{}
}

// session is a running MCP server subprocess.
type session struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr io.ReadCloser
}

// result holds timing data for one operation.
type result struct {
	Server    string
	Operation string
	Duration  time.Duration
	Error     string
}

// benchSuite holds all results for reporting.
type benchSuite struct {
	Results []result
}

func main() {
	// Locate executables
	shimExe := findShim()
	nodeExe, _ := exec.LookPath("node")
	fsServerJS := `C:\Users\Momo\AppData\Local\npm-cache\_npx\a3241bba59c344f5\node_modules\@modelcontextprotocol\server-filesystem\dist\index.js`
	dcServerJS := `C:\Users\Momo\AppData\Roaming\Claude\Claude Extensions\ant.dir.gh.wonderwhy-er.desktopcommandermcp\dist\index.js`

	// Create test fixtures
	testDir, cleanup := createFixtures()
	defer cleanup()

	servers := []serverConfig{
		{
			Name:      "WinMcpShim",
			Command:   shimExe,
			Args:      nil, // config is adjacent shim.toml
			ReadFile:  "read",
			WriteFile: "write",
			ListDir:   "list",
			FileInfo:  "info",
			GrepTool:  "grep",
			ReadParams: func(path string) map[string]interface{} {
				return map[string]interface{}{"path": path}
			},
			WriteParams: func(path, content string) map[string]interface{} {
				return map[string]interface{}{"path": path, "content": content}
			},
			ListParams: func(path string) map[string]interface{} {
				return map[string]interface{}{"path": path}
			},
			InfoParams: func(path string) map[string]interface{} {
				return map[string]interface{}{"path": path}
			},
			GrepParams: func(path, pattern string) map[string]interface{} {
				return map[string]interface{}{"path": path, "pattern": pattern, "recursive": true}
			},
		},
	}

	// Add Node.js-based servers only if node and their entry points exist
	if nodeExe != "" {
		if _, err := os.Stat(fsServerJS); err == nil {
			servers = append(servers, serverConfig{
				Name: "Filesystem", Command: nodeExe, Args: []string{fsServerJS, testDir},
				ReadFile: "read_file", WriteFile: "write_file", ListDir: "list_directory", FileInfo: "get_file_info",
				ReadParams:  func(path string) map[string]interface{} { return map[string]interface{}{"path": path} },
				WriteParams: func(path, content string) map[string]interface{} { return map[string]interface{}{"path": path, "content": content} },
				ListParams:  func(path string) map[string]interface{} { return map[string]interface{}{"path": path} },
				InfoParams:  func(path string) map[string]interface{} { return map[string]interface{}{"path": path} },
			})
		} else {
			fmt.Printf("SKIP: Filesystem server not found at %s\n", fsServerJS)
		}
		if _, err := os.Stat(dcServerJS); err == nil {
			servers = append(servers, serverConfig{
				Name: "DesktopCmd", Command: nodeExe, Args: []string{dcServerJS},
				ReadFile: "read_file", WriteFile: "write_file", ListDir: "list_directory", FileInfo: "get_file_info",
				ReadParams:  func(path string) map[string]interface{} { return map[string]interface{}{"path": path} },
				WriteParams: func(path, content string) map[string]interface{} { return map[string]interface{}{"path": path, "content": content} },
				ListParams:  func(path string) map[string]interface{} { return map[string]interface{}{"path": path} },
				InfoParams:  func(path string) map[string]interface{} { return map[string]interface{}{"path": path} },
			})
		} else {
			fmt.Printf("SKIP: Desktop Commander not found at %s\n", dcServerJS)
		}
	} else {
		fmt.Println("SKIP: node not on PATH — skipping Filesystem and Desktop Commander")
	}

	suite := &benchSuite{}

	// Fixture paths
	smallFile := filepath.Join(testDir, "small.txt")
	largeFile := filepath.Join(testDir, "large.txt")
	writeTarget := filepath.Join(testDir, "write_target.txt")
	listDir := filepath.Join(testDir, "listdir")
	grepDir := filepath.Join(testDir, "grepdir")

	fmt.Println("MCP Server Benchmark")
	fmt.Println("====================")
	fmt.Printf("Test directory: %s\n", testDir)
	fmt.Printf("Iterations per operation: %d\n\n", 20)

	for _, srv := range servers {
		fmt.Printf("--- %s ---\n", srv.Name)

		// Measure cold start
		coldStart := measureColdStart(srv)
		suite.Results = append(suite.Results, result{
			Server: srv.Name, Operation: "cold_start", Duration: coldStart,
		})
		fmt.Printf("  cold start:    %s\n", fmtDur(coldStart))

		// Start a persistent session for the remaining benchmarks
		s, err := startSession(srv)
		if err != nil {
			fmt.Printf("  ERROR: could not start: %v\n", err)
			continue
		}
		if err := handshake(s); err != nil {
			fmt.Printf("  ERROR: handshake failed: %v\n", err)
			s.close()
			continue
		}

		// Read small file (1 KB)
		med := benchOp(s, srv.Name, "read_1kb", srv.ReadFile, srv.ReadParams(smallFile), 20)
		suite.Results = append(suite.Results, med)
		fmt.Printf("  read 1KB:      %s\n", fmtResult(med))

		// Read large file (100 KB)
		med = benchOp(s, srv.Name, "read_100kb", srv.ReadFile, srv.ReadParams(largeFile), 20)
		suite.Results = append(suite.Results, med)
		fmt.Printf("  read 100KB:    %s\n", fmtResult(med))

		// List directory (50 entries)
		med = benchOp(s, srv.Name, "list_dir", srv.ListDir, srv.ListParams(listDir), 20)
		suite.Results = append(suite.Results, med)
		fmt.Printf("  list dir:      %s\n", fmtResult(med))

		// File info
		med = benchOp(s, srv.Name, "file_info", srv.FileInfo, srv.InfoParams(smallFile), 20)
		suite.Results = append(suite.Results, med)
		fmt.Printf("  file info:     %s\n", fmtResult(med))

		// Write file
		med = benchOp(s, srv.Name, "write_file", srv.WriteFile, srv.WriteParams(writeTarget, "benchmark write content\n"), 20)
		suite.Results = append(suite.Results, med)
		fmt.Printf("  write file:    %s\n", fmtResult(med))

		// Throughput: 100 sequential reads
		tp := benchThroughput(s, srv.Name, srv.ReadFile, srv.ReadParams(smallFile), 100)
		suite.Results = append(suite.Results, tp)
		fmt.Printf("  throughput:    %s\n", fmtResult(tp))

		// Grep: single file
		if srv.GrepTool != "" {
			med = benchOp(s, srv.Name, "grep_1file (builtin)", srv.GrepTool, srv.GrepParams(largeFile, "benchmark"), 20)
			suite.Results = append(suite.Results, med)
			fmt.Printf("  grep 1 file:   %s\n", fmtResult(med))

			// Grep: recursive directory tree (200 files)
			med = benchOp(s, srv.Name, "grep_200files (builtin)", srv.GrepTool, srv.GrepParams(grepDir, "NEEDLE"), 20)
			suite.Results = append(suite.Results, med)
			fmt.Printf("  grep 200 files:%s\n", fmtResult(med))
		}

		s.close()
		fmt.Println()
	}

	// Print comparison table
	printComparison(suite)

	// Built-in vs external grep comparison
	fmt.Println()
	fmt.Println("Grep: Built-in vs External")
	fmt.Println("==========================")
	benchGrepComparison(shimExe, testDir, grepDir)
}

// measureColdStart spawns the server and times until initialize response.
func measureColdStart(srv serverConfig) time.Duration {
	start := time.Now()
	s, err := startSession(srv)
	if err != nil {
		return 0
	}
	err = handshake(s)
	elapsed := time.Since(start)
	s.close()
	if err != nil {
		return 0
	}
	return elapsed
}

// benchOp runs an operation N times and returns the median timing.
func benchOp(s *session, server, opName, toolName string, params map[string]interface{}, n int) result {
	var durations []time.Duration
	idBase := int(time.Now().UnixNano() % 100000)
	for i := 0; i < n; i++ {
		start := time.Now()
		_, callErr := callTool(s, idBase+i, toolName, params)
		elapsed := time.Since(start)
		if callErr != "" {
			return result{Server: server, Operation: opName, Error: callErr}
		}
		durations = append(durations, elapsed)
	}
	// Filter out zero-duration samples caused by buffered reads
	var nonzero []time.Duration
	for _, d := range durations {
		if d > 0 {
			nonzero = append(nonzero, d)
		}
	}
	if len(nonzero) == 0 {
		return result{Server: server, Operation: opName, Error: "all samples zero"}
	}
	sort.Slice(nonzero, func(a, b int) bool { return nonzero[a] < nonzero[b] })
	return result{Server: server, Operation: opName, Duration: nonzero[len(nonzero)/2]}
}

// benchThroughput runs N sequential calls and reports calls/second.
func benchThroughput(s *session, server, toolName string, params map[string]interface{}, n int) result {
	start := time.Now()
	for i := 0; i < n; i++ {
		_, err := callTool(s, i+1000, toolName, params)
		if err != "" {
			return result{Server: server, Operation: "throughput", Error: err}
		}
	}
	elapsed := time.Since(start)
	callsPerSec := float64(n) / elapsed.Seconds()
	return result{
		Server:    server,
		Operation: fmt.Sprintf("throughput (%d calls)", n),
		Duration:  time.Duration(float64(time.Second) / callsPerSec), // store as per-call time
	}
}

// startSession spawns an MCP server subprocess.
func startSession(srv serverConfig) (*session, error) {
	cmd := exec.Command(srv.Command, srv.Args...)
	cmd.Stderr = nil // discard stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &session{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdoutPipe), stderr: stderrPipe}, nil
}

func (s *session) close() {
	s.stdin.Close()
	s.cmd.Wait()
}

// handshake performs the MCP initialize/initialized exchange.
func handshake(s *session) error {
	msg := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"bench","version":"1.0"}}}`
	if _, err := fmt.Fprintf(s.stdin, "%s\n", msg); err != nil {
		return err
	}
	if _, err := s.stdout.ReadBytes('\n'); err != nil {
		return fmt.Errorf("no response to initialize: %w", err)
	}
	// Send initialized notification
	if _, err := fmt.Fprintf(s.stdin, `{"jsonrpc":"2.0","method":"initialized"}`+"\n"); err != nil {
		return err
	}
	time.Sleep(20 * time.Millisecond) // let server process notification
	return nil
}

// callTool sends a tools/call and reads the response. Returns text and error string.
func callTool(s *session, id int, name string, args map[string]interface{}) (string, string) {
	argsJSON, _ := json.Marshal(args)
	msg := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, id, name, argsJSON)
	if _, err := fmt.Fprintf(s.stdin, "%s\n", msg); err != nil {
		return "", fmt.Sprintf("write error: %v", err)
	}
	line, err2 := s.stdout.ReadBytes('\n')
	if err2 != nil {
		return "", fmt.Sprintf("read error: %v", err2)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(line, &resp); err != nil {
		return "", fmt.Sprintf("unmarshal: %v", err)
	}
	if errObj, ok := resp["error"]; ok {
		return "", fmt.Sprintf("rpc error: %v", errObj)
	}
	// Check for tool-level error (isError in result)
	if result, ok := resp["result"].(map[string]interface{}); ok {
		if isErr, ok := result["isError"].(bool); ok && isErr {
			if content, ok := result["content"].([]interface{}); ok && len(content) > 0 {
				if item, ok := content[0].(map[string]interface{}); ok {
					return "", fmt.Sprintf("tool error: %v", item["text"])
				}
			}
			return "", "tool error (unknown)"
		}
	}
	return string(line), ""
}

// createFixtures creates test files for benchmarking.
func createFixtures() (string, func()) {
	dir, err := os.MkdirTemp("", "mcpbench-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}
	// Small file (1 KB)
	os.WriteFile(filepath.Join(dir, "small.txt"), []byte(strings.Repeat("Hello, world! This is a benchmark test line.\n", 23)), 0644)
	// Large file (100 KB)
	os.WriteFile(filepath.Join(dir, "large.txt"), []byte(strings.Repeat("A line of text for the large file benchmark test.\n", 2048)), 0644)
	// Directory with 50 files
	listDir := filepath.Join(dir, "listdir")
	os.Mkdir(listDir, 0755)
	for i := 0; i < 50; i++ {
		os.WriteFile(filepath.Join(listDir, fmt.Sprintf("file_%03d.txt", i)), []byte(fmt.Sprintf("file %d\n", i)), 0644)
	}
	// Grep fixtures: 200 files across 10 subdirectories, 5 contain the NEEDLE
	grepDir := filepath.Join(dir, "grepdir")
	for d := 0; d < 10; d++ {
		sub := filepath.Join(grepDir, fmt.Sprintf("pkg%02d", d))
		os.MkdirAll(sub, 0755)
		for f := 0; f < 20; f++ {
			content := fmt.Sprintf("// Package pkg%02d file %d\nfunc process() {\n\treturn nil\n}\n", d, f)
			if d*20+f == 7 || d*20+f == 53 || d*20+f == 99 || d*20+f == 141 || d*20+f == 188 {
				content += "// NEEDLE: this line should be found by grep\n"
			}
			os.WriteFile(filepath.Join(sub, fmt.Sprintf("file_%03d.go", f)), []byte(content), 0644)
		}
	}
	// Write shim.toml for WinMcpShim (allowed_roots = testdir)
	// Save and restore the user's shim.toml
	shimDir := filepath.Dir(findShim())
	shimTomlPath := filepath.Join(shimDir, "shim.toml")
	origToml, hadOrig := readFileIfExists(shimTomlPath)
	shimToml := fmt.Sprintf(`[security]
allowed_roots = [%q]
max_timeout = 60

[run]
inactivity_timeout = 10
total_timeout = 300
max_output = 102400

[builtin_descriptions]
cat = "cat"
copy = "copy"
delete = "delete"
diff = "diff"
edit = "edit"
head = "head"
info = "info"
list = "list"
mkdir = "mkdir"
move = "move"
read = "read"
roots = "roots"
run = "run"
search = "search"
tail = "tail"
tree = "tree"
wc = "wc"
write = "write"
`, dir)
	os.WriteFile(shimTomlPath, []byte(shimToml), 0644)

	return dir, func() {
		os.RemoveAll(dir)
		if hadOrig {
			os.WriteFile(shimTomlPath, origToml, 0644)
		} else {
			os.Remove(shimTomlPath)
		}
	}
}

// findShim locates winmcpshim.exe.
func findShim() string {
	// Look adjacent to the bench binary first, then in known location
	candidates := []string{
		filepath.Join(`D:\projects\WinMcpShim`, "bin", "winmcpshim.exe"),
		filepath.Join(filepath.Dir(os.Args[0]), "winmcpshim.exe"),
		filepath.Join(filepath.Dir(os.Args[0]), "..", "bin", "winmcpshim.exe"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	fmt.Fprintln(os.Stderr, "ERROR: winmcpshim.exe not found. Build it first: go build -o winmcpshim.exe ./shim")
	os.Exit(1)
	return ""
}

func fmtDur(d time.Duration) string {
	if d == 0 {
		return "ERROR"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.0f us", float64(d.Microseconds()))
	}
	return fmt.Sprintf("%.1f ms", float64(d.Microseconds())/1000.0)
}

func fmtResult(r result) string {
	if r.Error != "" {
		return "ERR: " + r.Error
	}
	return fmtDur(r.Duration)
}

// printComparison prints a side-by-side comparison table.
func printComparison(suite *benchSuite) {
	// Group by operation
	type opRow struct {
		op      string
		servers map[string]time.Duration
		errors  map[string]string
	}
	ops := make(map[string]*opRow)
	var opOrder []string
	for _, r := range suite.Results {
		row, ok := ops[r.Operation]
		if !ok {
			row = &opRow{op: r.Operation, servers: make(map[string]time.Duration), errors: make(map[string]string)}
			ops[r.Operation] = row
			opOrder = append(opOrder, r.Operation)
		}
		if r.Error != "" {
			row.errors[r.Server] = r.Error
		} else {
			row.servers[r.Server] = r.Duration
		}
	}

	fmt.Println("Comparison Table")
	fmt.Println("================")
	fmt.Printf("%-25s %15s %15s %15s\n", "Operation", "WinMcpShim", "Filesystem", "DesktopCmd")
	fmt.Println(strings.Repeat("-", 72))
	for _, opName := range opOrder {
		row := ops[opName]
		vals := [3]string{}
		names := [3]string{"WinMcpShim", "Filesystem", "DesktopCmd"}
		for i, name := range names {
			if _, ok := row.errors[name]; ok {
				vals[i] = "ERR"
			} else if d, ok := row.servers[name]; ok {
				vals[i] = fmtDur(d)
			} else {
				vals[i] = "-"
			}
		}
		fmt.Printf("%-25s %15s %15s %15s\n", row.op, vals[0], vals[1], vals[2])
	}

	// Print speedup summary
	fmt.Println()
	fmt.Println("Speedup vs WinMcpShim")
	fmt.Println("=====================")
	for _, opName := range opOrder {
		row := ops[opName]
		shimDur, shimOK := row.servers["WinMcpShim"]
		if !shimOK || shimDur == 0 {
			continue
		}
		for _, other := range []string{"Filesystem", "DesktopCmd"} {
			otherDur, ok := row.servers[other]
			if !ok || otherDur == 0 {
				continue
			}
			ratio := float64(otherDur) / float64(shimDur)
			fmt.Printf("  %-20s %s: %.1fx %s\n", opName, other, ratio, speedLabel(ratio))
		}
	}
}

func speedLabel(ratio float64) string {
	if ratio > 1.1 {
		return "(shim faster)"
	}
	if ratio < 0.9 {
		return "(shim slower)"
	}
	return "(similar)"
}

// benchGrepComparison compares built-in grep (in-process) against external
// grep (spawning grep.exe via configured tool) using the same shim binary.
func benchGrepComparison(shimExe, testDir, grepDir string) {
	grepExe := `C:\Program Files\Git\usr\bin\grep.exe`
	if _, err := os.Stat(grepExe); err != nil {
		fmt.Println("  SKIP: Git grep.exe not found at", grepExe)
		return
	}
	// Config WITHOUT external grep → uses built-in
	builtinConfig := fmt.Sprintf(`[security]
allowed_roots = [%q]
max_timeout = 60

[run]
inactivity_timeout = 10
total_timeout = 300
max_output = 102400

[builtin_descriptions]
cat = "cat"
copy = "copy"
delete = "delete"
diff = "diff"
edit = "edit"
head = "head"
info = "info"
list = "list"
mkdir = "mkdir"
move = "move"
read = "read"
roots = "roots"
run = "run"
search = "search"
tail = "tail"
tree = "tree"
wc = "wc"
write = "write"
`, testDir)
	// Config WITH external grep → spawns grep.exe
	externalConfig := fmt.Sprintf(`[security]
allowed_roots = [%q]
max_timeout = 60

[run]
inactivity_timeout = 10
total_timeout = 300
max_output = 102400

[builtin_descriptions]
cat = "cat"
copy = "copy"
delete = "delete"
diff = "diff"
edit = "edit"
head = "head"
info = "info"
list = "list"
mkdir = "mkdir"
move = "move"
read = "read"
roots = "roots"
run = "run"
search = "search"
tail = "tail"
tree = "tree"
wc = "wc"
write = "write"

[tools.grep]
exe = %q
description = "Search file contents by regex."
inactivity_timeout = 10
total_timeout = 300
max_output = 102400
success_codes = [0, 1]

[tools.grep.params]
pattern = { type = "string", description = "Regex pattern", required = true, position = 1 }
path = { type = "string", description = "File or directory", required = true, position = 2 }
recursive = { type = "boolean", description = "Search subdirectories", default = true, flag = "-r" }
line_numbers = { type = "boolean", description = "Show line numbers", default = true, flag = "-n" }
`, testDir, grepExe)
	grepParams := map[string]interface{}{"path": grepDir, "pattern": "NEEDLE", "recursive": true}
	singleParams := map[string]interface{}{"path": filepath.Join(grepDir, "pkg00", "file_007.go"), "pattern": "NEEDLE"}
	shimDir := filepath.Dir(shimExe)
	shimToml := filepath.Join(shimDir, "shim.toml")
	n := 20
	// Benchmark built-in grep
	os.WriteFile(shimToml, []byte(builtinConfig), 0644)
	srv := serverConfig{Name: "built-in", Command: shimExe}
	s, err := startSession(srv)
	if err != nil {
		fmt.Printf("  ERROR starting built-in session: %v\n", err)
		return
	}
	if err := handshake(s); err != nil {
		fmt.Printf("  ERROR handshake: %v\n", err)
		s.close()
		return
	}
	builtinSingle := benchOp(s, "built-in", "grep_1file", "grep", singleParams, n)
	builtinTree := benchOp(s, "built-in", "grep_200files", "grep", grepParams, n)
	s.close()
	fmt.Printf("  %-20s %12s %12s\n", "", "built-in", "external")
	fmt.Printf("  %-20s %12s", "1 file", fmtResult(builtinSingle))
	// Benchmark external grep
	os.WriteFile(shimToml, []byte(externalConfig), 0644)
	srv2 := serverConfig{Name: "external", Command: shimExe}
	s2, err := startSession(srv2)
	if err != nil {
		fmt.Printf("  ERROR starting external session: %v\n", err)
		return
	}
	if err := handshake(s2); err != nil {
		fmt.Printf("  ERROR handshake: %v\n", err)
		s2.close()
		return
	}
	externalSingle := benchOp(s2, "external", "grep_1file", "grep", singleParams, n)
	externalTree := benchOp(s2, "external", "grep_200files", "grep", grepParams, n)
	s2.close()
	fmt.Printf(" %12s", fmtResult(externalSingle))
	if builtinSingle.Duration > 0 && externalSingle.Duration > 0 {
		fmt.Printf("  %.1fx", float64(externalSingle.Duration)/float64(builtinSingle.Duration))
	}
	fmt.Println()
	fmt.Printf("  %-20s %12s %12s", "200 files recursive", fmtResult(builtinTree), fmtResult(externalTree))
	if builtinTree.Duration > 0 && externalTree.Duration > 0 {
		fmt.Printf("  %.1fx", float64(externalTree.Duration)/float64(builtinTree.Duration))
	}
	fmt.Println()
}

// readFileIfExists reads a file and returns (content, true) or (nil, false).
func readFileIfExists(path string) ([]byte, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// Silence unused import warning for math package if not used elsewhere.
var _ = math.Abs
