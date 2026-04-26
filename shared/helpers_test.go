package shared

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// ===== AtomicWrite =====

func TestAtomicWrite_CRLFPreservation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crlf.txt")
	// Write initial CRLF content
	os.WriteFile(path, []byte("line1\r\nline2\r\n"), 0644)
	// AtomicWrite replaces the file
	err := AtomicWrite(path, []byte("new content\r\n"))
	if err != nil {
		t.Fatalf("AtomicWrite error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new content\r\n" {
		t.Errorf("expected CRLF content preserved, got %q", string(data))
	}
}

func TestAtomicWrite_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	err := AtomicWrite(path, []byte("hello"))
	if err != nil {
		t.Fatalf("AtomicWrite error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}
}

func TestAtomicWrite_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	os.WriteFile(path, []byte("old"), 0644)
	err := AtomicWrite(path, []byte("new"))
	if err != nil {
		t.Fatalf("AtomicWrite error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Errorf("expected 'new', got %q", string(data))
	}
}

// ===== CheckTextFile =====

func TestCheckTextFile_Binary(t *testing.T) {
	data := []byte{'h', 'e', 'l', 0x00, 'l', 'o'}
	err := CheckTextFile(data)
	if err == nil {
		t.Fatal("expected error for binary data")
	}
	if !strings.Contains(err.Error(), "binary") {
		t.Errorf("expected 'binary' in error, got %q", err.Error())
	}
}

func TestCheckTextFile_TooLarge(t *testing.T) {
	// CheckTextFile doesn't check size — it checks content. MaxReadSize check
	// is done at the caller level. Just verify no crash on large data.
	data := make([]byte, MaxReadSize+1)
	for i := range data {
		data[i] = 'a'
	}
	err := CheckTextFile(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckTextFile_UTF16(t *testing.T) {
	data := []byte{0xFF, 0xFE, 'h', 0, 'i', 0}
	err := CheckTextFile(data)
	if err == nil {
		t.Fatal("expected error for UTF-16 data")
	}
	if !strings.Contains(err.Error(), "UTF-16") {
		t.Errorf("expected 'UTF-16' in error, got %q", err.Error())
	}
}

// ===== CopyDir =====

func TestCopyDir_DeepNested(t *testing.T) {
	src := t.TempDir()
	// Create 3 levels deep
	deep := filepath.Join(src, "a", "b", "c")
	os.MkdirAll(deep, 0755)
	os.WriteFile(filepath.Join(deep, "file.txt"), []byte("deep content"), 0644)
	os.WriteFile(filepath.Join(src, "root.txt"), []byte("root content"), 0644)
	dst := filepath.Join(t.TempDir(), "copy")
	got, err := CopyDir(src, dst)
	if err != nil {
		t.Fatalf("CopyDir error: %v", err)
	}
	if !strings.Contains(got, "2 files") {
		t.Errorf("expected '2 files' in result, got %q", got)
	}
	data, _ := os.ReadFile(filepath.Join(dst, "a", "b", "c", "file.txt"))
	if string(data) != "deep content" {
		t.Errorf("expected 'deep content', got %q", string(data))
	}
}

// ===== CopyFile =====

func TestCopyFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	os.WriteFile(src, []byte("copy me"), 0644)
	dst := filepath.Join(dir, "dst.txt")
	got, err := CopyFile(src, dst)
	if err != nil {
		t.Fatalf("CopyFile error: %v", err)
	}
	if !strings.Contains(got, "Copied") {
		t.Errorf("expected 'Copied' message, got %q", got)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "copy me" {
		t.Errorf("expected 'copy me', got %q", string(data))
	}
}

func TestCopyFile_SourceNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := CopyFile(filepath.Join(dir, "nope.txt"), filepath.Join(dir, "dst.txt"))
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

// ===== OpenFileWithRetry =====

func TestOpenFileWithRetry_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello"), 0644)
	f, err := OpenFileWithRetry(path)
	if err != nil {
		t.Fatalf("OpenFileWithRetry error: %v", err)
	}
	f.Close()
}

// ===== OptionalInt =====

func TestOptionalInt_Missing(t *testing.T) {
	m := map[string]interface{}{}
	val, ok := OptionalInt(m, "key")
	if ok {
		t.Error("expected ok=false for missing key")
	}
	if val != 0 {
		t.Errorf("expected 0 default, got %d", val)
	}
}

func TestOptionalInt_Present(t *testing.T) {
	m := map[string]interface{}{"key": float64(42)}
	val, ok := OptionalInt(m, "key")
	if !ok {
		t.Error("expected ok=true")
	}
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}
}

func TestOptionalInt_WrongType(t *testing.T) {
	m := map[string]interface{}{"key": "not a number"}
	_, ok := OptionalInt(m, "key")
	if ok {
		t.Error("expected ok=false for wrong type")
	}
}

// ===== ReadFileWithRetry =====

func TestReadFileWithRetry_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello"), 0644)
	data, err := ReadFileWithRetry(path)
	if err != nil {
		t.Fatalf("ReadFileWithRetry error: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}
}

func TestReadFileWithRetry_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadFileWithRetry(filepath.Join(dir, "nope.txt"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// ===== RequireString =====

func TestRequireString_Missing(t *testing.T) {
	m := map[string]interface{}{}
	_, err := RequireString(m, "key")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("expected 'missing' in error, got %q", err.Error())
	}
}

func TestRequireString_WrongType(t *testing.T) {
	m := map[string]interface{}{"key": 123}
	_, err := RequireString(m, "key")
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
	if !strings.Contains(err.Error(), "string") {
		t.Errorf("expected 'string' in error, got %q", err.Error())
	}
}

// ===== StatWithRetry =====

func TestStatWithRetry_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello"), 0644)
	info, err := StatWithRetry(path)
	if err != nil {
		t.Fatalf("StatWithRetry error: %v", err)
	}
	if info.Size() != 5 {
		t.Errorf("expected size 5, got %d", info.Size())
	}
}

func TestStatWithRetry_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := StatWithRetry(filepath.Join(dir, "nope.txt"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// ===== IsNotExist =====

func TestIsNotExist_True(t *testing.T) {
	_, err := os.Open(filepath.Join(t.TempDir(), "nope.txt"))
	if !IsNotExist(err) {
		t.Error("expected IsNotExist to be true for missing file")
	}
}

func TestIsNotExist_False(t *testing.T) {
	if IsNotExist(os.ErrPermission) {
		t.Error("expected IsNotExist to be false for permission error")
	}
}

// ===== OptionalBool =====

func TestOptionalBool_Missing(t *testing.T) {
	m := map[string]interface{}{}
	val, ok := OptionalBool(m, "key")
	if ok {
		t.Error("expected ok=false for missing key")
	}
	if val {
		t.Error("expected false default")
	}
}

func TestOptionalBool_Present(t *testing.T) {
	m := map[string]interface{}{"key": true}
	val, ok := OptionalBool(m, "key")
	if !ok {
		t.Error("expected ok=true")
	}
	if !val {
		t.Error("expected true")
	}
}

func TestOptionalBool_WrongType(t *testing.T) {
	m := map[string]interface{}{"key": "not bool"}
	_, ok := OptionalBool(m, "key")
	if ok {
		t.Error("expected ok=false for wrong type")
	}
}

// ===== OptionalString =====

func TestOptionalString_Missing(t *testing.T) {
	m := map[string]interface{}{}
	val, ok := OptionalString(m, "key")
	if ok {
		t.Error("expected ok=false")
	}
	if val != "" {
		t.Errorf("expected empty default, got %q", val)
	}
}

func TestOptionalString_Present(t *testing.T) {
	m := map[string]interface{}{"key": "hello"}
	val, ok := OptionalString(m, "key")
	if !ok {
		t.Error("expected ok=true")
	}
	if val != "hello" {
		t.Errorf("expected 'hello', got %q", val)
	}
}

// ===== OptionalInt with int type =====

func TestOptionalInt_IntType(t *testing.T) {
	m := map[string]interface{}{"key": 42}
	val, ok := OptionalInt(m, "key")
	if !ok {
		t.Error("expected ok=true for int type")
	}
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}
}

// ===== NormaliseToCRLF =====

func TestNormaliseToCRLF_BareNewlines(t *testing.T) {
	got := NormaliseToCRLF([]byte("a\nb\nc"))
	if string(got) != "a\r\nb\r\nc" {
		t.Errorf("expected CRLF, got %q", string(got))
	}
}

func TestNormaliseToCRLF_AlreadyCRLF(t *testing.T) {
	got := NormaliseToCRLF([]byte("a\r\nb\r\n"))
	if string(got) != "a\r\nb\r\n" {
		t.Errorf("expected unchanged CRLF, got %q", string(got))
	}
}

// ===== AtomicWrite with parent dirs =====

func TestAtomicWrite_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "file.txt")
	err := AtomicWrite(path, []byte("deep write"))
	if err != nil {
		t.Fatalf("AtomicWrite error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "deep write" {
		t.Errorf("expected 'deep write', got %q", string(data))
	}
}

// ===== OpenFileWithRetry_NotFound =====

func TestOpenFileWithRetry_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := OpenFileWithRetry(filepath.Join(dir, "nope.txt"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "File not found") {
		t.Errorf("expected 'not found' in error, got %q", err.Error())
	}
}

// ===== CheckTextFile empty =====

func TestCheckTextFile_Empty(t *testing.T) {
	err := CheckTextFile([]byte{})
	if err != nil {
		t.Fatalf("unexpected error for empty data: %v", err)
	}
}

func TestCheckTextFile_Normal(t *testing.T) {
	err := CheckTextFile([]byte("hello world\nfoo bar\n"))
	if err != nil {
		t.Fatalf("unexpected error for normal text: %v", err)
	}
}

// ===== DetectTextEncoding empty =====

func TestDetectTextEncoding_Empty(t *testing.T) {
	enc, err := DetectTextEncoding([]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc != UTF8 {
		t.Errorf("expected UTF8 for empty, got %d", enc)
	}
}

// ===== LoadConfig =====

func TestLoadConfig_MissingFile(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	// Should return default config
	if cfg.Run.InactivityTimeout != DefaultInactivityTimeout {
		t.Errorf("expected default inactivity timeout %d, got %d", DefaultInactivityTimeout, cfg.Run.InactivityTimeout)
	}
	if len(cfg.BuiltinDescriptions) == 0 {
		t.Error("expected default builtin descriptions")
	}
}

func TestLoadConfig_ValidToml(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	content := `
[security]
allowed_roots = ["C:\\Test"]
max_timeout = 30

[run]
inactivity_timeout = 15
total_timeout = 120
max_output = 51200

[builtin_descriptions]
read = "Read a file"
write = "Write a file"
edit = "Edit a file"
copy = "Copy"
move = "Move"
delete = "Delete"
list = "List"
search = "Search"
cat = "Cat"
diff = "Diff"
head = "Head"
info = "Info"
mkdir = "Mkdir"
roots = "Roots"
tail = "Tail"
tree = "Tree"
wc = "Wc"
run = "Run"
grep = "Grep"
`
	os.WriteFile(path, []byte(content), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.Run.InactivityTimeout != 15 {
		t.Errorf("expected 15, got %d", cfg.Run.InactivityTimeout)
	}
	if len(cfg.Security.AllowedRoots) != 1 {
		t.Errorf("expected 1 root, got %d", len(cfg.Security.AllowedRoots))
	}
}

func TestLoadConfig_UTF16Rejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "utf16.toml")
	os.WriteFile(path, []byte{0xFF, 0xFE, 'a', 0}, 0644)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for UTF-16 config")
	}
	if !strings.Contains(err.Error(), "UTF-8") {
		t.Errorf("expected 'UTF-8' in error, got %q", err.Error())
	}
}

func TestLoadConfig_DefaultsApplied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "minimal.toml")
	os.WriteFile(path, []byte(""), 0644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.Run.InactivityTimeout != DefaultInactivityTimeout {
		t.Errorf("expected default inactivity timeout")
	}
	if cfg.Run.TotalTimeout != DefaultTotalTimeout {
		t.Errorf("expected default total timeout")
	}
	if cfg.Run.MaxOutput != DefaultMaxOutput {
		t.Errorf("expected default max output")
	}
	if cfg.Security.MaxTimeout != DefaultMaxTimeout {
		t.Errorf("expected default max timeout")
	}
}

func TestLoadConfig_InvalidToml(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	os.WriteFile(path, []byte("[broken\nnot valid toml"), 0644)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

// ===== ValidateToolConfigs =====

func TestValidateToolConfigs_Valid(t *testing.T) {
	tools := map[string]ToolConfig{
		"test": {
			Exe: "test.exe",
			Params: map[string]ParamConfig{
				"input": {Flag: "--input"},
				"pos":   {Position: 1},
			},
		},
	}
	err := ValidateToolConfigs(tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateToolConfigs_BothFlagAndPosition(t *testing.T) {
	tools := map[string]ToolConfig{
		"test": {
			Params: map[string]ParamConfig{
				"bad": {Flag: "--bad", Position: 1},
			},
		},
	}
	err := ValidateToolConfigs(tools)
	if err == nil {
		t.Fatal("expected error for both flag and position")
	}
	if !strings.Contains(err.Error(), "both") {
		t.Errorf("expected 'both' in error, got %q", err.Error())
	}
}

func TestValidateToolConfigs_NeitherFlagNorPosition(t *testing.T) {
	tools := map[string]ToolConfig{
		"test": {
			Params: map[string]ParamConfig{
				"bad": {Type: "string"},
			},
		},
	}
	err := ValidateToolConfigs(tools)
	if err == nil {
		t.Fatal("expected error for neither flag nor position")
	}
	if !strings.Contains(err.Error(), "neither") {
		t.Errorf("expected 'neither' in error, got %q", err.Error())
	}
}

// ===== DefaultRunConfig =====

func TestDefaultRunConfig(t *testing.T) {
	cfg := DefaultRunConfig()
	if cfg.InactivityTimeout != DefaultInactivityTimeout {
		t.Errorf("expected %d, got %d", DefaultInactivityTimeout, cfg.InactivityTimeout)
	}
	if cfg.TotalTimeout != DefaultTotalTimeout {
		t.Errorf("expected %d, got %d", DefaultTotalTimeout, cfg.TotalTimeout)
	}
}

// ===== DefaultBuiltinDescriptions =====

func TestDefaultBuiltinDescriptions(t *testing.T) {
	desc := DefaultBuiltinDescriptions()
	if _, ok := desc["read"]; !ok {
		t.Error("expected 'read' in default descriptions")
	}
	if _, ok := desc["grep"]; !ok {
		t.Error("expected 'grep' in default descriptions")
	}
	if len(desc) < 18 {
		t.Errorf("expected at least 18 descriptions, got %d", len(desc))
	}
}

// Suppress unused import
var _ = toml.Decode
