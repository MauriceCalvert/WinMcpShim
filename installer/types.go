package installer

// CheckStatus is the result of a single discovery check.
type CheckStatus int

const (
	StatusOK   CheckStatus = iota
	StatusWarn
	StatusFail
)

// CheckResult holds the outcome of one discovery check.
type CheckResult struct {
	Req    string      // requirement ID, e.g. "INS-01"
	Name   string      // human-readable check name
	Status CheckStatus
	Detail string      // explanation (always populated)
}

// ConfigAction describes what UpdateClaudeConfig decided.
type ConfigAction int

const (
	ActionAdded   ConfigAction = iota
	ActionUpdated
	ActionSkipped
)

// TomlState classifies the state of shim.toml.
type TomlState int

const (
	TomlMissing      TomlState = iota
	TomlUnconfigured           // exists, contains CHANGE_ME
	TomlConfigured             // exists, no CHANGE_ME
)

// ProcessInfo holds a discovered process.
type ProcessInfo struct {
	PID  uint32
	Name string
}

// Plan holds everything discovered in Pass 1 plus
// inputs collected in Pass 2.
type Plan struct {
	ShimDir      string        // directory containing install.exe
	ShimExe      string        // full path to winmcpshim.exe
	GitRoot      string        // Git for Windows root (empty if not found)
	GitUsrBin    string        // gitRoot\usr\bin
	ClaudeDir    string        // %APPDATA%\Claude
	ConfigPath   string        // claude_desktop_config.json full path
	TomlPath     string        // shim.toml full path
	TomlState    TomlState     // Missing, Unconfigured, Configured
	ConfigExists bool
	ConfigAction ConfigAction  // what needs doing to Claude config
	AllowedRoots []string      // validated paths (empty if not needed)
	LogDir       string        // validated log directory path
	TarAvailable bool
	Checks       []CheckResult
}

// MinWindowsBuild is the minimum Windows 10 build number (RTM).
const MinWindowsBuild = 10240

// RequiredGitTools lists executables that must be present in Git's usr\bin.
var RequiredGitTools = []string{
	"grep.exe",
	"gawk.exe",
	"sed.exe",
	"sort.exe",
	"xxd.exe",
	"dos2unix.exe",
	"cut.exe",
	"uniq.exe",
}
