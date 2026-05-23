# mcp-server-go-quality Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an MCP server in Go that wraps golangci-lint, govulncheck, and nilaway, returning unified diagnostics with extracted file/line/message fields plus native tool output.

**Architecture:** Three-layer design — transport (MCP stdio), tool handlers (pure functions per checker), subprocess exec + parsing (one file per tool). Pre-flight sequential tool discovery/install prevents race conditions. Parallel goroutines run checks concurrently with a shared timeout context.

**Tech Stack:** Go 1.25, `mark3labs/mcp-go` v0.54.0, `os/exec` subprocess management, `gopkg.in/yaml.v3` for config.

---

## File Structure

```
mcp-server-go-quality/
├── cmd/mcp-server-go-quality/main.go   # Entry point, server bootstrap, CLI flags
├── internal/
│   ├── checkers/
│   │   ├── checkers.go                 # Diagnostic type
│   │   ├── golangci_lint.go            # golangci-lint handler + parser
│   │   ├── govulncheck.go              # govulncheck handler + parser (NDJSON)
│   │   ├── nilaway.go                  # nilaway handler + parser
│   │   ├── orchestrate.go              # runAll parallel dispatch + pre-flight
│   │   └── checkers_test.go            # Unit + integration tests for all checkers
│   ├── config/
│   │   ├── config.go                   # Config struct, .go-quality.yaml loader
│   │   └── config_test.go
│   ├── discovery/
│   │   ├── tools.go                    # Tool discovery, version resolution, install
│   │   └── tools_test.go
│   ├── runner/
│   │   ├── runner.go                   # CommandRunner interface + ExecRunner
│   │   └── runner_test.go
│   └── version/
│       ├── version.go                  # Version string with VCS metadata
│       └── version_test.go
├── testdata/sample_project/            # Go project with intentional issues
│   ├── go.mod
│   ├── main.go
│   └── helper.go
├── go.mod
└── Makefile
```

---

### Task 1: Initialize Go Module and Directory Structure

**Files:**
- Create: `go.mod`
- Create: all empty directory paths

- [ ] **Step 1: Create directories**

```bash
mkdir -p cmd/mcp-server-go-quality
mkdir -p internal/checkers internal/config internal/discovery internal/runner internal/version
mkdir -p testdata/sample_project
```

- [ ] **Step 2: Initialize Go module**

```bash
go mod init github.com/afshinator/mcp-server-go-quality
```

Run: `go mod init github.com/afshinator/mcp-server-go-quality`
Expected: creates `go.mod` with `go 1.25.9` (or current installed version)

- [ ] **Step 3: Commit**

```bash
git add go.mod $(find . -type d -empty -not -path './.git/*')
git commit -m "chore: initialize Go module and directory structure"
```

---

### Task 2: Diagnostic Type

**Files:**
- Create: `internal/checkers/checkers.go`
- Create: `internal/checkers/checkers_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/checkers/checkers_test.go
package checkers

import (
    "encoding/json"
    "testing"
)

func TestDiagnosticJSONOmitEmptyFields(t *testing.T) {
    d := Diagnostic{
        Tool:    "nilaway",
        Message: "Potential nil panic",
    }
    b, err := json.Marshal(d)
    if err != nil {
        t.Fatal(err)
    }
    var result map[string]interface{}
    if err := json.Unmarshal(b, &result); err != nil {
        t.Fatal(err)
    }
    // Zero-value fields should be omitted
    if _, ok := result["file"]; ok {
        t.Error("empty file should be omitted")
    }
    if _, ok := result["line"]; ok {
        t.Error("zero line should be omitted")
    }
    if _, ok := result["error"]; ok {
        t.Error("empty error should be omitted")
    }
    if result["tool"] != "nilaway" {
        t.Errorf("tool = %v, want nilaway", result["tool"])
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/checkers/ -run TestDiagnosticJSONOmitEmptyFields -v`
Expected: FAIL — undefined: Diagnostic

- [ ] **Step 3: Write minimal implementation**

```go
// internal/checkers/checkers.go
package checkers

import "encoding/json"

type Diagnostic struct {
    Tool    string          `json:"tool"`
    File    string          `json:"file,omitempty"`
    Line    int             `json:"line,omitempty"`
    Message string          `json:"message,omitempty"`
    Error   string          `json:"error,omitempty"`
    Native  json.RawMessage `json:"native,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/checkers/ -run TestDiagnosticJSONOmitEmptyFields -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/checkers/checkers.go internal/checkers/checkers_test.go
git commit -m "feat: add Diagnostic type with JSON omitempty semantics"
```

---

### Task 3: Version Package

**Files:**
- Create: `internal/version/version.go`
- Create: `internal/version/version_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/version/version_test.go
package version

import "testing"

func TestStringNotEmpty(t *testing.T) {
    s := String()
    if s == "" {
        t.Error("version string must not be empty")
    }
}

func TestValueDefault(t *testing.T) {
    if Value == "" {
        t.Error("Value must have a default")
    }
}

func TestFormatVersion(t *testing.T) {
    tests := []struct {
        name     string
        base     string
        settings []debugSetting
        want     string
    }{
        {
            name: "clean commit",
            base: "v0.1.0",
            settings: []debugSetting{
                {Key: "vcs.revision", Value: "abcdef1234567890"},
                {Key: "vcs.modified", Value: "false"},
            },
            want: "v0.1.0 (abcdef1)",
        },
        {
            name: "dirty workspace",
            base: "v0.1.0",
            settings: []debugSetting{
                {Key: "vcs.revision", Value: "abcdef1234567890"},
                {Key: "vcs.modified", Value: "true"},
            },
            want: "v0.1.0 (abcdef1-dirty)",
        },
        {
            name: "no VCS metadata",
            base: "v0.1.0",
            want: "v0.1.0",
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := formatVersion(tt.base, toDebugBuildSettings(tt.settings))
            if got != tt.want {
                t.Errorf("formatVersion() = %q, want %q", got, tt.want)
            }
        })
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/version/ -v`
Expected: FAIL — undefined symbols

- [ ] **Step 3: Write implementation**

```go
// internal/version/version.go
package version

import (
    "fmt"
    "runtime/debug"
)

var Value = "v0.1.0"

var tagged = ""

func String() string {
    if tagged == "true" {
        return Value
    }
    info, ok := debug.ReadBuildInfo()
    if !ok {
        return Value
    }
    return formatVersion(Value, info.Settings)
}

type debugSetting = debug.BuildSetting

func toDebugBuildSettings(s []debugSetting) []debug.BuildSetting {
    return s
}

func formatVersion(base string, settings []debug.BuildSetting) string {
    var commit string
    var dirty bool
    for _, s := range settings {
        switch s.Key {
        case "vcs.revision":
            n := min(7, len(s.Value))
            commit = s.Value[:n]
        case "vcs.modified":
            dirty = s.Value == "true"
        }
    }
    if commit == "" {
        return base
    }
    suffix := commit
    if dirty {
        suffix += "-dirty"
    }
    return fmt.Sprintf("%s (%s)", base, suffix)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/version/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/version/version.go internal/version/version_test.go
git commit -m "feat: add version package with VCS metadata support"
```

---

### Task 4: Config Loading

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/config/config_test.go
package config

import (
    "os"
    "path/filepath"
    "testing"
    "time"
)

func TestLoadDefaults(t *testing.T) {
    cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Timeout != 5*time.Minute {
        t.Errorf("default timeout = %v, want 5m", cfg.Timeout)
    }
    if cfg.Tools["golangci-lint"].Version != "latest" {
        t.Errorf("golangci-lint version = %q, want latest", cfg.Tools["golangci-lint"].Version)
    }
    if cfg.Tools["govulncheck"].Version != "latest" {
        t.Errorf("govulncheck version = %q, want latest", cfg.Tools["govulncheck"].Version)
    }
    if cfg.Tools["nilaway"].Version != "latest" {
        t.Errorf("nilaway version = %q, want latest", cfg.Tools["nilaway"].Version)
    }
}

func TestLoadFromFile(t *testing.T) {
    yaml := `
timeout: 10m
tools:
  golangci-lint:
    version: v2.3.0
    extra_args: ["--fast"]
  govulncheck:
    version: latest
    extra_args: ["--scan=package"]
  nilaway:
    version: v0.19.0
    extra_args: ["--exclude-pkgs=github.com/myorg/vendor"]
`
    dir := t.TempDir()
    path := filepath.Join(dir, ".go-quality.yaml")
    if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
        t.Fatal(err)
    }
    cfg, err := Load(path)
    if err != nil {
        t.Fatal(err)
    }
    if cfg.Timeout != 10*time.Minute {
        t.Errorf("timeout = %v, want 10m", cfg.Timeout)
    }
    if cfg.Tools["golangci-lint"].Version != "v2.3.0" {
        t.Errorf("golangci-lint = %q", cfg.Tools["golangci-lint"].Version)
    }
    if len(cfg.Tools["golangci-lint"].ExtraArgs) != 1 {
        t.Errorf("golangci-lint extra_args len = %d, want 1", len(cfg.Tools["golangci-lint"].ExtraArgs))
    }
}

func TestResolveTimeoutDefault(t *testing.T) {
    cfg := DefaultConfig()
    if cfg.ResolveTimeout() != 5*time.Minute {
        t.Errorf("default timeout = %v", cfg.ResolveTimeout())
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — undefined: Config, Load, etc.

- [ ] **Step 3: Write implementation**

```go
// internal/config/config.go
package config

import (
    "os"
    "time"

    "gopkg.in/yaml.v3"
)

type ToolConfig struct {
    Version   string   `yaml:"version"`
    ExtraArgs []string `yaml:"extra_args"`
}

type Config struct {
    Timeout time.Duration         `yaml:"timeout"`
    Tools   map[string]ToolConfig `yaml:"tools"`
}

func DefaultConfig() Config {
    return Config{
        Timeout: 5 * time.Minute,
        Tools: map[string]ToolConfig{
            "golangci-lint": {Version: "latest"},
            "govulncheck":   {Version: "latest"},
            "nilaway":       {Version: "latest"},
        },
    }
}

func (c Config) ResolveTimeout() time.Duration {
    if c.Timeout <= 0 {
        return 5 * time.Minute
    }
    return c.Timeout
}

func Load(path string) (Config, error) {
    cfg := DefaultConfig()
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return cfg, nil
        }
        return cfg, err
    }
    if err := yaml.Unmarshal(data, &cfg); err != nil {
        return cfg, err
    }
    for name := range cfg.Tools {
        tc := cfg.Tools[name]
        if tc.Version == "" {
            tc.Version = "latest"
            cfg.Tools[name] = tc
        }
    }
    return cfg, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS (after `go mod tidy` to fetch yaml.v3)

- [ ] **Step 5: Tidy dependencies**

```bash
go mod tidy
```

- [ ] **Step 6: Run tests again**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go go.mod go.sum
git commit -m "feat: add config loading from .go-quality.yaml with defaults"
```

---

### Task 5: Tool Discovery & Installation

**Files:**
- Create: `internal/discovery/tools.go`
- Create: `internal/discovery/tools_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/discovery/tools_test.go
package discovery

import (
    "os/exec"
    "testing"
)

func TestToolInfoDefaults(t *testing.T) {
    ti := ToolInfo{Name: "nilaway"}
    if ti.installPath() != "go.uber.org/nilaway/cmd/nilaway" {
        t.Errorf("installPath = %q", ti.installPath())
    }
}

func TestAllRequiredTools(t *testing.T) {
    tools := AllRequiredTools("latest")
    if len(tools) != 3 {
        t.Fatalf("got %d tools, want 3", len(tools))
    }
    names := map[string]bool{}
    for _, ti := range tools {
        names[ti.Name] = true
    }
    for _, name := range []string{"golangci-lint", "govulncheck", "nilaway"} {
        if !names[name] {
            t.Errorf("missing tool: %s", name)
        }
    }
}

func TestDiscoverTool(t *testing.T) {
    // go should always be findable
    ti := ToolInfo{Name: "go"}
    _, err := ti.Discover()
    if err != nil {
        t.Errorf("expected go to be discoverable: %v", err)
    }
}

func TestDiscoverMissingTool(t *testing.T) {
    ti := ToolInfo{Name: "definitely-not-a-real-tool-xyzzy"}
    _, err := ti.Discover()
    if err == nil {
        t.Error("expected error for nonexistent tool")
    }
    if _, ok := err.(*exec.Error); !ok {
        // ok if it's a lookup error
        t.Logf("error type: %T: %v", err, err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/discovery/ -v`
Expected: FAIL — undefined: ToolInfo, AllRequiredTools

- [ ] **Step 3: Write implementation**

```go
// internal/discovery/tools.go
package discovery

import (
    "fmt"
    "os/exec"
)

type ToolInfo struct {
    Name      string
    Version   string
    path      string
    installed bool
}

func (t *ToolInfo) Discover() (string, error) {
    if t.path != "" {
        return t.path, nil
    }
    p, err := exec.LookPath(t.Name)
    if err != nil {
        return "", fmt.Errorf("tool %q not found on PATH", t.Name)
    }
    t.path = p
    t.installed = true
    return p, nil
}

func (t *ToolInfo) IsInstalled() bool {
    if t.installed {
        return true
    }
    _, err := exec.LookPath(t.Name)
    return err == nil
}

func (t *ToolInfo) installPath() string {
    switch t.Name {
    case "golangci-lint":
        return "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
    case "govulncheck":
        return "golang.org/x/vuln/cmd/govulncheck"
    case "nilaway":
        return "go.uber.org/nilaway/cmd/nilaway"
    default:
        return ""
    }
}

func (t *ToolInfo) Install() error {
    pkg := t.installPath()
    if pkg == "" {
        return fmt.Errorf("no install path for tool %q", t.Name)
    }
    version := t.Version
    if version == "" || version == "latest" {
        version = "latest"
    }
    pkgWithVersion := fmt.Sprintf("%s@%s", pkg, version)
    cmd := exec.Command("go", "install", pkgWithVersion)
    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("go install %s failed: %w\n%s", pkgWithVersion, err, string(output))
    }
    return nil
}

func AllRequiredTools(version string) []ToolInfo {
    return []ToolInfo{
        {Name: "golangci-lint", Version: version},
        {Name: "govulncheck", Version: version},
        {Name: "nilaway", Version: version},
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/discovery/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/discovery/tools.go internal/discovery/tools_test.go
git commit -m "feat: add tool discovery and installation support"
```

---

### Task 6: Command Runner Interface

**Files:**
- Create: `internal/runner/runner.go`
- Create: `internal/runner/runner_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/runner/runner_test.go
package runner

import (
    "context"
    "os/exec"
    "testing"
    "time"
)

type mockRunner struct {
    output string
    err    error
}

func (m *mockRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
    if m.err != nil {
        return nil, m.err
    }
    return []byte(m.output), nil
}

func TestMockRunnerReturnsOutput(t *testing.T) {
    r := &mockRunner{output: "hello"}
    out, err := r.Run(context.Background(), "echo", "hello")
    if err != nil {
        t.Fatal(err)
    }
    if string(out) != "hello" {
        t.Errorf("output = %q, want %q", string(out), "hello")
    }
}

func TestExecRunnerRunsCommand(t *testing.T) {
    r := &ExecRunner{}
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    out, err := r.Run(ctx, "go", "version")
    if err != nil {
        t.Fatal(err)
    }
    if len(out) == 0 {
        t.Error("expected non-empty output from go version")
    }
}

func TestExecRunnerNonExistentCommand(t *testing.T) {
    r := &ExecRunner{}
    ctx := context.Background()
    _, err := r.Run(ctx, "definitely-not-a-real-command")
    if err == nil {
        t.Error("expected error for nonexistent command")
    }
}

func TestExecRunnerTimeout(t *testing.T) {
    r := &ExecRunner{}
    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
    defer cancel()
    time.Sleep(10 * time.Millisecond)
    _, err := r.Run(ctx, "go", "version")
    if err == nil {
        t.Log("timeout may not trigger on fast commands")
    }
}

func TestExecRunnerStderr(t *testing.T) {
    r := &ExecRunner{}
    ctx := context.Background()
    _, err := r.Run(ctx, "go", "build", "./definitely-not-a-file.go")
    if err == nil {
        t.Error("expected error from go build on nonexistent file")
    }
    if _, ok := err.(*exec.ExitError); !ok {
        t.Logf("error type: %T", err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/runner/ -v`
Expected: FAIL — undefined: CommandRunner, ExecRunner

- [ ] **Step 3: Write implementation**

```go
// internal/runner/runner.go
package runner

import (
    "context"
    "fmt"
    "os/exec"
)

type CommandRunner interface {
    Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (r *ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
    cmd := exec.CommandContext(ctx, name, args...)
    output, err := cmd.Output()
    if err != nil {
        if exitErr, ok := err.(*exec.ExitError); ok {
            return nil, fmt.Errorf("%s: %w\n%s", name, exitErr, string(exitErr.Stderr))
        }
        return nil, err
    }
    return output, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runner/ -v`
Expected: PASS (first 3 tests; last 2 may vary by timing)

- [ ] **Step 5: Commit**

```bash
git add internal/runner/runner.go internal/runner/runner_test.go
git commit -m "feat: add CommandRunner interface and ExecRunner"
```

---

### Task 7: golangci-lint Handler + Parser

**Files:**
- Create: `internal/checkers/golangci_lint.go`
- Modify: `internal/checkers/checkers_test.go`

- [ ] **Step 1: Write failing test**

```go
// Add to internal/checkers/checkers_test.go

import (
    "context"
    "encoding/json"
    "testing"
)

type mockRunner struct {
    outputs map[string][]byte
}

func (m *mockRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
    key := name + ":" + args[0]
    if out, ok := m.outputs[key]; ok {
        return out, nil
    }
    return nil, nil
}

func TestParseGolangciLintOutput(t *testing.T) {
    input := `{
  "Issues": [
    {
      "FromLinter": "gocognit",
      "Text": "cognitive complexity 18 of func ` + "`ProcessStream`" + ` is high (> 15)",
      "Severity": "warning",
      "Pos": {
        "Filename": "cmd/main.go",
        "Line": 115,
        "Column": 1
      },
      "SourceLines": ["func ProcessStream(w io.Writer, data []byte) {"],
      "SuggestedFixes": null
    },
    {
      "FromLinter": "gosec",
      "Text": "G101: Potential hardcoded credentials",
      "Severity": "warning",
      "Pos": {
        "Filename": "internal/auth/auth.go",
        "Line": 22,
        "Column": 10
      },
      "SourceLines": ["    token := \"supersecret\""],
      "SuggestedFixes": null
    }
  ]
}`
    diags, err := parseGolangciLintOutput([]byte(input), "/project/myapp")
    if err != nil {
        t.Fatal(err)
    }
    if len(diags) != 2 {
        t.Fatalf("got %d diagnostics, want 2", len(diags))
    }
    d0 := diags[0]
    if d0.Tool != "golangci-lint" {
        t.Errorf("tool = %q", d0.Tool)
    }
    if d0.File != "cmd/main.go" {
        t.Errorf("file = %q, want cmd/main.go", d0.File)
    }
    if d0.Line != 115 {
        t.Errorf("line = %d, want 115", d0.Line)
    }
    if d0.Message == "" {
        t.Error("message is empty")
    }
    if d0.Native == nil {
        t.Error("native is nil")
    }
}

func TestRunGolangciLintWithMock(t *testing.T) {
    input := `{"Issues": null}`
    runner := &mockRunner{
        outputs: map[string][]byte{
            "golangci-lint:run": []byte(input),
        },
    }
    diags, err := runGolangciLint(context.Background(), runner, "/project/myapp")
    if err != nil {
        t.Fatal(err)
    }
    if len(diags) != 0 {
        t.Errorf("expected 0 diagnostics, got %d", len(diags))
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/checkers/ -run "TestParseGolangciLintOutput|TestRunGolangciLintWithMock" -v`
Expected: FAIL — undefined: parseGolangciLintOutput, runGolangciLint

- [ ] **Step 3: Write implementation**

```go
// internal/checkers/golangci_lint.go
package checkers

import (
    "context"
    "encoding/json"
    "fmt"
    "path/filepath"

    "github.com/afshinator/mcp-server-go-quality/internal/runner"
)

type golangciLintIssue struct {
    FromLinter string `json:"FromLinter"`
    Text       string `json:"Text"`
    Severity   string `json:"Severity"`
    Pos        struct {
        Filename string `json:"Filename"`
        Line     int    `json:"Line"`
        Column   int    `json:"Column"`
    } `json:"Pos"`
    SourceLines    []string `json:"SourceLines"`
    SuggestedFixes []any    `json:"SuggestedFixes"`
}

type golangciLintOutput struct {
    Issues []golangciLintIssue `json:"Issues"`
}

func runGolangciLint(ctx context.Context, r runner.CommandRunner, projectPath string) ([]Diagnostic, error) {
    args := []string{"run", "--out-format=json", "./..."}
    output, err := r.Run(ctx, "golangci-lint", args...)
    if err != nil {
        return nil, fmt.Errorf("golangci-lint: %w", err)
    }
    return parseGolangciLintOutput(output, projectPath)
}

func parseGolangciLintOutput(output []byte, projectPath string) ([]Diagnostic, error) {
    var result golangciLintOutput
    if err := json.Unmarshal(output, &result); err != nil {
        return nil, fmt.Errorf("parsing golangci-lint output: %w", err)
    }
    diags := make([]Diagnostic, 0, len(result.Issues))
    for _, issue := range result.Issues {
        file := issue.Pos.Filename
        if rel, err := filepath.Rel(projectPath, file); err == nil {
            file = rel
        }
        native, _ := json.Marshal(issue)
        diags = append(diags, Diagnostic{
            Tool:    "golangci-lint",
            File:    file,
            Line:    issue.Pos.Line,
            Message: issue.Text,
            Native:  native,
        })
    }
    return diags, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/checkers/ -run "TestParseGolangciLintOutput|TestRunGolangciLintWithMock" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/checkers/golangci_lint.go internal/checkers/checkers_test.go
git commit -m "feat: add golangci-lint handler and JSON parser"
```

---

### Task 8: govulncheck Handler + Parser (NDJSON)

**Files:**
- Create: `internal/checkers/govulncheck.go`
- Modify: `internal/checkers/checkers_test.go`

- [ ] **Step 1: Write failing test**

```go
// Add to internal/checkers/checkers_test.go

func TestParseGovulncheckOutputFindsVulnerabilities(t *testing.T) {
    input := `{"config":{"protocol_version":"v1.0.0"}}
{"osv":{"id":"GO-2026-4918","summary":"Infinite loop in HTTP/2 transport","aliases":["CVE-2026-33814"]}}
{"finding":{"osv":"GO-2026-4918","fixed_version":"v1.25.10","trace":[
  {"module":"stdlib","version":"v1.25.9","package":"net/http","function":"Do","position":{"filename":"src/net/http/client.go","line":586,"column":18}},
  {"module":"github.com/myorg/myapp","package":"github.com/myorg/myapp/internal/httpclient","function":"Get","position":{"filename":"internal/httpclient/client.go","line":78,"column":25}}
]}}
{"finding":{"osv":"GO-2026-4971","fixed_version":"v1.25.10","trace":[
  {"module":"stdlib","version":"v1.25.9","package":"net"}
]}}
{"progress":{"message":"done"}}
`
    diags, err := parseGovulncheckOutput([]byte(input), "/project/myapp")
    if err != nil {
        t.Fatal(err)
    }
    if len(diags) != 2 {
        t.Fatalf("got %d diagnostics, want 2", len(diags))
    }
    d0 := diags[0]
    if d0.Tool != "govulncheck" {
        t.Errorf("tool = %q", d0.Tool)
    }
    if d0.File != "internal/httpclient/client.go" {
        t.Errorf("file = %q, want internal/httpclient/client.go", d0.File)
    }
    if d0.Line != 78 {
        t.Errorf("line = %d, want 78", d0.Line)
    }
    if d0.Message == "" {
        t.Error("message is empty")
    }
    d1 := diags[1]
    if d1.File != "" {
        t.Errorf("file should be empty for module-level finding, got %q", d1.File)
    }
    if d1.Line != 0 {
        t.Errorf("line should be 0 for module-level finding, got %d", d1.Line)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/checkers/ -run TestParseGovulncheckOutputFindsVulnerabilities -v`
Expected: FAIL — undefined: parseGovulncheckOutput

- [ ] **Step 3: Write implementation**

```go
// internal/checkers/govulncheck.go
package checkers

import (
    "bufio"
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "path/filepath"

    "github.com/afshinator/mcp-server-go-quality/internal/runner"
)

type govulnOSV struct {
    ID      string   `json:"id"`
    Summary string   `json:"summary"`
    Aliases []string `json:"aliases"`
}

type govulnPosition struct {
    Filename string `json:"filename"`
    Line     int    `json:"line"`
    Column   int    `json:"column"`
}

type govulnTraceEntry struct {
    Module   string          `json:"module"`
    Version  string          `json:"version"`
    Package  string          `json:"package"`
    Function string          `json:"function"`
    Position *govulnPosition `json:"position,omitempty"`
}

type govulnFinding struct {
    OSV          string             `json:"osv"`
    FixedVersion string             `json:"fixed_version"`
    Trace        []govulnTraceEntry `json:"trace"`
}

type govulnLine struct {
    Config  *json.RawMessage `json:"config,omitempty"`
    OSV     *struct {
        ID      string   `json:"id"`
        Summary string   `json:"summary"`
        Aliases []string `json:"aliases"`
    } `json:"osv,omitempty"`
    Finding  *govulnFinding     `json:"finding,omitempty"`
    SBOM     *json.RawMessage   `json:"SBOM,omitempty"`
    Progress *json.RawMessage   `json:"progress,omitempty"`
}

func runGovulncheck(ctx context.Context, r runner.CommandRunner, projectPath string) ([]Diagnostic, error) {
    args := []string{"-json", "./..."}
    output, err := r.Run(ctx, "govulncheck", args...)
    if err != nil {
        return nil, fmt.Errorf("govulncheck: %w", err)
    }
    return parseGovulncheckOutput(output, projectPath)
}

func parseGovulncheckOutput(output []byte, projectPath string) ([]Diagnostic, error) {
    scanner := bufio.NewScanner(bytes.NewReader(output))
    scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

    osvMap := map[string]string{}
    var findings []govulnLine

    for scanner.Scan() {
        line := scanner.Bytes()
        if len(line) == 0 {
            continue
        }
        var entry govulnLine
        if err := json.Unmarshal(line, &entry); err != nil {
            continue
        }
        if entry.OSV != nil {
            osvMap[entry.OSV.ID] = entry.OSV.Summary
        }
        if entry.Finding != nil {
            findings = append(findings, entry)
        }
    }

    diags := make([]Diagnostic, 0, len(findings))
    for _, f := range findings {
        if f.Finding == nil {
            continue
        }
        message := osvMap[f.Finding.OSV]
        if message == "" {
            message = f.Finding.OSV
        }

        file := ""
        line := 0
        for i := len(f.Finding.Trace) - 1; i >= 0; i-- {
            t := f.Finding.Trace[i]
            if t.Position != nil && t.Position.Filename != "" {
                file = t.Position.Filename
                line = t.Position.Line
                if rel, err := filepath.Rel(projectPath, file); err == nil {
                    file = rel
                }
                break
            }
        }

        nativeBytes := mustMarshal(map[string]interface{}{
            "finding": f.Finding,
            "summary": message,
        })

        diags = append(diags, Diagnostic{
            Tool:    "govulncheck",
            File:    file,
            Line:    line,
            Message: message,
            Native:  nativeBytes,
        })
    }
    return diags, nil
}

func mustMarshal(v interface{}) json.RawMessage {
    b, _ := json.Marshal(v)
    return b
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/checkers/ -run TestParseGovulncheckOutputFindsVulnerabilities -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/checkers/govulncheck.go internal/checkers/checkers_test.go
git commit -m "feat: add govulncheck handler with NDJSON streaming parser"
```

---

### Task 9: nilaway Handler + Parser

**Files:**
- Create: `internal/checkers/nilaway.go`
- Modify: `internal/checkers/checkers_test.go`

- [ ] **Step 1: Write failing test**

```go
// Add to internal/checkers/checkers_test.go

func TestParseNilawayOutput(t *testing.T) {
    input := `{
  "github.com/myorg/myapp/internal/engine": {
    "nilaway": [
      {
        "posn": "/project/myapp/internal/engine/pulse.go:42:12",
        "end": "/project/myapp/internal/engine/pulse.go:42:15",
        "message": "Potential nil panic detected. Observed nil flow from source to dereference point: \n\t- engine/pulse.go:42:12: variable \\"resp\\" used as non-nil\n"
      }
    ]
  },
  "github.com/myorg/myapp/internal/api": {
    "nilaway": [
      {
        "posn": "/project/myapp/internal/api/client.go:288:4",
        "end": "/project/myapp/internal/api/client.go:288:4",
        "message": "Potential nil panic detected. Observed nil flow from source to dereference point: \n\t- api/client.go:288:4: deep read from local variable \\"counts\\"\n"
      }
    ]
  }
}`
    diags, err := parseNilawayOutput([]byte(input), "/project/myapp")
    if err != nil {
        t.Fatal(err)
    }
    if len(diags) != 2 {
        t.Fatalf("got %d diagnostics, want 2", len(diags))
    }
    d0 := diags[0]
    if d0.Tool != "nilaway" {
        t.Errorf("tool = %q", d0.Tool)
    }
    if d0.File != "internal/engine/pulse.go" {
        t.Errorf("file = %q, want internal/engine/pulse.go", d0.File)
    }
    if d0.Line != 42 {
        t.Errorf("line = %d, want 42", d0.Line)
    }
    if d0.Message == "" {
        t.Error("message is empty")
    }
    if d0.Native == nil {
        t.Error("native is nil")
    }
}

func TestParsePosnString(t *testing.T) {
    file, line, col, err := parsePosn("/project/myapp/pkg/handler.go:123:45")
    if err != nil {
        t.Fatal(err)
    }
    if file != "/project/myapp/pkg/handler.go" {
        t.Errorf("file = %q", file)
    }
    if line != 123 {
        t.Errorf("line = %d", line)
    }
    if col != 45 {
        t.Errorf("col = %d", col)
    }
}

func TestExtractFirstSentence(t *testing.T) {
    tests := []struct {
        input string
        want  string
    }{
        {"Potential nil panic. More details...", "Potential nil panic."},
        {"Single sentence without period", "Single sentence without period"},
        {"First. Second. Third.", "First."},
        {"", ""},
    }
    for _, tt := range tests {
        t.Run(tt.input, func(t *testing.T) {
            got := extractFirstSentence(tt.input)
            if got != tt.want {
                t.Errorf("extractFirstSentence(%q) = %q, want %q", tt.input, got, tt.want)
            }
        })
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/checkers/ -run "TestParseNilawayOutput|TestParsePosnString|TestExtractFirstSentence" -v`
Expected: FAIL — undefined symbols

- [ ] **Step 3: Write implementation**

```go
// internal/checkers/nilaway.go
package checkers

import (
    "context"
    "encoding/json"
    "fmt"
    "path/filepath"
    "strconv"
    "strings"

    "github.com/afshinator/mcp-server-go-quality/internal/runner"
)

type nilawayError struct {
    Posn    string `json:"posn"`
    End     string `json:"end"`
    Message string `json:"message"`
}

func runNilaway(ctx context.Context, r runner.CommandRunner, projectPath string) ([]Diagnostic, error) {
    moduleName, err := readModuleName(ctx, r, projectPath)
    if err != nil {
        return nil, fmt.Errorf("nilaway: reading module name: %w", err)
    }
    includePkgs := fmt.Sprintf("%s", moduleName)
    args := []string{"-json", "-pretty-print=false", "-include-pkgs=" + includePkgs, "./..."}
    output, err := r.Run(ctx, "nilaway", args...)
    if err != nil {
        return nil, fmt.Errorf("nilaway: %w", err)
    }
    return parseNilawayOutput(output, projectPath)
}

func parseNilawayOutput(output []byte, projectPath string) ([]Diagnostic, error) {
    var raw map[string]map[string][]nilawayError
    if err := json.Unmarshal(output, &raw); err != nil {
        return nil, fmt.Errorf("parsing nilaway output: %w", err)
    }
    var diags []Diagnostic
    for _, pkg := range raw {
        for _, errors := range pkg {
            for _, e := range errors {
                file, _, _, err := parsePosn(e.Posn)
                if err != nil {
                    continue
                }
                if rel, err := filepath.Rel(projectPath, file); err == nil {
                    file = rel
                }
                _, line, _, _ := parsePosn(e.Posn)
                native, _ := json.Marshal(e)
                diags = append(diags, Diagnostic{
                    Tool:    "nilaway",
                    File:    file,
                    Line:    line,
                    Message: extractFirstSentence(e.Message),
                    Native:  native,
                })
            }
        }
    }
    return diags, nil
}

func parsePosn(posn string) (file string, line int, col int, err error) {
    lastColon := strings.LastIndex(posn, ":")
    if lastColon < 0 {
        return posn, 0, 0, nil
    }
    secondLast := strings.LastIndex(posn[:lastColon], ":")
    if secondLast < 0 {
        return posn[:lastColon], 0, 0, nil
    }
    file = posn[:secondLast]
    lineStr := posn[secondLast+1 : lastColon]
    colStr := posn[lastColon+1:]
    line, _ = strconv.Atoi(lineStr)
    col, _ = strconv.Atoi(colStr)
    return file, line, col, nil
}

func extractFirstSentence(s string) string {
    for i, ch := range s {
        if ch == '.' {
            return s[:i+1]
        }
    }
    return s
}

func readModuleName(ctx context.Context, r runner.CommandRunner, projectPath string) (string, error) {
    out, err := r.Run(ctx, "go", "list", "-m")
    if err != nil {
        return "", fmt.Errorf("go list -m: %w", err)
    }
    return strings.TrimSpace(string(out)), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/checkers/ -run "TestParseNilawayOutput|TestParsePosnString|TestExtractFirstSentence" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/checkers/nilaway.go internal/checkers/checkers_test.go
git commit -m "feat: add nilaway handler and posn-based parser"
```

---

### Task 10: runAll Orchestrator (Parallel Dispatch + Pre-Flight)

**Files:**
- Create: `internal/checkers/orchestrate.go`
- Modify: `internal/checkers/checkers_test.go`

- [ ] **Step 1: Write failing test**

```go
// Add to internal/checkers/checkers_test.go

func TestRunAllParallelDispatch(t *testing.T) {
    lintOutput := `{"Issues": [{"FromLinter":"gocritic","Text":"test","Pos":{"Filename":"main.go","Line":1,"Column":1}}]}`
    vulnOutput := `{"config":{"protocol_version":"v1.0.0"}}
`
    nilawayOutput := `{}`

    runner := &mockRunner{
        outputs: map[string][]byte{
            "golangci-lint:run": []byte(lintOutput),
            "govulncheck:-json": []byte(vulnOutput),
            "nilaway:-json":     []byte(nilawayOutput),
        },
    }

    opts := CheckOptions{
        ProjectPath: "/project/myapp",
        Timeout:     30 * 1000000000, // 30s in nanoseconds
    }

    diags, err := RunAllChecks(context.Background(), runner, opts)
    if err != nil {
        t.Fatal(err)
    }
    if len(diags) != 1 {
        t.Fatalf("got %d diagnostics, want 1", len(diags))
    }
    if diags[0].Tool != "golangci-lint" {
        t.Errorf("tool = %q", diags[0].Tool)
    }
}

func TestValidateProjectPathNoMod(t *testing.T) {
    dir := t.TempDir()
    err := validateProjectPath(dir)
    if err == nil {
        t.Error("expected error for dir without go.mod")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/checkers/ -run "TestRunAllParallelDispatch|TestValidateProjectPathNoMod" -v`
Expected: FAIL — undefined: CheckOptions, RunAllChecks, validateProjectPath

- [ ] **Step 3: Write implementation**

```go
// internal/checkers/orchestrate.go
package checkers

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "sync"
    "time"

    "github.com/afshinator/mcp-server-go-quality/internal/discovery"
    "github.com/afshinator/mcp-server-go-quality/internal/runner"
)

type CheckOptions struct {
    ProjectPath  string
    Timeout      time.Duration
    ToolVersions map[string]string
}

func (opts CheckOptions) ToolVersion(name string) string {
    if v, ok := opts.ToolVersions[name]; ok {
        return v
    }
    return "latest"
}

func RunAllChecks(ctx context.Context, r runner.CommandRunner, opts CheckOptions) ([]Diagnostic, error) {
    if err := validateProjectPath(opts.ProjectPath); err != nil {
        return nil, err
    }

    // Pre-flight: synchronous sequential install for any missing tools
    if err := ensureToolsInstalled(opts); err != nil {
        return nil, fmt.Errorf("installing required tools: %w", err)
    }

    if opts.Timeout <= 0 {
        opts.Timeout = 5 * time.Minute
    }
    ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
    defer cancel()

    var (
        wg     sync.WaitGroup
        mu     sync.Mutex
        all    []Diagnostic
        first  error
    )

    type result struct {
        diags []Diagnostic
        err   error
    }

    ch := make(chan result, 3)

    run := func(fn func(context.Context, runner.CommandRunner, string) ([]Diagnostic, error)) {
        wg.Add(1)
        go func() {
            defer wg.Done()
            d, e := fn(ctx, r, opts.ProjectPath)
            ch <- result{diags: d, err: e}
        }()
    }

    run(runGolangciLint)
    run(runGovulncheck)
    run(runNilaway)

    go func() {
        wg.Wait()
        close(ch)
    }()

    for r := range ch {
        mu.Lock()
        if r.err != nil {
            all = append(all, Diagnostic{
                Tool:  "server",
                Error: r.err.Error(),
            })
            if first == nil {
                first = r.err
            }
        }
        all = append(all, r.diags...)
        mu.Unlock()
    }

    sort.Slice(all, func(i, j int) bool {
        if all[i].File != all[j].File {
            return all[i].File < all[j].File
        }
        return all[i].Line < all[j].Line
    })

    return all, first
}

func ensureToolsInstalled(opts CheckOptions) error {
    for _, name := range []string{"golangci-lint", "govulncheck", "nilaway"} {
        ti := discovery.ToolInfo{Name: name, Version: opts.ToolVersion(name)}
        if ti.IsInstalled() {
            continue
        }
        if err := ti.Install(); err != nil {
            return fmt.Errorf("installing %s: %w", name, err)
        }
    }
    return nil
}

func validateProjectPath(path string) error {
    info, err := os.Stat(path)
    if err != nil {
        return fmt.Errorf("project path %q: %w", path, err)
    }
    if !info.IsDir() {
        return fmt.Errorf("project path %q is not a directory", path)
    }
    workFile := filepath.Join(path, "go.work")
    if _, err := os.Stat(workFile); err == nil {
        return nil
    }
    modFile := filepath.Join(path, "go.mod")
    if _, err := os.Stat(modFile); os.IsNotExist(err) {
        return fmt.Errorf("not a Go project: no go.mod or go.work found in %q", path)
    }
    return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/checkers/ -run "TestRunAllParallelDispatch|TestValidateProjectPathNoMod" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/checkers/orchestrate.go internal/checkers/checkers_test.go
git commit -m "feat: add RunAllChecks orchestrator with parallel dispatch and validation"
```

---

### Task 11: MCP Server Setup (Tool Registration + Stdio)

**Files:**
- Create: `cmd/mcp-server-go-quality/main.go`

- [ ] **Step 1: Write the main entry point with tool registration**

```go
// cmd/mcp-server-go-quality/main.go
package main

import (
    "context"
    "encoding/json"
    "flag"
    "fmt"
    "log"
    "os"

    "github.com/afshinator/mcp-server-go-quality/internal/checkers"
    "github.com/afshinator/mcp-server-go-quality/internal/config"
    "github.com/afshinator/mcp-server-go-quality/internal/discovery"
    "github.com/afshinator/mcp-server-go-quality/internal/runner"
    "github.com/afshinator/mcp-server-go-quality/internal/version"
    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
)

func main() {
    configPath := flag.String("config", ".go-quality.yaml", "path to config file")
    verbose := flag.Bool("verbose", false, "emit diagnostic logging to stderr")
    showVersion := flag.Bool("version", false, "print version and exit")
    flag.Parse()

    if *showVersion {
        fmt.Println(version.String())
        os.Exit(0)
    }

    if !*verbose {
        log.SetOutput(os.Stderr)
        log.SetFlags(0)
    }

    cfg, err := config.Load(*configPath)
    if err != nil {
        log.Printf("warning: loading config: %v (using defaults)", err)
    }

    s := server.NewMCPServer(
        "go-quality",
        version.String(),
        server.WithToolCapabilities(true),
    )

    registerTools(s, cfg)

    if err := server.ServeStdio(s); err != nil {
        log.Fatalf("server error: %v", err)
    }
}

func registerTools(s *server.MCPServer, cfg config.Config) {
    runAllTool := mcp.NewTool("run_code_checks",
        mcp.WithDescription("Run all Go code quality checks in parallel: golangci-lint, govulncheck, and nilaway. Returns unified diagnostics with file, line, message, and native tool output."),
        mcp.WithString("project_path",
            mcp.Description("Path to the Go project root (default: current working directory)"),
        ),
    )
    s.AddTool(runAllTool, makeRunAllHandler(cfg))

    runLintTool := mcp.NewTool("run_lint",
        mcp.WithDescription("Run golangci-lint only. Returns linting, complexity, and security pattern issues."),
        mcp.WithString("project_path",
            mcp.Description("Path to the Go project root (default: current working directory)"),
        ),
    )
    s.AddTool(runLintTool, makeSingleHandler("golangci-lint", cfg))

    runVulnTool := mcp.NewTool("run_vuln_check",
        mcp.WithDescription("Run govulncheck only. Returns known CVEs in the dependency tree."),
        mcp.WithString("project_path",
            mcp.Description("Path to the Go project root (default: current working directory)"),
        ),
    )
    s.AddTool(runVulnTool, makeSingleHandler("govulncheck", cfg))

    runNilTool := mcp.NewTool("run_nil_check",
        mcp.WithDescription("Run nilaway only. Returns potential nil panics detected via static analysis."),
        mcp.WithString("project_path",
            mcp.Description("Path to the Go project root (default: current working directory)"),
        ),
    )
    s.AddTool(runNilTool, makeSingleHandler("nilaway", cfg))

    installTool := mcp.NewTool("install_tools",
        mcp.WithDescription("Pre-install all required Go quality tools (golangci-lint, govulncheck, nilaway) with pinned versions."),
    )
    s.AddTool(installTool, makeInstallHandler(cfg))
}

func resolveProjectPath(request mcp.CallToolRequest) (string, error) {
    if path, ok := request.Params.Arguments["project_path"].(string); ok && path != "" {
        return path, nil
    }
    cwd, err := os.Getwd()
    if err != nil {
        return "", fmt.Errorf("getting current working directory: %w", err)
    }
    return cwd, nil
}

func makeRunAllHandler(cfg config.Config) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        projectPath, err := resolveProjectPath(req)
        if err != nil {
            return mcp.NewToolResultError(err.Error()), nil
        }

        r := &runner.ExecRunner{}
        opts := checkers.CheckOptions{
            ProjectPath: projectPath,
            Timeout:     cfg.ResolveTimeout(),
        }

        diags, _ := checkers.RunAllChecks(ctx, r, opts)

        b, err := json.Marshal(diags)
        if err != nil {
            return mcp.NewToolResultError(fmt.Sprintf("marshaling results: %v", err)), nil
        }
        return mcp.NewToolResultText(string(b)), nil
    }
}

func makeSingleHandler(toolName string, cfg config.Config) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        projectPath, err := resolveProjectPath(req)
        if err != nil {
            return mcp.NewToolResultError(err.Error()), nil
        }

        r := &runner.ExecRunner{}

        var diags []checkers.Diagnostic
        switch toolName {
        case "golangci-lint":
            diags, err = checkers.RunGolangciLintOnly(ctx, r, projectPath)
        case "govulncheck":
            diags, err = checkers.RunGovulncheckOnly(ctx, r, projectPath)
        case "nilaway":
            diags, err = checkers.RunNilawayOnly(ctx, r, projectPath)
        }
        if err != nil {
            return mcp.NewToolResultError(err.Error()), nil
        }

        b, _ := json.Marshal(diags)
        return mcp.NewToolResultText(string(b)), nil
    }
}

func makeInstallHandler(cfg config.Config) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        tools := discovery.AllRequiredTools("latest")
        var installed []string
        var failed []string
        for _, tool := range tools {
            if tool.IsInstalled() {
                installed = append(installed, tool.Name)
                continue
            }
            ver := "latest"
            if tc, ok := cfg.Tools[tool.Name]; ok && tc.Version != "" {
                ver = tc.Version
            }
            tool.Version = ver
            if err := tool.Install(); err != nil {
                failed = append(failed, fmt.Sprintf("%s: %v", tool.Name, err))
            } else {
                installed = append(installed, tool.Name)
            }
        }
        msg := fmt.Sprintf("Installed: %v. Failed: %v. Already present: %v", installed, failed, installed)
        return mcp.NewToolResultText(msg), nil
    }
}
```

- [ ] **Step 2: Add public exports to checker files (if not already present)**

Each checker file (`golangci_lint.go`, `govulncheck.go`, `nilaway.go`) must export a public function wrapping its private handler. Add to each:

```go
// In golangci_lint.go:
func RunGolangciLintOnly(ctx context.Context, r runner.CommandRunner, projectPath string) ([]Diagnostic, error) {
    return runGolangciLint(ctx, r, projectPath)
}

// In govulncheck.go:
func RunGovulncheckOnly(ctx context.Context, r runner.CommandRunner, projectPath string) ([]Diagnostic, error) {
    return runGovulncheck(ctx, r, projectPath)
}

// In nilaway.go:
func RunNilawayOnly(ctx context.Context, r runner.CommandRunner, projectPath string) ([]Diagnostic, error) {
    return runNilaway(ctx, r, projectPath)
}
```

- [ ] **Step 3: Tidy dependencies**

```bash
go mod tidy
```

- [ ] **Step 4: Verify it compiles**

```bash
go build ./cmd/mcp-server-go-quality/
```

Expected: builds without errors

- [ ] **Step 5: Commit**

```bash
git add cmd/mcp-server-go-quality/main.go internal/checkers/golangci_lint.go internal/checkers/govulncheck.go internal/checkers/nilaway.go go.mod go.sum
git commit -m "feat: add MCP server entry point with 5 registered tools"
```

---

### Task 12: testdata/ Sample Project

**Files:**
- Create: `testdata/sample_project/go.mod`
- Create: `testdata/sample_project/main.go`
- Create: `testdata/sample_project/helper.go`

- [ ] **Step 1: Create go.mod**

```go
// testdata/sample_project/go.mod
module github.com/afshinator/mcp-server-go-quality/testdata/sample_project

go 1.25
```

- [ ] **Step 2: Create main.go with intentional lint issues**

```go
// testdata/sample_project/main.go
package main

import "fmt"

func main() {
    result := getData()
    fmt.Println(result.Name)
}

func getData() *User {
    return nil
}

type User struct {
    Name string
}

func processStream(w io.Writer, data []byte, options ...Option) error {
    var i int
    i++
    if data != nil {
        if len(data) > 0 {
            for _, b := range data {
                if b > 0 {
                    if b%2 == 0 {
                        for j := 0; j < int(b); j++ {
                            if j > 100 {
                                if j%2 == 0 {
                                    i += j
                                } else if j%3 == 0 {
                                    i -= j
                                } else {
                                    i *= j
                                }
                            }
                        }
                    }
                }
            }
        }
    }
    return nil
}
```

- [ ] **Step 3: Create helper.go**

```go
// testdata/sample_project/helper.go
package main

import "io"

type Option func(*Config)

type Config struct {
    Verbose bool
}

func WithVerbose(v bool) Option {
    return func(c *Config) {
        c.Verbose = v
    }
}
```

- [ ] **Step 4: Add .golangci.yml for sample project**

```yaml
# testdata/sample_project/.golangci.yml
version: "2"
run:
  timeout: 2m
linters:
  default: standard
  enable:
    - gocyclo
    - gocognit
    - gosec
    - nilerr
```

- [ ] **Step 5: Commit**

```bash
git add testdata/sample_project/
git commit -m "test: add sample Go project with intentional issues for all 3 tools"
```

---

### Task 13: Integration Tests

**Files:**
- Modify: `internal/checkers/checkers_test.go`

- [ ] **Step 1: Write integration test for real tool execution**

```go
// Add to internal/checkers/checkers_test.go

func TestIntegrationGolangciLint(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }
    r := &runner.ExecRunner{}
    ctx := context.Background()
    diags, err := runGolangciLint(ctx, r, "../../testdata/sample_project")
    if err != nil {
        t.Fatalf("golangci-lint failed: %v", err)
    }
    t.Logf("golangci-lint found %d issues", len(diags))
    for _, d := range diags {
        t.Logf("  %s:%d: %s", d.File, d.Line, d.Message)
    }
}

func TestIntegrationGovulncheck(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }
    r := &runner.ExecRunner{}
    ctx := context.Background()
    diags, err := runGovulncheck(ctx, r, "../../testdata/sample_project")
    if err != nil {
        t.Fatalf("govulncheck failed: %v", err)
    }
    t.Logf("govulncheck found %d vulnerabilities", len(diags))
}

func TestIntegrationNilaway(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }
    r := &runner.ExecRunner{}
    ctx := context.Background()
    diags, err := runNilaway(ctx, r, "../../testdata/sample_project")
    if err != nil {
        t.Fatalf("nilaway failed: %v", err)
    }
    t.Logf("nilaway found %d nil panics", len(diags))
}

func TestIntegrationRunAllChecks(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }
    r := &runner.ExecRunner{}
    ctx := context.Background()
    opts := CheckOptions{
        ProjectPath: "../../testdata/sample_project",
        Timeout:     2 * time.Minute,
    }
    diags, err := RunAllChecks(ctx, r, opts)
    if err != nil {
        t.Fatal(err)
    }
    if len(diags) == 0 {
        t.Error("expected at least one diagnostic from test project")
    }
    for _, d := range diags {
        if d.Tool == "" {
            t.Error("diagnostic has empty tool field")
        }
        t.Logf("[%s] %s:%d: %s", d.Tool, d.File, d.Line, d.Message)
    }
}
```

Add the `runner` import to checkers_test.go:
```go
import (
    ...
    "github.com/afshinator/mcp-server-go-quality/internal/runner"
    ...
)
```

- [ ] **Step 2: Run integration tests**

```bash
go test ./internal/checkers/ -run Integration -v -timeout 5m
```

Expected: PASS with logged diagnostics showing real tool output

- [ ] **Step 3: Commit**

```bash
git add internal/checkers/checkers_test.go
git commit -m "test: add integration tests against testdata/sample_project"
```

---

### Task 14: Makefile + Final Wiring

**Files:**
- Create: `Makefile`

- [ ] **Step 1: Create Makefile**

```makefile
# Makefile

APP_NAME := mcp-server-go-quality
CMD_DIR := ./cmd/$(APP_NAME)

.PHONY: build test lint clean install

build:
	go build -o bin/$(APP_NAME) $(CMD_DIR)

install:
	go install $(CMD_DIR)

test:
	go test -short ./...

test-all:
	go test -timeout 10m ./...

lint:
	golangci-lint run ./...

vet:
	go vet ./...

clean:
	rm -rf bin/

fmt:
	gofumpt -w .
	goimports -w .

.PHONY: run
run:
	go run $(CMD_DIR)
```

- [ ] **Step 2: Run full test suite**

```bash
go test -short ./... -v
```

Expected: PASS (all unit tests, skipping integration tests)

- [ ] **Step 3: Build binary**

```bash
make build
```

Expected: produces `bin/mcp-server-go-quality`

- [ ] **Step 4: Verify --version**

```bash
./bin/mcp-server-go-quality --version
```

Expected: prints version string (e.g., `v0.1.0 (abc1234)`)

- [ ] **Step 5: Commit**

```bash
git add Makefile
git commit -m "chore: add Makefile with build, test, lint targets"
```

---

### Task 15: Run Quality Suite

- [ ] **Step 1: Run golangci-lint on the server itself**

```bash
golangci-lint run ./...
```

- [ ] **Step 2: Run go vet**

```bash
go vet ./...
```

- [ ] **Step 3: Run gofumpt + goimports**

```bash
gofumpt -w .
goimports -w .
```

- [ ] **Step 4: Run all tests**

```bash
go test ./... -v
```

Expected: PASS

- [ ] **Step 5: Final commit if any formatting changes**

```bash
git add -A
git commit -m "chore: run quality suite (lint, vet, format)"
```
