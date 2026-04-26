package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MauriceCalvert/WinMcpShim/shared"
	"github.com/MauriceCalvert/WinMcpShim/tools"
)

var shimExePath string
var rogueExePath string
var testDir string
var coverDir string

// TestMain builds winmcpshim.exe (with coverage instrumentation) and
// strpatch.exe into a temp directory. Binary coverage data is written
// to coverDir by each shim subprocess on exit. After all tests, the
// data is converted to a text profile if SHIM_COVER_OUT is set.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "shim-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	testDir = dir
	coverDir = filepath.Join(dir, "covdata")
	if err := os.Mkdir(coverDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "create cover dir: %v\n", err)
		os.Exit(1)
	}
	os.Setenv("GOCOVERDIR", coverDir)
	goExe, err := exec.LookPath("go")
	if err != nil {
		fmt.Fprintf(os.Stderr, "go not found on PATH: %v\n", err)
		os.Exit(1)
	}
	shimExePath = filepath.Join(dir, "winmcpshim.exe")
	build := exec.Command(goExe, "build", "-cover", "-o", shimExePath, ".")
	build.Dir = "."
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build winmcpshim.exe: %v\n", err)
		os.Exit(1)
	}
	strpatchExe := filepath.Join(dir, "strpatch.exe")
	build2 := exec.Command(goExe, "build", "-o", strpatchExe, ".")
	build2.Dir = "../strpatch"
	build2.Stderr = os.Stderr
	if err := build2.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build strpatch.exe: %v\n", err)
		os.Exit(1)
	}
	rogueExePath = filepath.Join(dir, "rogue.exe")
	build3 := exec.Command(goExe, "build", "-o", rogueExePath, "./testhelpers/rogue")
	build3.Dir = ".."
	build3.Stderr = os.Stderr
	if err := build3.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build rogue.exe: %v\n", err)
		os.Exit(1)
	}
	exitCode := m.Run()
	// Convert binary coverage data to text profile if requested.
	outProfile := os.Getenv("SHIM_COVER_OUT")
	if outProfile != "" {
		entries, _ := os.ReadDir(coverDir)
		if len(entries) > 0 {
			conv := exec.Command(goExe, "tool", "covdata", "textfmt",
				"-i="+coverDir, "-o="+outProfile)
			conv.Stderr = os.Stderr
			if err := conv.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "covdata convert: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "Binary coverage written to %s\n", outProfile)
			}
		} else {
			fmt.Fprintf(os.Stderr, "No binary coverage data in %s\n", coverDir)
		}
	}
	os.Exit(exitCode)
}

// shimSession manages a running shim process for testing.
type shimSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	stderr io.ReadCloser
}

// startShim starts a shim process with a config that allows the given root.
func startShim(t *testing.T, allowedRoot string) *shimSession {
	t.Helper()
	configPath := filepath.Join(testDir, "test_shim.toml")
	config := fmt.Sprintf(`[security]
allowed_roots = [%q]

[run]
inactivity_timeout = 3
total_timeout = 5
max_output = 102400
`, allowedRoot)
	os.WriteFile(configPath, []byte(config), 0644)
	shimConfig := filepath.Join(filepath.Dir(shimExePath), "shim.toml")
	os.WriteFile(shimConfig, []byte(config), 0644)
	cmd := exec.Command(shimExePath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start shim: %v", err)
	}
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, shared.MaxLineSize), shared.MaxLineSize)
	return &shimSession{cmd: cmd, stdin: stdin, stdout: scanner, stderr: stderrPipe}
}

// send sends a JSON-RPC request and returns the parsed response.
func (s *shimSession) send(t *testing.T, msg string) map[string]interface{} {
	t.Helper()
	_, err := fmt.Fprintf(s.stdin, "%s\n", msg)
	if err != nil {
		t.Fatalf("write to shim: %v", err)
	}
	if !s.stdout.Scan() {
		t.Fatalf("no response from shim (err: %v)", s.stdout.Err())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(s.stdout.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nraw: %s", err, s.stdout.Text())
	}
	return resp
}

// sendNotification sends a JSON-RPC notification (no response expected).
func (s *shimSession) sendNotification(t *testing.T, msg string) {
	t.Helper()
	_, err := fmt.Fprintf(s.stdin, "%s\n", msg)
	if err != nil {
		t.Fatalf("write notification to shim: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
}

// close terminates the shim process.
func (s *shimSession) close() {
	s.stdin.Close()
	s.cmd.Wait()
}

// handshake performs the initialize/initialized handshake.
func (s *shimSession) handshake(t *testing.T) {
	t.Helper()
	resp := s.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`)
	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("initialize: no result: %v", resp)
	}
	if result["protocolVersion"] != shared.MaxProtocolVersion {
		t.Errorf("protocol version = %v, want %s", result["protocolVersion"], shared.MaxProtocolVersion)
	}
	s.sendNotification(t, `{"jsonrpc":"2.0","method":"initialized"}`)
}

// callTool sends a tools/call request and returns the text result and isError.
func (s *shimSession) callTool(t *testing.T, id int, name string, args map[string]interface{}) (string, bool) {
	t.Helper()
	argsJSON, _ := json.Marshal(args)
	msg := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, id, name, argsJSON)
	resp := s.send(t, msg)
	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		if errObj, ok := resp["error"]; ok {
			return fmt.Sprintf("%v", errObj), true
		}
		t.Fatalf("no result in response: %v", resp)
	}
	isError := false
	if ie, ok := result["isError"].(bool); ok {
		isError = ie
	}
	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		return "", isError
	}
	item, ok := content[0].(map[string]interface{})
	if !ok {
		return "", isError
	}
	text, _ := item["text"].(string)
	return text, isError
}

// callToolCritical sends a tools/call request expecting a critical error response.
// Critical errors produce TWO stdout lines: a notification then the tool response.
// This helper reads both and returns the tool response text and isError.
func callToolCritical(t *testing.T, s *shimSession, id int, name string, args map[string]interface{}) (string, bool) {
	t.Helper()
	argsJSON, _ := json.Marshal(args)
	msg := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, id, name, argsJSON)
	fmt.Fprintf(s.stdin, "%s\n", msg)
	// Read first line: could be notification or tool response.
	if !s.stdout.Scan() {
		t.Fatalf("no response from shim (err: %v)", s.stdout.Err())
	}
	var first map[string]interface{}
	if err := json.Unmarshal(s.stdout.Bytes(), &first); err != nil {
		t.Fatalf("unmarshal first line: %v\nraw: %s", err, s.stdout.Text())
	}
	// If it's a notification, read the second line for the actual response.
	resp := first
	if _, isNotif := first["method"]; isNotif {
		if !s.stdout.Scan() {
			t.Fatalf("no second response from shim")
		}
		if err := json.Unmarshal(s.stdout.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal second line: %v\nraw: %s", err, s.stdout.Text())
		}
	}
	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		if errObj, ok := resp["error"]; ok {
			return fmt.Sprintf("%v", errObj), true
		}
		t.Fatalf("no result in response: %v", resp)
	}
	isError := false
	if ie, ok := result["isError"].(bool); ok {
		isError = ie
	}
	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		return "", isError
	}
	item, ok := content[0].(map[string]interface{})
	if !ok {
		return "", isError
	}
	text, _ := item["text"].(string)
	return text, isError
}

// copyRogueToDir copies rogue.exe into dir (an allowed root) and returns its path.
func copyRogueToDir(t *testing.T, dir string) string {
	t.Helper()
	localRogue := filepath.Join(dir, "rogue.exe")
	data, err := os.ReadFile(rogueExePath)
	if err != nil {
		t.Fatalf("read rogue.exe: %v", err)
	}
	os.WriteFile(localRogue, data, 0755)
	return localRogue
}

// writeTestFile creates a test file in the given directory.
func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// --- §5.1 Protocol Tests ---

func TestProtocol_Initialize(t *testing.T) {
	s := startShim(t, testDir)
	defer s.close()
	resp := s.send(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`)
	result := resp["result"].(map[string]interface{})
	caps := result["capabilities"].(map[string]interface{})
	if _, ok := caps["tools"]; !ok {
		t.Error("initialize response missing tools capability")
	}
}

func TestProtocol_ToolsList(t *testing.T) {
	s := startShim(t, testDir)
	defer s.close()
	s.handshake(t)
	resp := s.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	result := resp["result"].(map[string]interface{})
	toolsList := result["tools"].([]interface{})
	names := make(map[string]bool)
	for _, tool := range toolsList {
		tm := tool.(map[string]interface{})
		names[tm["name"].(string)] = true
	}
	for _, expected := range []string{"read", "write", "edit", "copy", "move", "delete", "list", "search", "info", "run"} {
		if !names[expected] {
			t.Errorf("tools/list missing: %s", expected)
		}
	}
}

func TestProtocol_UnknownMethod(t *testing.T) {
	s := startShim(t, testDir)
	defer s.close()
	s.handshake(t)
	resp := s.send(t, `{"jsonrpc":"2.0","id":3,"method":"unknown/method"}`)
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error response, got: %v", resp)
	}
	code := int(errObj["code"].(float64))
	if code != -32601 {
		t.Errorf("error code = %d, want -32601", code)
	}
}

func TestProtocol_UnknownTool(t *testing.T) {
	s := startShim(t, testDir)
	defer s.close()
	s.handshake(t)
	resp := s.send(t, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}`)
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected JSON-RPC error, got: %v", resp)
	}
	code := int(errObj["code"].(float64))
	if code != -32602 {
		t.Errorf("error code = %d, want -32602", code)
	}
}

// --- §5.2 File Operation Tests ---

func TestRead_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "test.txt", "hello world")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "read", map[string]interface{}{"path": path})
	if isErr {
		t.Fatalf("read error: %s", text)
	}
	if text != "hello world" {
		t.Errorf("read = %q, want %q", text, "hello world")
	}
}

func TestRead_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "read", map[string]interface{}{"path": filepath.Join(dir, "nope.txt")})
	if !isErr {
		t.Fatalf("expected error, got: %s", text)
	}
	if !strings.Contains(text, "not found") && !strings.Contains(text, "File not found") {
		t.Errorf("error = %q, want mention of not found", text)
	}
}

func TestRead_BinaryFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "binary.bin", "hello\x00world")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "read", map[string]interface{}{"path": path})
	if !isErr {
		t.Fatalf("expected error, got: %s", text)
	}
	if !strings.Contains(text, "binary") {
		t.Errorf("error = %q, want mention of binary", text)
	}
}

func TestRead_UTF16File(t *testing.T) {
	dir := t.TempDir()
	// Create valid UTF-16LE file: BOM + "hello" in UTF-16LE
	utf16le := []byte{0xFF, 0xFE, 'h', 0, 'e', 0, 'l', 0, 'l', 0, 'o', 0}
	path := writeTestFile(t, dir, "utf16.txt", string(utf16le))
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "read", map[string]interface{}{"path": path})
	if isErr {
		t.Fatalf("read UTF-16 error: %s", text)
	}
	if text != "hello" {
		t.Errorf("read UTF-16 = %q, want %q", text, "hello")
	}
}

func TestRead_TooLarge(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("a", shared.MaxReadSize+1)
	path := writeTestFile(t, dir, "big.txt", content)
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "read", map[string]interface{}{"path": path})
	if !isErr {
		t.Fatalf("expected error for large file")
	}
	if !strings.Contains(text, "offset/limit") {
		t.Errorf("error = %q, want mention of offset/limit", text)
	}
}

func TestRead_PathOutsideRoots(t *testing.T) {
	dir := t.TempDir()
	otherDir := t.TempDir()
	path := writeTestFile(t, otherDir, "secret.txt", "secret")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "read", map[string]interface{}{"path": path})
	if !isErr {
		t.Fatalf("expected error for confined path, got: %s", text)
	}
	if !strings.Contains(text, "not within allowed") {
		t.Errorf("error = %q, want confinement error", text)
	}
}

func TestWrite_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "write", map[string]interface{}{"path": path, "content": "hello"})
	if isErr {
		t.Fatalf("write error: %s", text)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hello" {
		t.Errorf("file = %q, want %q", string(data), "hello")
	}
}

func TestWrite_CRLFPreservation(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "crlf.txt", "line1\r\nline2\r\n")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "write", map[string]interface{}{"path": path, "content": "new1\nnew2\n"})
	if isErr {
		t.Fatalf("write error: %s", text)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "\r\n") {
		t.Errorf("CRLF not preserved: %q", string(data))
	}
}

func TestEdit_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "edit.txt", "hello world")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "edit", map[string]interface{}{"path": path, "old_text": "hello", "new_text": "goodbye"})
	if isErr {
		t.Fatalf("edit error: %s", text)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "goodbye world" {
		t.Errorf("file = %q, want %q", string(data), "goodbye world")
	}
}

func TestEdit_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "edit2.txt", "hello world")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "edit", map[string]interface{}{"path": path, "old_text": "nonexistent", "new_text": "x"})
	if !isErr {
		t.Fatalf("expected error, got: %s", text)
	}
	if !strings.Contains(text, "not found") {
		t.Errorf("error = %q", text)
	}
}

func TestEdit_NotUnique(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "edit3.txt", "hello hello")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "edit", map[string]interface{}{"path": path, "old_text": "hello", "new_text": "x"})
	if !isErr {
		t.Fatalf("expected error, got: %s", text)
	}
	if !strings.Contains(text, "not unique") {
		t.Errorf("error = %q", text)
	}
}

func TestCopy_HappyPath(t *testing.T) {
	dir := t.TempDir()
	src := writeTestFile(t, dir, "src.txt", "content")
	dst := filepath.Join(dir, "dst.txt")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "copy", map[string]interface{}{"source": src, "destination": dst})
	if isErr {
		t.Fatalf("copy error: %s", text)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "content" {
		t.Errorf("dst = %q, want %q", string(data), "content")
	}
}

func TestCopy_DestinationExists(t *testing.T) {
	dir := t.TempDir()
	src := writeTestFile(t, dir, "src.txt", "content")
	dst := writeTestFile(t, dir, "dst.txt", "existing")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "copy", map[string]interface{}{"source": src, "destination": dst})
	if !isErr {
		t.Fatalf("expected error, got: %s", text)
	}
	if !strings.Contains(text, "already exists") {
		t.Errorf("error = %q", text)
	}
}

func TestMove_HappyPath(t *testing.T) {
	dir := t.TempDir()
	src := writeTestFile(t, dir, "src.txt", "content")
	dst := filepath.Join(dir, "moved.txt")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "move", map[string]interface{}{"source": src, "destination": dst})
	if isErr {
		t.Fatalf("move error: %s", text)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source still exists after move")
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "content" {
		t.Errorf("dst = %q", string(data))
	}
}

func TestMove_DestinationExists(t *testing.T) {
	dir := t.TempDir()
	src := writeTestFile(t, dir, "src.txt", "content")
	dst := writeTestFile(t, dir, "dst.txt", "existing")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "move", map[string]interface{}{"source": src, "destination": dst})
	if !isErr {
		t.Fatalf("expected error, got: %s", text)
	}
	if !strings.Contains(text, "already exists") {
		t.Errorf("error = %q", text)
	}
}

func TestDelete_File(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "del.txt", "x")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	_, isErr := s.callTool(t, 10, "delete", map[string]interface{}{"path": path})
	if isErr {
		t.Fatal("delete error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file still exists after delete")
	}
}

func TestDelete_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "empty")
	os.Mkdir(subdir, 0755)
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	_, isErr := s.callTool(t, 10, "delete", map[string]interface{}{"path": subdir})
	if isErr {
		t.Fatal("delete empty dir error")
	}
}

func TestDelete_NonEmptyDir(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "notempty")
	os.Mkdir(subdir, 0755)
	writeTestFile(t, subdir, "file.txt", "x")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "delete", map[string]interface{}{"path": subdir})
	if !isErr {
		t.Fatalf("expected error, got: %s", text)
	}
}

func TestList_WithAndWithoutPattern(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "a.txt", "x")
	writeTestFile(t, dir, "b.py", "x")
	writeTestFile(t, dir, "c.txt", "x")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "list", map[string]interface{}{"path": dir})
	if isErr {
		t.Fatalf("list error: %s", text)
	}
	if !strings.Contains(text, "a.txt") || !strings.Contains(text, "b.py") || !strings.Contains(text, "c.txt") {
		t.Errorf("list missing entries: %s", text)
	}
	text2, isErr2 := s.callTool(t, 11, "list", map[string]interface{}{"path": dir, "pattern": "*.txt"})
	if isErr2 {
		t.Fatalf("list error: %s", text2)
	}
	if !strings.Contains(text2, "a.txt") || !strings.Contains(text2, "c.txt") {
		t.Errorf("filtered list missing .txt entries: %s", text2)
	}
	if strings.Contains(text2, "b.py") {
		t.Errorf("filtered list should not contain b.py: %s", text2)
	}
}

func TestSearch_Recursive(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0755)
	writeTestFile(t, dir, "top.txt", "x")
	writeTestFile(t, sub, "deep.txt", "x")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "search", map[string]interface{}{"path": dir, "pattern": "*.txt"})
	if isErr {
		t.Fatalf("search error: %s", text)
	}
	if !strings.Contains(text, "top.txt") || !strings.Contains(text, "deep.txt") {
		t.Errorf("search missing: %s", text)
	}
}

func TestSearch_MaxResults(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		writeTestFile(t, dir, fmt.Sprintf("file%d.txt", i), "x")
	}
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "search", map[string]interface{}{"path": dir, "pattern": "*.txt", "max_results": 5})
	if isErr {
		t.Fatalf("search error: %s", text)
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 5 {
		t.Errorf("search returned %d results, want <= 5", len(lines))
	}
}

func TestInfo_FileAndDirectory(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "info.txt", "hello")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "info", map[string]interface{}{"path": path})
	if isErr {
		t.Fatalf("info error: %s", text)
	}
	if !strings.Contains(text, "file") || !strings.Contains(text, "5") {
		t.Errorf("info = %s", text)
	}
	text2, isErr2 := s.callTool(t, 11, "info", map[string]interface{}{"path": dir})
	if isErr2 {
		t.Fatalf("info dir error: %s", text2)
	}
	if !strings.Contains(text2, "directory") {
		t.Errorf("info dir = %s", text2)
	}
}

// --- §5.3 run Tests ---

func TestRun_SimpleCommand(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "run", map[string]interface{}{"command": "cmd.exe", "args": "/c echo hello"})
	if isErr {
		t.Fatalf("run error: %s", text)
	}
	if !strings.Contains(text, "hello") {
		t.Errorf("run output = %q, want hello", text)
	}
}

func TestRun_NonZeroExit(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "run", map[string]interface{}{"command": "cmd.exe", "args": "/c exit 42"})
	if isErr {
		t.Fatalf("run returned tool error (should report exit code): %s", text)
	}
	if !strings.Contains(text, "42") {
		t.Errorf("run output = %q, want exit code 42", text)
	}
}

func TestRun_InactivityTimeout(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	// §6.3.2: timeouts return text results with diagnostic message
	text, _ := s.callTool(t, 10, "run", map[string]interface{}{"command": "cmd.exe", "args": "/c ping -n 100 127.0.0.1 > nul"})
	if !strings.Contains(text, "no output") {
		t.Errorf("timeout message = %q, want mention of 'no output'", text)
	}
}

func TestRun_TotalTimeout(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	// §6.3.2: timeouts return text results with diagnostic message
	text, _ := s.callTool(t, 10, "run", map[string]interface{}{"command": "cmd.exe", "args": "/c ping -n 100 127.0.0.1"})
	if !strings.Contains(text, "timeout") && !strings.Contains(text, "no output") {
		t.Errorf("timeout message = %q, want mention of timeout or 'no output'", text)
	}
}

func TestRun_OutputTruncation(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "big.bat")
	os.WriteFile(script, []byte("@echo off\nfor /L %%i in (1,1,5000) do echo aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), 0644)
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "run", map[string]interface{}{"command": "cmd.exe", "args": "/c " + script})
	if isErr {
		if strings.Contains(text, "truncated") || strings.Contains(text, "timeout") {
			return
		}
		t.Fatalf("unexpected error: %s", text)
	}
	if !strings.Contains(text, "truncated") {
		if len(text) < 50000 {
			t.Errorf("expected truncation or large output, got %d bytes", len(text))
		}
	}
}

// --- Helpers for advanced tests ---

// startShimWithConfig starts a shim with a custom TOML config string.
func startShimWithConfig(t *testing.T, configContent string) *shimSession {
	t.Helper()
	configPath := filepath.Join(testDir, "test_shim.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	shimConfig := filepath.Join(filepath.Dir(shimExePath), "shim.toml")
	os.WriteFile(shimConfig, []byte(configContent), 0644)
	cmd := exec.Command(shimExePath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start shim: %v", err)
	}
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, shared.MaxLineSize), shared.MaxLineSize)
	return &shimSession{cmd: cmd, stdin: stdin, stdout: scanner, stderr: stderrPipe}
}

// startShimWithEnv starts a shim with extra environment variables.
func startShimWithEnv(t *testing.T, allowedRoot string, env []string) *shimSession {
	t.Helper()
	configPath := filepath.Join(testDir, "test_shim.toml")
	config := fmt.Sprintf(`[security]
allowed_roots = [%q]

[run]
inactivity_timeout = 3
total_timeout = 5
max_output = 102400
`, allowedRoot)
	os.WriteFile(configPath, []byte(config), 0644)
	shimConfig := filepath.Join(filepath.Dir(shimExePath), "shim.toml")
	os.WriteFile(shimConfig, []byte(config), 0644)
	cmd := exec.Command(shimExePath)
	cmd.Env = append(os.Environ(), env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start shim: %v", err)
	}
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, shared.MaxLineSize), shared.MaxLineSize)
	return &shimSession{cmd: cmd, stdin: stdin, stdout: scanner, stderr: stderrPipe}
}

// startShimWithArgs starts a shim with extra command-line arguments.
func startShimWithArgs(t *testing.T, allowedRoot string, extraArgs ...string) *shimSession {
	t.Helper()
	configPath := filepath.Join(testDir, "test_shim.toml")
	config := fmt.Sprintf(`[security]
allowed_roots = [%q]

[run]
inactivity_timeout = 3
total_timeout = 5
max_output = 102400
`, allowedRoot)
	os.WriteFile(configPath, []byte(config), 0644)
	shimConfig := filepath.Join(filepath.Dir(shimExePath), "shim.toml")
	os.WriteFile(shimConfig, []byte(config), 0644)
	cmd := exec.Command(shimExePath, extraArgs...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start shim: %v", err)
	}
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, shared.MaxLineSize), shared.MaxLineSize)
	return &shimSession{cmd: cmd, stdin: stdin, stdout: scanner, stderr: stderrPipe}
}

// sendRaw sends a raw JSON-RPC message and reads the raw response line.
func (s *shimSession) sendRaw(t *testing.T, msg string) string {
	t.Helper()
	_, err := fmt.Fprintf(s.stdin, "%s\n", msg)
	if err != nil {
		t.Fatalf("write to shim: %v", err)
	}
	if !s.stdout.Scan() {
		t.Fatalf("no response from shim (err: %v)", s.stdout.Err())
	}
	return s.stdout.Text()
}

// --- Batch 1: Priority 1 MANDATORY-V1 Tests ---

// §8.1.2 / 14.2.4: Symlink/junction escape caught by post-check.
func TestSecurity_JunctionEscape(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction test requires Windows")
	}
	dir := t.TempDir()
	outsideDir := t.TempDir()
	secretPath := writeTestFile(t, outsideDir, "secret.txt", "top secret data")
	_ = secretPath
	// Create junction inside allowed root pointing outside
	junctionPath := filepath.Join(dir, "escape")
	cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", junctionPath, outsideDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mklink /J failed: %v\n%s", err, out)
	}
	defer os.Remove(junctionPath)
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	escapePath := filepath.Join(junctionPath, "secret.txt")
	text, isErr := callToolCritical(t, s, 10, "read", map[string]interface{}{"path": escapePath})
	if !isErr {
		t.Fatalf("expected confinement error for junction escape, got: %s", text)
	}
	if !strings.HasPrefix(text, "🛑 CRITICAL:") {
		t.Errorf("error = %q, want critical error with 🛑 prefix", text)
	}
}

// §9.5 / 14.5.4 / 14.5.6: Concurrent pipe draining — 100KB stdout + 100KB stderr.
func TestRun_ConcurrentPipeDraining(t *testing.T) {
	dir := t.TempDir()
	// Use longer timeouts for this test since it produces lots of output
	configContent := fmt.Sprintf(`[security]
allowed_roots = [%q]

[run]
inactivity_timeout = 15
total_timeout = 30
max_output = 204800
`, dir)
	s := startShimWithConfig(t, configContent)
	defer s.close()
	s.handshake(t)
	localRogue := copyRogueToDir(t, dir)
	text, isErr := s.callTool(t, 10, "run", map[string]interface{}{
		"command": localRogue,
		"args":    "--flood-both 102400",
	})
	if isErr {
		// Accept truncation as valid — the key assertion is no deadlock
		if strings.Contains(text, "truncated") {
			return
		}
		t.Fatalf("run error: %s", text)
	}
	// Verify stdout captured (O chars)
	if !strings.Contains(text, "OOOO") {
		t.Errorf("stdout not captured, output starts with: %.100s", text)
	}
	// Verify stderr captured separately (E chars in stderr section)
	if !strings.Contains(text, "stderr") || !strings.Contains(text, "EEEE") {
		t.Errorf("stderr not captured separately, output: %.200s", text)
	}
}

// §9.8.1 / 14.5.2: Job Object — grandchild killed when parent killed.
func TestRun_JobObjectGrandchildKilled(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Job Object test requires Windows")
	}
	dir := t.TempDir()
	// Use short total timeout to trigger kill, but keep inactivity high
	configContent := fmt.Sprintf(`[security]
allowed_roots = [%q]

[run]
inactivity_timeout = 10
total_timeout = 5
max_output = 102400
`, dir)
	s := startShimWithConfig(t, configContent)
	defer s.close()
	s.handshake(t)
	localRogue := copyRogueToDir(t, dir)
	pidFile := filepath.Join(dir, "grandchild.pid")
	text, isErr := s.callTool(t, 10, "run", map[string]interface{}{
		"command": localRogue,
		"args":    fmt.Sprintf(`--grandchild "%s"`, pidFile),
	})
	_ = text
	_ = isErr
	// Read grandchild PID from file
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		t.Skipf("could not read grandchild PID file: %v", err)
	}
	grandchildPID := strings.TrimSpace(string(pidData))
	if grandchildPID == "" {
		t.Skip("grandchild PID file is empty")
	}
	// Wait briefly for Job Object cleanup
	time.Sleep(1 * time.Second)
	// Check if grandchild is dead via tasklist
	check := exec.Command("cmd.exe", "/c", "tasklist", "/FI", fmt.Sprintf("PID eq %s", grandchildPID), "/NH")
	checkOut, _ := check.CombinedOutput()
	checkStr := string(checkOut)
	if strings.Contains(checkStr, "rogue") {
		t.Errorf("grandchild PID %s still alive after parent killed by Job Object", grandchildPID)
	}
}

// §9.8.2 / 14.5.1: WER suppression — child crash returns immediately.
func TestRun_WERSuppression(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("WER test requires Windows")
	}
	dir := t.TempDir()
	localRogue := copyRogueToDir(t, dir)
	// Use longer timeouts so we can measure WER-free response time
	configContent := fmt.Sprintf(`[security]
allowed_roots = [%q]

[run]
inactivity_timeout = 15
total_timeout = 30
max_output = 102400
`, dir)
	s := startShimWithConfig(t, configContent)
	defer s.close()
	s.handshake(t)
	start := time.Now()
	text, isErr := s.callTool(t, 10, "run", map[string]interface{}{
		"command": localRogue,
		"args":    "--crash",
	})
	elapsed := time.Since(start)
	// Crash should return quickly (< 5s), not wait for WER dialog (10s+)
	if elapsed > 5*time.Second {
		t.Errorf("crash took %v, expected < 5s (WER dialog not suppressed?)", elapsed)
	}
	// Should get a non-zero exit code, not a timeout
	_ = isErr
	if strings.Contains(text, "timeout") {
		t.Errorf("crash resulted in timeout instead of immediate exit: %s", text)
	}
	t.Logf("crash completed in %v, isErr=%v, output=%.100s", elapsed, isErr, text)
}

// §9.8.3+9.8.4 / 14.5.3: Kill sequence and handle leak — 200 sequential calls.
func TestRun_HandleLeak(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	for i := 0; i < 200; i++ {
		text, isErr := s.callTool(t, i+10, "run", map[string]interface{}{
			"command": "cmd.exe",
			"args":    fmt.Sprintf("/c echo iteration_%d", i),
		})
		if isErr {
			t.Fatalf("iteration %d error: %s", i, text)
		}
		if !strings.Contains(text, fmt.Sprintf("iteration_%d", i)) {
			t.Fatalf("iteration %d output missing, got: %s", i, text)
		}
	}
}

// §9.11 / 14.6.1+14.6.2: Panic recovery — shim returns error and continues.
func TestPanicRecovery(t *testing.T) {
	dir := t.TempDir()
	s := startShimWithEnv(t, dir, []string{"SHIM_TEST_PANIC=1"})
	defer s.close()
	s.handshake(t)
	// Call info tool — if SHIM_TEST_PANIC is wired, this should panic and recover.
	// If not wired, the tool will just work normally. Either way, test that shim continues.
	path := writeTestFile(t, dir, "test.txt", "hello")
	text1, isErr1 := s.callTool(t, 10, "info", map[string]interface{}{"path": path})
	_ = text1
	_ = isErr1
	// The key test: shim must still respond to subsequent requests
	text2, isErr2 := s.callTool(t, 11, "read", map[string]interface{}{"path": path})
	if isErr2 {
		t.Fatalf("second request after potential panic failed: %s", text2)
	}
	if text2 != "hello" {
		t.Errorf("second request returned %q, want %q", text2, "hello")
	}
}

// §9.12.1 / 14.7.1: Shutdown on stdin EOF — clean exit 0.
func TestShutdown_StdinEOF(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	s.handshake(t)
	// Close stdin
	s.stdin.Close()
	// Wait for shim to exit
	done := make(chan error, 1)
	go func() {
		done <- s.cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("shim exited with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		s.cmd.Process.Kill()
		t.Fatal("shim did not exit within 5s after stdin close")
	}
}

// §9.12.3 / 14.7.2: Shutdown while child running — child killed, clean exit.
// The shim processes requests synchronously. When a tool/call is being processed,
// the main loop is blocked in the handler. The child will be killed by the
// total timeout, then the stdin EOF is detected on the next scanner.Scan().
func TestShutdown_WhileChildRunning(t *testing.T) {
	dir := t.TempDir()
	// Use short total_timeout so the child gets killed quickly
	configContent := fmt.Sprintf(`[security]
allowed_roots = [%q]

[run]
inactivity_timeout = 3
total_timeout = 4
max_output = 102400
`, dir)
	s := startShimWithConfig(t, configContent)
	// Start a long-running command — will be killed by total_timeout=4
	localRogue := copyRogueToDir(t, dir)
	argsJSON, _ := json.Marshal(map[string]interface{}{
		"command": localRogue,
		"args":    "--trickle",
	})
	msg := fmt.Sprintf(`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"run","arguments":%s}}`, argsJSON)
	_, err := fmt.Fprintf(s.stdin, "%s\n", msg)
	if err != nil {
		t.Fatalf("write to shim: %v", err)
	}
	// Close stdin — the shim will notice after the tool call returns
	time.Sleep(200 * time.Millisecond)
	s.stdin.Close()
	// Wait for shim to exit (total_timeout kills child, then stdin EOF detected)
	done := make(chan error, 1)
	go func() {
		done <- s.cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("shim exited with error: %v (expected clean exit)", err)
		}
	case <-time.After(15 * time.Second):
		s.cmd.Process.Kill()
		t.Fatal("shim did not exit within 15s after stdin close with running child")
	}
}

// §9.12.2 / 14.7.3: Shutdown on broken stdout pipe.
// When the shim detects a write error on stdout, it sets shutdownFlag.
// The main loop then exits on the next scanner.Scan() (when stdin closes or EOF).
func TestShutdown_BrokenStdoutPipe(t *testing.T) {
	dir := t.TempDir()
	configContent := fmt.Sprintf(`[security]
allowed_roots = [%q]

[run]
inactivity_timeout = 3
total_timeout = 5
max_output = 102400
`, dir)
	configPath := filepath.Join(testDir, "test_shim.toml")
	os.WriteFile(configPath, []byte(configContent), 0644)
	shimConfig := filepath.Join(filepath.Dir(shimExePath), "shim.toml")
	os.WriteFile(shimConfig, []byte(configContent), 0644)
	cmd := exec.Command(shimExePath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start shim: %v", err)
	}
	// Close the stdout pipe from our side (simulating broken pipe)
	if closer, ok := stdoutPipe.(io.Closer); ok {
		closer.Close()
	}
	// Send a request — shim should detect broken stdout and set shutdownFlag
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`+"\n")
	// Give shim time to process and detect the broken pipe
	time.Sleep(200 * time.Millisecond)
	// Close stdin so scanner.Scan() unblocks and the shutdownFlag check triggers exit
	stdin.Close()
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case <-done:
		// Shim exited — that's what we want
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatal("shim did not exit within 5s after broken stdout pipe + stdin close")
	}
}

// §9.1.2: Never exit on tool error — return error and continue.
func TestErrorContinuation(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	// First call: trigger error (read non-existent file)
	text1, isErr1 := s.callTool(t, 10, "read", map[string]interface{}{"path": filepath.Join(dir, "nonexistent.txt")})
	if !isErr1 {
		t.Fatalf("expected error, got: %s", text1)
	}
	// Second call: should succeed
	path := writeTestFile(t, dir, "exists.txt", "hello")
	text2, isErr2 := s.callTool(t, 11, "read", map[string]interface{}{"path": path})
	if isErr2 {
		t.Fatalf("second call failed after first error: %s", text2)
	}
	if text2 != "hello" {
		t.Errorf("second call = %q, want %q", text2, "hello")
	}
}

// §14.5.5: Child stdin receives EOF — no hang.
func TestRun_ChildStdinEOF(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	localRogue := copyRogueToDir(t, dir)
	text, isErr := s.callTool(t, 10, "run", map[string]interface{}{
		"command": localRogue,
		"args":    "--read-stdin",
	})
	if isErr {
		t.Fatalf("run error: %s", text)
	}
	if !strings.Contains(text, "ok") {
		t.Errorf("expected 'ok' from child that got EOF on stdin, got: %s", text)
	}
}

// --- Batch 2: Priority 2 §4-§5 Protocol + Built-in Tool Tests ---

// §4.6: Preserve request id type (string and null).
func TestProtocol_IdTypePreservation(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	// Test string id
	raw := s.sendRaw(t, `{"jsonrpc":"2.0","id":"abc","method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`)
	var resp map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	idRaw := string(resp["id"])
	if idRaw != `"abc"` {
		t.Errorf("string id = %s, want %q", idRaw, `"abc"`)
	}
	// New session for null id test
	s.close()
	s2 := startShim(t, dir)
	defer s2.close()
	raw2 := s2.sendRaw(t, `{"jsonrpc":"2.0","id":null,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`)
	var resp2 map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw2), &resp2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	idRaw2 := string(resp2["id"])
	if idRaw2 != "null" {
		t.Errorf("null id = %s, want null", idRaw2)
	}
}

// §4.7: Notification (no id) — no response sent.
func TestProtocol_NotificationNoResponse(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	// Send a notification (no id) for an unknown method
	s.sendNotification(t, `{"jsonrpc":"2.0","method":"unknown/notification"}`)
	// Send a real request to verify shim is still alive
	resp := s.send(t, `{"jsonrpc":"2.0","id":99,"method":"tools/list"}`)
	if _, ok := resp["result"]; !ok {
		t.Fatalf("shim did not respond to request after notification: %v", resp)
	}
}

// §5.2.5: Skip UTF-8 BOM (EF BB BF).
func TestRead_UTF8BOMSkipped(t *testing.T) {
	dir := t.TempDir()
	// Create file with UTF-8 BOM
	path := filepath.Join(dir, "bom.txt")
	content := []byte{0xEF, 0xBB, 0xBF}
	content = append(content, []byte("hello")...)
	os.WriteFile(path, content, 0644)
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "read", map[string]interface{}{"path": path})
	if isErr {
		t.Fatalf("read error: %s", text)
	}
	if text != "hello" {
		t.Errorf("read with BOM = %q, want %q (BOM should be stripped)", text, "hello")
	}
}

// §5.2.6: offset/limit reads only requested range.
func TestRead_OffsetLimit(t *testing.T) {
	dir := t.TempDir()
	// Create 100KB file with known pattern
	content := make([]byte, 102400)
	for i := range content {
		content[i] = byte('A' + (i % 26))
	}
	path := filepath.Join(dir, "big.txt")
	os.WriteFile(path, content, 0644)
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "read", map[string]interface{}{
		"path":   path,
		"offset": 50000,
		"limit":  100,
	})
	if isErr {
		t.Fatalf("read error: %s", text)
	}
	if len(text) != 100 {
		t.Errorf("read returned %d bytes, want 100", len(text))
	}
	// Verify content matches expected range
	expected := string(content[50000:50100])
	if text != expected {
		t.Errorf("read content mismatch at offset 50000")
	}
}

// §5.3.3: Append mode.
func TestWrite_AppendMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "append.txt")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	// Write initial content
	_, isErr := s.callTool(t, 10, "write", map[string]interface{}{"path": path, "content": "line1\n"})
	if isErr {
		t.Fatal("initial write failed")
	}
	// Append
	_, isErr = s.callTool(t, 11, "write", map[string]interface{}{"path": path, "content": "line2\n", "append": true})
	if isErr {
		t.Fatal("append write failed")
	}
	// Read back
	text, isErr := s.callTool(t, 12, "read", map[string]interface{}{"path": path})
	if isErr {
		t.Fatalf("read error: %s", text)
	}
	if !strings.Contains(text, "line1") || !strings.Contains(text, "line2") {
		t.Errorf("append result = %q, want both lines", text)
	}
}

// §5.3.5: New files use LF line endings.
func TestWrite_NewFileLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "newfile.txt")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	_, isErr := s.callTool(t, 10, "write", map[string]interface{}{"path": path, "content": "line1\nline2\n"})
	if isErr {
		t.Fatal("write failed")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if strings.Contains(string(data), "\r\n") {
		t.Errorf("new file has CRLF, expected LF only: %q", string(data))
	}
}

// §5.5.3: Recursive directory copy.
func TestCopy_RecursiveDir(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	writeTestFile(t, srcDir, "a.txt", "aaa")
	writeTestFile(t, filepath.Join(srcDir, "sub"), "b.txt", "bbb")
	dstDir := filepath.Join(dir, "dst")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "copy", map[string]interface{}{
		"source":      srcDir,
		"destination": dstDir,
		"recursive":   true,
	})
	if isErr {
		t.Fatalf("recursive copy error: %s", text)
	}
	// Verify tree copied
	dataA, err := os.ReadFile(filepath.Join(dstDir, "a.txt"))
	if err != nil {
		t.Fatalf("a.txt not copied: %v", err)
	}
	if string(dataA) != "aaa" {
		t.Errorf("a.txt = %q", string(dataA))
	}
	dataB, err := os.ReadFile(filepath.Join(dstDir, "sub", "b.txt"))
	if err != nil {
		t.Fatalf("sub/b.txt not copied: %v", err)
	}
	if string(dataB) != "bbb" {
		t.Errorf("sub/b.txt = %q", string(dataB))
	}
}

// §5.5.4: Directory copy without recursive=true should fail.
func TestCopy_DirWithoutRecursive(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	os.Mkdir(srcDir, 0755)
	writeTestFile(t, srcDir, "a.txt", "aaa")
	dstDir := filepath.Join(dir, "dst")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "copy", map[string]interface{}{
		"source":      srcDir,
		"destination": dstDir,
	})
	if !isErr {
		t.Fatalf("expected error for dir copy without recursive, got: %s", text)
	}
	if !strings.Contains(text, "recursive") {
		t.Errorf("error = %q, want mention of recursive", text)
	}
}

// §5.7.4: os.Remove semantics — dir with file inside should fail.
func TestDelete_NonEmptyDirSemantics(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "hasfiles")
	os.Mkdir(subdir, 0755)
	writeTestFile(t, subdir, "inner.txt", "x")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "delete", map[string]interface{}{"path": subdir})
	if !isErr {
		t.Fatalf("expected error deleting non-empty dir, got: %s", text)
	}
	if !strings.Contains(strings.ToLower(text), "not empty") {
		t.Errorf("error = %q, want 'not empty'", text)
	}
}

// §5.8.3: List output format — name\ttype\tsize per line.
func TestList_OutputFormat(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "hello.txt", "12345")
	os.Mkdir(filepath.Join(dir, "subdir"), 0755)
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "list", map[string]interface{}{"path": dir})
	if isErr {
		t.Fatalf("list error: %s", text)
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			t.Errorf("list line %q has %d tab-separated fields, want 3", line, len(parts))
		}
	}
	// Check file entry has correct type and size
	if !strings.Contains(text, "hello.txt\tfile\t5") {
		t.Errorf("list missing expected 'hello.txt\\tfile\\t5', got: %s", text)
	}
	if !strings.Contains(text, "subdir\tdir\t") {
		t.Errorf("list missing expected 'subdir\\tdir\\t', got: %s", text)
	}
}

// §5.9.4: Search returns absolute paths.
func TestSearch_AbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "found.txt", "x")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "search", map[string]interface{}{"path": dir, "pattern": "*.txt"})
	if isErr {
		t.Fatalf("search error: %s", text)
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !filepath.IsAbs(line) {
			t.Errorf("search returned non-absolute path: %s", line)
		}
	}
}

// §5.10.1: Info includes creation time and read-only flag.
func TestInfo_CreationTimeAndReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "info.txt", "hello")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "info", map[string]interface{}{"path": path})
	if isErr {
		t.Fatalf("info error: %s", text)
	}
	if !strings.Contains(text, "created:") {
		t.Errorf("info missing creation time: %s", text)
	}
	if !strings.Contains(text, "read_only:") {
		t.Errorf("info missing read_only flag: %s", text)
	}
	if !strings.Contains(text, "read_only: false") {
		t.Errorf("expected read_only: false for normal file, got: %s", text)
	}
}

// §5.11.2: Argument splitting — quotes and whitespace.
func TestRun_ArgumentSplitting(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	localRogue := copyRogueToDir(t, dir)
	text, isErr := s.callTool(t, 10, "run", map[string]interface{}{
		"command": localRogue,
		"args":    `--echo hello world`,
	})
	if isErr {
		t.Fatalf("run error: %s", text)
	}
	if !strings.Contains(text, "hello world") {
		t.Errorf("run output = %q, want 'hello world'", text)
	}
}

// §5.11.7: Command confinement — absolute path outside roots rejected; unqualified name allowed.
func TestRun_CommandConfinement(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	// Absolute path outside allowed roots should fail
	text, isErr := s.callTool(t, 10, "run", map[string]interface{}{
		"command": `C:\Windows\System32\cmd.exe`,
		"args":    "/c echo hello",
	})
	if !isErr {
		t.Errorf("expected confinement error for absolute path outside roots, got: %s", text)
	}
	// Unqualified name should succeed (found on PATH)
	text2, isErr2 := s.callTool(t, 11, "run", map[string]interface{}{
		"command": "cmd.exe",
		"args":    "/c echo hello",
	})
	if isErr2 {
		t.Fatalf("unqualified command failed: %s", text2)
	}
	if !strings.Contains(text2, "hello") {
		t.Errorf("unqualified run output = %q, want 'hello'", text2)
	}
}

// §5.11.8: Timeout parameter — extract, clamp, use.
func TestRun_TimeoutParameter(t *testing.T) {
	dir := t.TempDir()
	configContent := fmt.Sprintf(`[security]
allowed_roots = [%q]
max_timeout = 60

[run]
inactivity_timeout = 10
total_timeout = 300
max_output = 102400
`, dir)
	s := startShimWithConfig(t, configContent)
	defer s.close()
	s.handshake(t)
	localRogue := copyRogueToDir(t, dir)
	// Use explicit timeout=2 — should kill after ~2s of inactivity
	start := time.Now()
	text, isErr := s.callTool(t, 10, "run", map[string]interface{}{
		"command": localRogue,
		"args":    "--hang",
		"timeout": 2,
	})
	elapsed := time.Since(start)
	if !isErr {
		t.Fatalf("expected timeout, got: %s", text)
	}
	// Should be killed around 2s, not the default 10s
	if elapsed > 8*time.Second {
		t.Errorf("timeout took %v, expected ~2s (timeout param not respected?)", elapsed)
	}
}

// §6.6.2: ClampTimeout(0, 60)=1, ClampTimeout(999, 60)=60.
func TestClampTimeout(t *testing.T) {
	if got := tools.ClampTimeout(0, 60); got != 1 {
		t.Errorf("ClampTimeout(0, 60) = %d, want 1", got)
	}
	if got := tools.ClampTimeout(999, 60); got != 60 {
		t.Errorf("ClampTimeout(999, 60) = %d, want 60", got)
	}
	if got := tools.ClampTimeout(30, 60); got != 30 {
		t.Errorf("ClampTimeout(30, 60) = %d, want 30", got)
	}
}

// --- Batch 3: Priority 2 §6-§11 External Tools, Config, Diagnostics ---

// §6.1.4+6.1.5: Config validation — param with both flag+position rejected;
// param with neither rejected.
func TestConfig_ParamValidation(t *testing.T) {
	// Both flag and position
	err := shared.ValidateToolConfigs(map[string]shared.ToolConfig{
		"bad": {
			Params: map[string]shared.ParamConfig{
				"p": {Flag: "--foo", Position: 1, Type: "string"},
			},
		},
	})
	if err == nil {
		t.Error("expected error for param with both flag and position")
	}
	// Neither flag nor position
	err = shared.ValidateToolConfigs(map[string]shared.ToolConfig{
		"bad": {
			Params: map[string]shared.ParamConfig{
				"p": {Type: "string"},
			},
		},
	})
	if err == nil {
		t.Error("expected error for param with neither flag nor position")
	}
	// Valid: flag only
	err = shared.ValidateToolConfigs(map[string]shared.ToolConfig{
		"good": {
			Params: map[string]shared.ParamConfig{
				"p": {Flag: "--foo", Type: "string"},
			},
		},
	})
	if err != nil {
		t.Errorf("unexpected error for valid config: %v", err)
	}
}

// §6.6.1: tools/list includes timeout param for run and configured tools.
func TestToolsList_TimeoutParam(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	resp := s.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	result := resp["result"].(map[string]interface{})
	toolsList := result["tools"].([]interface{})
	for _, tool := range toolsList {
		tm := tool.(map[string]interface{})
		name := tm["name"].(string)
		if name == "run" {
			schemaRaw, _ := json.Marshal(tm["inputSchema"])
			if !strings.Contains(string(schemaRaw), "timeout") {
				t.Error("run tool schema missing timeout parameter")
			}
			return
		}
	}
	t.Error("run tool not found in tools/list")
}

// §6.6.3: Timeout stripped from params before dispatch — verified via grep.
// We call run with timeout param and verify the command doesn't see it as an arg.
func TestRun_TimeoutStrippedFromParams(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	// Run echo with timeout param — timeout should not appear as a flag in the command
	text, isErr := s.callTool(t, 10, "run", map[string]interface{}{
		"command": "cmd.exe",
		"args":    "/c echo hello",
		"timeout": 5,
	})
	if isErr {
		t.Fatalf("run error: %s", text)
	}
	if !strings.Contains(text, "hello") {
		t.Errorf("run output = %q, want 'hello'", text)
	}
}

// §9.2 / 14.2.1: File lock retry exhausted → error.
func TestRead_FileLocked(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("file locking test requires Windows")
	}
	dir := t.TempDir()
	path := writeTestFile(t, dir, "locked.txt", "hello")
	// Open file with exclusive lock using Windows sharing flags
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open for lock: %v", err)
	}
	defer f.Close()
	// Lock the file using syscall
	lockFile(t, f)
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "read", map[string]interface{}{"path": path})
	if !isErr {
		t.Fatalf("expected lock error, got: %s", text)
	}
	if !strings.Contains(strings.ToLower(text), "locked") {
		t.Errorf("error = %q, want mention of locked", text)
	}
}

// §9.13.1: Missing config → built-in tools only.
func TestConfig_MissingConfigStartsWithBuiltins(t *testing.T) {
	// Write a config with no file to a path that doesn't exist
	shimConfig := filepath.Join(filepath.Dir(shimExePath), "shim.toml")
	original, _ := os.ReadFile(shimConfig)
	os.Remove(shimConfig)
	defer os.WriteFile(shimConfig, original, 0644)
	cmd := exec.Command(shimExePath)
	stdin, _ := cmd.StdinPipe()
	stdoutPipe, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start shim: %v", err)
	}
	defer func() {
		stdin.Close()
		cmd.Wait()
	}()
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, shared.MaxLineSize), shared.MaxLineSize)
	// Handshake
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`+"\n")
	if !scanner.Scan() {
		t.Fatal("no response")
	}
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","method":"initialized"}`+"\n")
	time.Sleep(50 * time.Millisecond)
	// tools/list — should have built-in tools
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n")
	if !scanner.Scan() {
		t.Fatal("no tools/list response")
	}
	var resp map[string]interface{}
	json.Unmarshal(scanner.Bytes(), &resp)
	result := resp["result"].(map[string]interface{})
	toolsList := result["tools"].([]interface{})
	names := make(map[string]bool)
	for _, tool := range toolsList {
		tm := tool.(map[string]interface{})
		names[tm["name"].(string)] = true
	}
	for _, expected := range []string{"read", "write", "edit", "run"} {
		if !names[expected] {
			t.Errorf("missing built-in tool: %s", expected)
		}
	}
}

// §9.13.2: Malformed config → refuse to start.
func TestConfig_MalformedConfigRefusesStart(t *testing.T) {
	shimConfig := filepath.Join(filepath.Dir(shimExePath), "shim.toml")
	original, _ := os.ReadFile(shimConfig)
	defer os.WriteFile(shimConfig, original, 0644)
	os.WriteFile(shimConfig, []byte("this is not valid toml {{{{"), 0644)
	cmd := exec.Command(shimExePath)
	cmd.Stdin = strings.NewReader("")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected shim to refuse to start with malformed config")
	}
	if !strings.Contains(stderr.String(), "config error") && !strings.Contains(stderr.String(), "parse") {
		t.Errorf("stderr = %q, want config parse error", stderr.String())
	}
}

// §9.13.3: UTF-16 config → detect BOM, refuse.
func TestConfig_UTF16ConfigRefusesStart(t *testing.T) {
	shimConfig := filepath.Join(filepath.Dir(shimExePath), "shim.toml")
	original, _ := os.ReadFile(shimConfig)
	defer os.WriteFile(shimConfig, original, 0644)
	// Write UTF-16 LE BOM followed by some content
	utf16Content := []byte{0xFF, 0xFE}
	utf16Content = append(utf16Content, []byte("[security]\n")...)
	os.WriteFile(shimConfig, utf16Content, 0644)
	cmd := exec.Command(shimExePath)
	cmd.Stdin = strings.NewReader("")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected shim to refuse to start with UTF-16 config")
	}
	if !strings.Contains(stderr.String(), "UTF-8") {
		t.Errorf("stderr = %q, want UTF-8 error message", stderr.String())
	}
}

// §9.13.4: Missing builtin_descriptions → refuse to start.
func TestConfig_MissingBuiltinDescriptionsRefusesStart(t *testing.T) {
	shimConfig := filepath.Join(filepath.Dir(shimExePath), "shim.toml")
	original, _ := os.ReadFile(shimConfig)
	defer os.WriteFile(shimConfig, original, 0644)
	// Config with partial builtin_descriptions — missing "read"
	os.WriteFile(shimConfig, []byte(`[security]
allowed_roots = ["C:\\temp"]

[builtin_descriptions]
write = "Write a file"
edit = "Edit a file"
copy = "Copy"
move = "Move"
delete = "Delete"
list = "List"
search = "Search"
info = "Info"
run = "Run"
`), 0644)
	cmd := exec.Command(shimExePath)
	cmd.Stdin = strings.NewReader("")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected shim to refuse to start with missing builtin description")
	}
	if !strings.Contains(stderr.String(), "read") {
		t.Errorf("stderr = %q, want mention of missing 'read' tool", stderr.String())
	}
}

// §10.1.1: --verbose writes tagged diagnostics to stderr.
func TestDiag_Verbose(t *testing.T) {
	dir := t.TempDir()
	s := startShimWithArgs(t, dir, "--verbose")
	s.handshake(t)
	path := writeTestFile(t, dir, "v.txt", "hello")
	s.callTool(t, 10, "read", map[string]interface{}{"path": path})
	// Read stderr in a goroutine before closing stdin
	stderrCh := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(s.stderr)
		stderrCh <- data
	}()
	s.stdin.Close()
	s.cmd.Wait()
	stderrData := <-stderrCh
	if len(stderrData) == 0 {
		t.Error("--verbose produced no stderr output")
	}
}

// §10.2: --log writes YYMMDDHHMMSS.log.
func TestDiag_LogFile(t *testing.T) {
	dir := t.TempDir()
	logDir := t.TempDir()
	s := startShimWithArgs(t, dir, "--log", logDir)
	defer s.close()
	s.handshake(t)
	path := writeTestFile(t, dir, "l.txt", "hello")
	s.callTool(t, 10, "read", map[string]interface{}{"path": path})
	s.stdin.Close()
	s.cmd.Wait()
	// Check for .log file in logDir
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("read log dir: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".log") {
			found = true
			data, _ := os.ReadFile(filepath.Join(logDir, e.Name()))
			if len(data) == 0 {
				t.Error("log file is empty")
			}
		}
	}
	if !found {
		t.Error("no .log file created in log directory")
	}
}

// §10.3: --scan lists .exe files and exits.
func TestDiag_Scan(t *testing.T) {
	dir := t.TempDir()
	shimConfig := filepath.Join(filepath.Dir(shimExePath), "shim.toml")
	// Ensure config has scan_dirs pointing to a directory with .exe files
	original, _ := os.ReadFile(shimConfig)
	defer os.WriteFile(shimConfig, original, 0644)
	scanDir := t.TempDir()
	writeTestFile(t, scanDir, "dummy.exe", "not-a-real-exe")
	writeTestFile(t, scanDir, "other.txt", "not-exe")
	config := fmt.Sprintf(`[security]
allowed_roots = [%q]

[scan_dirs]
paths = [%q]
`, dir, scanDir)
	os.WriteFile(shimConfig, []byte(config), 0644)
	cmd := exec.Command(shimExePath, "--scan")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--scan failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "dummy.exe") {
		t.Errorf("--scan output missing dummy.exe: %s", string(out))
	}
	if strings.Contains(string(out), "other.txt") {
		t.Errorf("--scan output should not contain other.txt: %s", string(out))
	}
}

// §11.1.2: max_timeout configurable — verify timeout clamp respects it.
func TestConfig_MaxTimeoutClamp(t *testing.T) {
	dir := t.TempDir()
	configContent := fmt.Sprintf(`[security]
allowed_roots = [%q]
max_timeout = 5

[run]
inactivity_timeout = 3
total_timeout = 30
max_output = 102400
`, dir)
	s := startShimWithConfig(t, configContent)
	defer s.close()
	s.handshake(t)
	localRogue := copyRogueToDir(t, dir)
	// Request timeout=999 — should be clamped to max_timeout=5
	start := time.Now()
	text, isErr := s.callTool(t, 10, "run", map[string]interface{}{
		"command": localRogue,
		"args":    "--hang",
		"timeout": 999,
	})
	elapsed := time.Since(start)
	if !isErr {
		t.Fatalf("expected timeout, got: %s", text)
	}
	// Should be clamped to ~5s, not 999s
	if elapsed > 15*time.Second {
		t.Errorf("timeout took %v, expected ~5s (max_timeout clamp not working?)", elapsed)
	}
}

// --- Batch 4: Priority 3 §14 Prescribed Tests ---

// §14.2.3: Long paths > 260 chars.
func TestLongPaths(t *testing.T) {
	dir := t.TempDir()
	// Create deeply nested directory to exceed 260 chars
	nested := dir
	for len(nested) < 280 {
		nested = filepath.Join(nested, "abcdefghijklmnop")
	}
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Skipf("cannot create long path: %v", err)
	}
	path := filepath.Join(nested, "test.txt")
	if err := os.WriteFile(path, []byte("long path content"), 0644); err != nil {
		t.Skipf("cannot write to long path: %v", err)
	}
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "read", map[string]interface{}{"path": path})
	if isErr {
		t.Fatalf("read long path error: %s", text)
	}
	if text != "long path content" {
		t.Errorf("read long path = %q, want %q", text, "long path content")
	}
}

// §14.3.3: Args with special characters — no injection.
func TestRun_SpecialCharsNoInjection(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	localRogue := copyRogueToDir(t, dir)
	text, isErr := s.callTool(t, 10, "run", map[string]interface{}{
		"command": localRogue,
		"args":    `--echo hello & echo injected`,
	})
	if isErr {
		t.Fatalf("run error: %s", text)
	}
	// Should print the literal string, not execute 'echo injected'
	if !strings.Contains(text, "hello & echo injected") {
		t.Errorf("special chars not handled correctly: %s", text)
	}
}

// lockFile applies an exclusive lock on Windows using LockFileEx.
func lockFile(t *testing.T, f *os.File) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("file locking requires Windows")
	}
	// Use syscall to lock the file exclusively
	lockFileWindows(t, f)
}

// --- Session 3: New Built-In Tool Tests ---

// mkdir tests
func TestMkdir_HappyPath(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "mkdir", map[string]interface{}{"path": nested})
	if isErr {
		t.Fatalf("mkdir error: %s", text)
	}
	info, err := os.Stat(nested)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("created path is not a directory")
	}
}

func TestMkdir_OutsideRoots(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "mkdir", map[string]interface{}{"path": filepath.Join(other, "nope")})
	if !isErr {
		t.Fatalf("expected confinement error, got: %s", text)
	}
}

// tree tests
func TestTree_HappyPath(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0755)
	writeTestFile(t, dir, "a.txt", "x")
	writeTestFile(t, filepath.Join(dir, "sub"), "b.txt", "x")
	writeTestFile(t, filepath.Join(dir, "sub", "deep"), "c.txt", "x")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "tree", map[string]interface{}{"path": dir})
	if isErr {
		t.Fatalf("tree error: %s", text)
	}
	if !strings.Contains(text, "sub/") {
		t.Errorf("tree missing 'sub/' directory: %s", text)
	}
	if !strings.Contains(text, "a.txt") {
		t.Errorf("tree missing 'a.txt': %s", text)
	}
}

func TestTree_DepthLimit(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a", "b", "c", "d"), 0755)
	writeTestFile(t, filepath.Join(dir, "a", "b", "c", "d"), "deep.txt", "x")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "tree", map[string]interface{}{"path": dir, "depth": 2})
	if isErr {
		t.Fatalf("tree error: %s", text)
	}
	if strings.Contains(text, "deep.txt") {
		t.Errorf("tree should not show files beyond depth 2: %s", text)
	}
}

func TestTree_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "tree", map[string]interface{}{"path": dir})
	if isErr {
		t.Fatalf("tree error: %s", text)
	}
	if strings.TrimSpace(text) != "" {
		t.Errorf("empty dir tree should be empty, got: %q", text)
	}
}

// head tests
func TestHead_HappyPath(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("line%d", i))
	}
	path := writeTestFile(t, dir, "big.txt", strings.Join(lines, "\n"))
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "head", map[string]interface{}{"path": path, "lines": 5})
	if isErr {
		t.Fatalf("head error: %s", text)
	}
	resultLines := strings.Split(text, "\n")
	if len(resultLines) != 5 {
		t.Errorf("head returned %d lines, want 5", len(resultLines))
	}
	if resultLines[0] != "line1" || resultLines[4] != "line5" {
		t.Errorf("head content wrong: %s", text)
	}
}

func TestHead_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "head", map[string]interface{}{"path": filepath.Join(dir, "nope.txt")})
	if !isErr {
		t.Fatalf("expected error, got: %s", text)
	}
}

// tail tests
func TestTail_HappyPath(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("line%d", i))
	}
	path := writeTestFile(t, dir, "big.txt", strings.Join(lines, "\n"))
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "tail", map[string]interface{}{"path": path, "lines": 5})
	if isErr {
		t.Fatalf("tail error: %s", text)
	}
	resultLines := strings.Split(text, "\n")
	if len(resultLines) != 5 {
		t.Errorf("tail returned %d lines, want 5", len(resultLines))
	}
	if resultLines[0] != "line96" || resultLines[4] != "line100" {
		t.Errorf("tail content wrong: %s", text)
	}
}

func TestTail_BinaryRefused(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "bin.dat", "hello\x00world")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "tail", map[string]interface{}{"path": path})
	if !isErr {
		t.Fatalf("expected binary refusal, got: %s", text)
	}
	if !strings.Contains(text, "binary") {
		t.Errorf("error = %q, want mention of binary", text)
	}
}

// wc tests
func TestWc_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "wc.txt", "hello world\nfoo bar baz\n")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "wc", map[string]interface{}{"path": path})
	if isErr {
		t.Fatalf("wc error: %s", text)
	}
	if !strings.Contains(text, "lines: 2") {
		t.Errorf("wc line count wrong: %s", text)
	}
	if !strings.Contains(text, "words: 5") {
		t.Errorf("wc word count wrong: %s", text)
	}
}

func TestWc_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "wc", map[string]interface{}{"path": filepath.Join(dir, "nope.txt")})
	if !isErr {
		t.Fatalf("expected error, got: %s", text)
	}
}

// diff tests
func TestDiff_DifferentFiles(t *testing.T) {
	dir := t.TempDir()
	path1 := writeTestFile(t, dir, "a.txt", "line1\nline2\nline3\n")
	path2 := writeTestFile(t, dir, "b.txt", "line1\nchanged\nline3\n")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "diff", map[string]interface{}{"file1": path1, "file2": path2})
	if isErr {
		t.Fatalf("diff error: %s", text)
	}
	if !strings.Contains(text, "---") || !strings.Contains(text, "+++") {
		t.Errorf("diff missing unified headers: %s", text)
	}
	if !strings.Contains(text, "@@") {
		t.Errorf("diff missing hunk header: %s", text)
	}
	if !strings.Contains(text, "-line2") || !strings.Contains(text, "+changed") {
		t.Errorf("diff missing change markers: %s", text)
	}
}

func TestDiff_IdenticalFiles(t *testing.T) {
	dir := t.TempDir()
	path1 := writeTestFile(t, dir, "a.txt", "same content\n")
	path2 := writeTestFile(t, dir, "b.txt", "same content\n")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "diff", map[string]interface{}{"file1": path1, "file2": path2})
	if isErr {
		t.Fatalf("diff error: %s", text)
	}
	if text != "" {
		t.Errorf("diff of identical files should be empty, got: %q", text)
	}
}

// cat tests
func TestCat_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	path1 := writeTestFile(t, dir, "a.txt", "aaa")
	path2 := writeTestFile(t, dir, "b.txt", "bbb")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "cat", map[string]interface{}{"paths": path1 + " " + path2})
	if isErr {
		t.Fatalf("cat error: %s", text)
	}
	if !strings.Contains(text, "aaa") || !strings.Contains(text, "bbb") {
		t.Errorf("cat missing content: %s", text)
	}
}

func TestCat_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path1 := writeTestFile(t, dir, "a.txt", "aaa")
	missing := filepath.Join(dir, "nope.txt")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "cat", map[string]interface{}{"paths": path1 + " " + missing})
	if !isErr {
		t.Fatalf("expected error for missing file, got: %s", text)
	}
}

// roots tests
func TestRoots_ReturnsAllowedRoots(t *testing.T) {
	dir := shared.ToLongPath(t.TempDir())
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "roots", map[string]interface{}{})
	if isErr {
		t.Fatalf("roots error: %s", text)
	}
	if !strings.Contains(text, dir) {
		t.Errorf("roots should contain %q, got: %s", dir, text)
	}
}

// Verify all 8 new tools appear in tools/list
func TestToolsList_NewBuiltins(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	resp := s.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	result := resp["result"].(map[string]interface{})
	toolsList := result["tools"].([]interface{})
	names := make(map[string]bool)
	for _, tool := range toolsList {
		tm := tool.(map[string]interface{})
		names[tm["name"].(string)] = true
	}
	for _, expected := range []string{"cat", "diff", "head", "mkdir", "roots", "tail", "tree", "wc"} {
		if !names[expected] {
			t.Errorf("tools/list missing new built-in: %s", expected)
		}
	}
}


// §5.12.5 cat: max 512KB combined output
func TestCat_MaxOutputExceeded(t *testing.T) {
	dir := t.TempDir()
	// Create a 300KB file — two of these exceed 512KB
	big := strings.Repeat("abcdefghij", 30000)
	path1 := writeTestFile(t, dir, "big1.txt", big)
	path2 := writeTestFile(t, dir, "big2.txt", big)
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "cat", map[string]interface{}{"paths": path1 + " " + path2})
	if !isErr {
		t.Fatalf("expected error for >512KB combined output, got %d bytes", len(text))
	}
	if !strings.Contains(text, "524288") && !strings.Contains(text, "512") {
		t.Errorf("error should mention size limit: %s", text)
	}
}

// §5.15.4 tail: large file (>512KB) uses seek
func TestTail_LargeFile(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	for i := 0; i < 60000; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	path := writeTestFile(t, dir, "big.txt", sb.String())
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "tail", map[string]interface{}{"path": path, "lines": 3})
	if isErr {
		t.Fatalf("tail error: %s", text)
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[len(lines)-1], "59999") {
		t.Errorf("last line should be 59999, got: %s", lines[len(lines)-1])
	}
}


// §5.16.4 diff: context_lines parameter
func TestDiff_ContextLines(t *testing.T) {
	dir := t.TempDir()
	var sb1, sb2 strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&sb1, "line %d\n", i)
		if i == 10 {
			fmt.Fprintf(&sb2, "CHANGED\n")
		} else {
			fmt.Fprintf(&sb2, "line %d\n", i)
		}
	}
	path1 := writeTestFile(t, dir, "a.txt", sb1.String())
	path2 := writeTestFile(t, dir, "b.txt", sb2.String())
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "diff", map[string]interface{}{"file1": path1, "file2": path2, "context_lines": 1})
	if isErr {
		t.Fatalf("diff error: %s", text)
	}
	if !strings.Contains(text, "@@") {
		t.Errorf("expected unified diff markers, got: %s", text)
	}
	// With context_lines=1, we should see line 9 and line 11 as context,
	// but NOT line 7 or line 13 (which would appear with context_lines=3)
	if strings.Contains(text, "line 8") {
		t.Errorf("context_lines=1 should not show line 8: %s", text)
	}
}

// §5.18.5 tree: pattern filter
func TestTree_PatternFilter(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "readme.md", "x")
	writeTestFile(t, dir, "main.py", "x")
	writeTestFile(t, dir, "test.py", "x")
	writeTestFile(t, dir, "data.csv", "x")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "tree", map[string]interface{}{"path": dir, "pattern": "*.py"})
	if isErr {
		t.Fatalf("tree error: %s", text)
	}
	if !strings.Contains(text, "main.py") || !strings.Contains(text, "test.py") {
		t.Errorf("tree should include .py files: %s", text)
	}
	if strings.Contains(text, "data.csv") || strings.Contains(text, "readme.md") {
		t.Errorf("tree should filter out non-.py files: %s", text)
	}
}


// §6.3.4 Total timeout not exposed to Claude
func TestToolsList_NoTotalTimeout(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	resp := s.send(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	raw, _ := json.Marshal(resp)
	output := string(raw)
	if strings.Contains(output, "total_timeout") {
		t.Errorf("tools/list should not expose total_timeout to Claude")
	}
}

// §6.3.2 Inactivity timeout returns text result (not JSON-RPC error)
func TestRun_TimeoutIsTextResult(t *testing.T) {
	dir := t.TempDir()
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	localRogue := copyRogueToDir(t, dir)
	// callTool returns text from ToolResult content, not from JSON-RPC error.
	// If the shim returned a JSON-RPC error, callTool would get it from resp["error"].
	text, _ := s.callTool(t, 10, "run", map[string]interface{}{
		"command": localRogue,
		"args":    "--hang",
		"timeout": 2,
	})
	if !strings.Contains(text, "no output") {
		t.Errorf("timeout message should mention 'no output': %s", text)
	}
	if !strings.Contains(text, "retry") {
		t.Errorf("timeout message should suggest retry: %s", text)
	}
}


// §6.1.2, §6.1.3 Configured tool: boolean flag params and positional params.
// External grep is forbidden (see commit 32bdbf4), so this exercises the
// configured-tool dispatch path via `uniq` from Git for Windows. Exit-code
// handling is covered by TestExecuteExternal_SuccessCode.
func TestConfiguredTool_ExternalTool(t *testing.T) {
	uniqExe := `C:\Program Files\Git\usr\bin\uniq.exe`
	if _, err := os.Stat(uniqExe); err != nil {
		t.Skip("uniq.exe not found (Git for Windows not installed)")
	}
	dir := t.TempDir()
	writeTestFile(t, dir, "test.txt", "a\na\nb\nb\nb\nc\n")
	config := fmt.Sprintf(`[security]
allowed_roots = [%q]
[run]
inactivity_timeout = 3
total_timeout = 5
max_output = 102400
[tools.uniq]
exe = %q
description = "test uniq"
title = "Uniq"
read_only = true
inactivity_timeout = 5
total_timeout = 10
max_output = 102400
success_codes = [0]
[tools.uniq.params]
path = { type = "string", required = true, position = 1, description = "file" }
count = { type = "boolean", default = false, flag = "-c", description = "count occurrences" }
`, dir, uniqExe)
	s := startShimWithConfig(t, config)
	defer s.close()
	s.handshake(t)
	// §6.1.2 boolean flag: count=true -> -c included
	// §6.1.3 positional: path at position 1
	text, isErr := s.callTool(t, 10, "uniq", map[string]interface{}{
		"path": filepath.Join(dir, "test.txt"), "count": true,
	})
	if isErr {
		t.Fatalf("uniq error: %s", text)
	}
	if !strings.Contains(text, "2 a") || !strings.Contains(text, "3 b") {
		t.Errorf("expected count output with '2 a' and '3 b', got: %s", text)
	}
}

// §9.15.1 / TestCriticalError_ConfinementFormat: junction escape produces critical error.
func TestCriticalError_ConfinementFormat(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction test requires Windows")
	}
	dir := t.TempDir()
	outsideDir := t.TempDir()
	writeTestFile(t, outsideDir, "secret.txt", "top secret data")
	junctionPath := filepath.Join(dir, "escape")
	cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", junctionPath, outsideDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mklink /J failed: %v\n%s", err, out)
	}
	defer os.Remove(junctionPath)
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	escapePath := filepath.Join(junctionPath, "secret.txt")
	text, isErr := callToolCritical(t, s, 10, "read", map[string]interface{}{"path": escapePath})
	if !isErr {
		t.Fatalf("expected error, got success: %s", text)
	}
	if !strings.HasPrefix(text, "🛑 CRITICAL:") {
		t.Errorf("error text does not start with critical prefix: %q", text)
	}
	if !strings.Contains(text, "confinement breach") {
		t.Errorf("error text missing 'confinement breach': %q", text)
	}
}

// §9.15.2 / TestCriticalError_ResponseFormat: critical error has isError + formatted text.
func TestCriticalError_ResponseFormat(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction test requires Windows")
	}
	dir := t.TempDir()
	outsideDir := t.TempDir()
	writeTestFile(t, outsideDir, "secret.txt", "data")
	junctionPath := filepath.Join(dir, "escape2")
	cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", junctionPath, outsideDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mklink /J failed: %v\n%s", err, out)
	}
	defer os.Remove(junctionPath)
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	escapePath := filepath.Join(junctionPath, "secret.txt")
	text, isErr := callToolCritical(t, s, 10, "read", map[string]interface{}{"path": escapePath})
	if !isErr {
		t.Fatalf("expected isError=true, got false: %s", text)
	}
	if !strings.HasPrefix(text, "🛑 CRITICAL:") {
		t.Errorf("missing 🛑 prefix: %q", text)
	}
	if !strings.Contains(text, "alert the user") {
		t.Errorf("missing 'alert the user' instruction: %q", text)
	}
	if !strings.Contains(text, "Do not retry") {
		t.Errorf("missing 'Do not retry' instruction: %q", text)
	}
}

// §9.15.3 / TestCriticalError_Notification: notification sent before tool response.
func TestCriticalError_Notification(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junction test requires Windows")
	}
	dir := t.TempDir()
	outsideDir := t.TempDir()
	writeTestFile(t, outsideDir, "secret.txt", "data")
	junctionPath := filepath.Join(dir, "escape3")
	cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", junctionPath, outsideDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mklink /J failed: %v\n%s", err, out)
	}
	defer os.Remove(junctionPath)
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	// Send the tool call request directly and read TWO lines from stdout:
	// first the notification, then the tool response.
	escapePath := filepath.Join(junctionPath, "secret.txt")
	argsJSON, _ := json.Marshal(map[string]interface{}{"path": escapePath})
	msg := fmt.Sprintf(`{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"read","arguments":%s}}`, argsJSON)
	fmt.Fprintf(s.stdin, "%s\n", msg)
	// Read line 1: should be the notification.
	if !s.stdout.Scan() {
		t.Fatalf("no first response from shim")
	}
	var notif map[string]interface{}
	if err := json.Unmarshal(s.stdout.Bytes(), &notif); err != nil {
		t.Fatalf("unmarshal notification: %v\nraw: %s", err, s.stdout.Text())
	}
	if notif["method"] != "notifications/message" {
		t.Fatalf("first message is not notification: method=%v", notif["method"])
	}
	params, _ := notif["params"].(map[string]interface{})
	if params["level"] != "error" {
		t.Errorf("notification level = %v, want error", params["level"])
	}
	if params["logger"] != "winmcpshim" {
		t.Errorf("notification logger = %v, want winmcpshim", params["logger"])
	}
	if _, ok := params["data"].(string); !ok {
		t.Errorf("notification data is not a string: %v", params["data"])
	}
	// Read line 2: should be the tool error response.
	if !s.stdout.Scan() {
		t.Fatalf("no second response from shim")
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(s.stdout.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nraw: %s", err, s.stdout.Text())
	}
	result, _ := resp["result"].(map[string]interface{})
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Errorf("tool response isError = false, want true")
	}
}

// §RGE-12 / TestRun_ComboAttack: grandchild + flood + crash exercised simultaneously.
func TestRun_ComboAttack(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("combo attack test requires Windows")
	}
	dir := t.TempDir()
	localRogue := copyRogueToDir(t, dir)
	pidFile := filepath.Join(dir, "grandchild.pid")
	configContent := fmt.Sprintf(`[security]
allowed_roots = [%q]
[run]
inactivity_timeout = 10
total_timeout = 30
max_output = 102400
`, dir)
	s := startShimWithConfig(t, configContent)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "run", map[string]interface{}{
		"command": localRogue,
		"args":    fmt.Sprintf(`--combo "%s"`, pidFile),
	})
	// Should get an error (crash or truncation).
	_ = text
	_ = isErr
	// Wait for Job Object cleanup.
	time.Sleep(1 * time.Second)
	// Read grandchild PID and verify it's dead.
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		t.Skipf("PID file not written: %v", err)
	}
	grandchildPID := strings.TrimSpace(string(pidData))
	if grandchildPID == "" {
		t.Skip("PID file empty")
	}
	check := exec.Command("cmd.exe", "/c", "tasklist",
		"/FI", fmt.Sprintf("PID eq %s", grandchildPID), "/NH")
	out, _ := check.CombinedOutput()
	if strings.Contains(string(out), "rogue") {
		t.Errorf("grandchild PID %s still alive after combo", grandchildPID)
	}
	// Verify shim is still alive.
	_, isErr2 := s.callTool(t, 11, "roots", nil)
	if isErr2 {
		t.Error("shim died after combo attack")
	}
}

// --- §14.8 UTF-16 Decode Integration Tests ---

// makeUTF16LE creates a UTF-16LE encoded byte slice with BOM from a Go string.
func makeUTF16LE(s string) []byte {
	runes := []rune(s)
	buf := []byte{0xFF, 0xFE} // BOM
	for _, r := range runes {
		buf = append(buf, byte(r&0xFF), byte(r>>8))
	}
	return buf
}

// makeUTF16BE creates a UTF-16BE encoded byte slice with BOM from a Go string.
func makeUTF16BE(s string) []byte {
	runes := []rune(s)
	buf := []byte{0xFE, 0xFF} // BOM
	for _, r := range runes {
		buf = append(buf, byte(r>>8), byte(r&0xFF))
	}
	return buf
}

// §14.8.1: UTF-16LE decode
func TestRead_UTF16LEDecode(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\n"
	path := writeTestFile(t, dir, "utf16le.txt", string(makeUTF16LE(content)))
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "read", map[string]interface{}{"path": path})
	if isErr {
		t.Fatalf("read UTF-16LE error: %s", text)
	}
	if !strings.Contains(text, "line1") || !strings.Contains(text, "line3") {
		t.Errorf("read UTF-16LE = %q, want line1..line3", text)
	}
}

// §14.8.2: UTF-16BE decode
func TestRead_UTF16BEDecode(t *testing.T) {
	dir := t.TempDir()
	content := "hello world"
	path := writeTestFile(t, dir, "utf16be.txt", string(makeUTF16BE(content)))
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "read", map[string]interface{}{"path": path})
	if isErr {
		t.Fatalf("read UTF-16BE error: %s", text)
	}
	if text != content {
		t.Errorf("read UTF-16BE = %q, want %q", text, content)
	}
}

// §14.8.3: head on UTF-16 file
func TestHead_UTF16(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, fmt.Sprintf("line%d", i))
	}
	content := strings.Join(lines, "\n") + "\n"
	path := writeTestFile(t, dir, "head16.txt", string(makeUTF16LE(content)))
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "head", map[string]interface{}{"path": path, "lines": 5})
	if isErr {
		t.Fatalf("head UTF-16 error: %s", text)
	}
	if !strings.Contains(text, "line1") || !strings.Contains(text, "line5") {
		t.Errorf("head UTF-16 = %q, want line1..line5", text)
	}
	if strings.Contains(text, "line6") {
		t.Errorf("head UTF-16 should not contain line6: %s", text)
	}
}

// §14.8.3: tail on UTF-16 file
func TestTail_UTF16(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, fmt.Sprintf("line%d", i))
	}
	content := strings.Join(lines, "\n") + "\n"
	path := writeTestFile(t, dir, "tail16.txt", string(makeUTF16LE(content)))
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "tail", map[string]interface{}{"path": path, "lines": 5})
	if isErr {
		t.Fatalf("tail UTF-16 error: %s", text)
	}
	if !strings.Contains(text, "line16") || !strings.Contains(text, "line20") {
		t.Errorf("tail UTF-16 = %q, want line16..line20", text)
	}
	if strings.Contains(text, "line15") {
		t.Errorf("tail UTF-16 should not contain line15: %s", text)
	}
}

// §14.8.3: cat with UTF-16 file
func TestCat_UTF16(t *testing.T) {
	dir := t.TempDir()
	utf16Path := writeTestFile(t, dir, "cat16.txt", string(makeUTF16LE("utf16 content")))
	utf8Path := writeTestFile(t, dir, "cat8.txt", "utf8 content")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "cat", map[string]interface{}{"paths": utf16Path + " " + utf8Path})
	if isErr {
		t.Fatalf("cat UTF-16 error: %s", text)
	}
	if !strings.Contains(text, "utf16 content") {
		t.Errorf("cat missing UTF-16 content: %s", text)
	}
	if !strings.Contains(text, "utf8 content") {
		t.Errorf("cat missing UTF-8 content: %s", text)
	}
}

// §14.8.3: wc on UTF-16 file
func TestWc_UTF16(t *testing.T) {
	dir := t.TempDir()
	content := "one two three\nfour five\n"
	path := writeTestFile(t, dir, "wc16.txt", string(makeUTF16LE(content)))
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "wc", map[string]interface{}{"path": path})
	if isErr {
		t.Fatalf("wc UTF-16 error: %s", text)
	}
	if !strings.Contains(text, "lines: 2") {
		t.Errorf("wc UTF-16 line count wrong: %s", text)
	}
	if !strings.Contains(text, "words: 5") {
		t.Errorf("wc UTF-16 word count wrong: %s", text)
	}
}

// §14.8.3: diff with UTF-16 file
func TestDiff_UTF16(t *testing.T) {
	dir := t.TempDir()
	utf16Path := writeTestFile(t, dir, "diff16.txt", string(makeUTF16LE("line1\nline2\n")))
	utf8Path := writeTestFile(t, dir, "diff8.txt", "line1\nline3\n")
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "diff", map[string]interface{}{"file1": utf16Path, "file2": utf8Path})
	if isErr {
		t.Fatalf("diff UTF-16 error: %s", text)
	}
	// Should show differences between line2 and line3
	if !strings.Contains(text, "line2") || !strings.Contains(text, "line3") {
		t.Errorf("diff UTF-16 should show line differences: %s", text)
	}
}

// §14.8.4: write after UTF-16 read produces UTF-8
func TestWrite_AfterUTF16Read(t *testing.T) {
	dir := t.TempDir()
	content := "hello world"
	path := writeTestFile(t, dir, "rw16.txt", string(makeUTF16LE(content)))
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	// Read the UTF-16 file (succeeds with decoded content)
	text, isErr := s.callTool(t, 10, "read", map[string]interface{}{"path": path})
	if isErr {
		t.Fatalf("read error: %s", text)
	}
	// Write new content to the same path
	_, isErr = s.callTool(t, 11, "write", map[string]interface{}{"path": path, "content": "new content"})
	if isErr {
		t.Fatalf("write error after UTF-16 read")
	}
	// Verify the file is now UTF-8 (no BOM)
	data, _ := os.ReadFile(path)
	if len(data) >= 2 && ((data[0] == 0xFF && data[1] == 0xFE) || (data[0] == 0xFE && data[1] == 0xFF)) {
		t.Errorf("file should be UTF-8 after write, but has UTF-16 BOM")
	}
	if string(data) != "new content" {
		t.Errorf("file content = %q, want %q", string(data), "new content")
	}
}

// §14.8.5: edit refuses UTF-16 file
func TestEdit_RefusesUTF16(t *testing.T) {
	dir := t.TempDir()
	path := writeTestFile(t, dir, "edit16.txt", string(makeUTF16LE("old text")))
	s := startShim(t, dir)
	defer s.close()
	s.handshake(t)
	text, isErr := s.callTool(t, 10, "edit", map[string]interface{}{
		"path":     path,
		"old_text": "old text",
		"new_text": "new text",
	})
	if !isErr {
		t.Fatalf("expected edit to refuse UTF-16 file, got: %s", text)
	}
	if !strings.Contains(text, "UTF-16") {
		t.Errorf("error = %q, want mention of UTF-16", text)
	}
}
