# mcp-server-go-quality v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an MCP server in Go that wraps golangci-lint, govulncheck, and nilaway, returning unified structured `Diagnostic` JSON with extracted file/line/column/severity fields plus native tool output. Supports version-pinned auto-install, multi-module Go workspaces, per-tool independent timeouts, and standardized error reporting.

**Architecture:** Three-layer design — transport (MCP stdio, 5 registered tools), tool handlers (pure functions per checker with `CommandRunner` dependency), subprocess exec + parsing (one file per tool). ExecRunner carries `Dir` for project root isolation. Version-aware cache with `sync.RWMutex` double-check pattern prevents redundant installs. Cancel-only parent context with independent per-tool timeouts enables concurrent execution without shared deadlines.

**Tech Stack:** Go 1.25, `mark3labs/mcp-go` for MCP server, `gopkg.in/yaml.v3` for config, `os/exec` for subprocess management, `bufio.Scanner` for NDJSON parsing.

---

## File Structure

```
mcp-server-go-quality/
├── cmd/mcp-server-go-quality/main.go          # Entry point, MCP server bootstrap, CLI flags
├── internal/
│   ├── diagnostic/
│   │   ├── diagnostic.go                      # Diagnostic struct with JSON tags
│   │   └── diagnostic_test.go
│   ├── version/
│   │   ├── version.go                         # Version string with VCS metadata
│   │   └── version_test.go
│   ├── pathutil/
│   │   ├── pathutil.go                        # filepath.Rel normalization with fallbacks
│   │   └── pathutil_test.go
│   ├── runner/
│   │   ├── runner.go                          # CommandRunner interface + ExecRunner (Dir field)
│   │   └── runner_test.go
│   ├── root/
│   │   ├── root.go                            # Two-pass root discovery (go.work → go.mod)
│   │   └── root_test.go
│   ├── config/
│   │   ├── config.go                          # YAML loader, extra_args validation, defaults
│   │   └── config_test.go
│   ├── discover/
│   │   ├── discover.go                        # Binary dir resolution, version-aware cache, install
│   │   └── discover_test.go
│   ├── checkers/
│   │   ├── checker.go                         # Checker interface + runResult type
│   │   ├── golangci_lint.go                   # golangci-lint handler + parser
│   │   ├── golangci_lint_test.go
│   │   ├── govulncheck.go                     # govulncheck handler + NDJSON parser + DB lock retry
│   │   ├── govulncheck_test.go
│   │   ├── nilaway.go                         # nilaway handler + parser + workspace module resolution
│   │   ├── nilaway_test.go
│   │   ├── orchestrator.go                    # runAll parallel dispatch + pre-flight + panic recovery
│   │   └── orchestrator_test.go
│   └── toolname/
│       ├── toolname.go                        # Valid tool names constants and validation
│       └── toolname_test.go
├── testdata/
│   └── sample_project/
│       ├── go.mod                             # Module with pinned vulnerable dependency
│       ├── go.sum
│       ├── .golangci.yml                      # Enables gocyclo, gocognit, gosec
│       ├── main.go                            # High complexity function + nil deref path
│       └── helpers.go                         # Helper types used by main.go
├── AGENTS.md                                   # Agent usage guide
├── Makefile
├── go.mod
└── go.sum
```

---

### Task 1: Initialize Go Module and Directory Structure

**Files:**
- Create: `go.mod`
- Create: all empty directory paths

- [ ] **Step 1: Create directories**

```bash
mkdir -p cmd/mcp-server-go-quality
mkdir -p internal/diagnostic internal/version internal/pathutil internal/runner
mkdir -p internal/root internal/config internal/discover internal/checkers internal/toolname
mkdir -p testdata/sample_project
```

- [ ] **Step 2: Initialize Go module**

```bash
go mod init github.com/afshinator/mcp-server-go-quality
```

Run: `go mod init github.com/afshinator/mcp-server-go-quality`
Expected: creates `go.mod` with `go 1.25.x`

- [ ] **Step 3: Commit**

```bash
git add go.mod && find . -type d -empty -not -path './.git/*' -not -path './.superpowers/*' -not -path './.commandcode/*' | while read d; do touch "$d/.gitkeep"; done
git add $(find . -name .gitkeep)
git commit -m "chore: initialize Go module and directory structure"
```

---

### Task 2: Diagnostic Type

**Files:**
- Create: `internal/diagnostic/diagnostic.go`
- Create: `internal/diagnostic/diagnostic_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/diagnostic/diagnostic_test.go
package diagnostic

import (
	"encoding/json"
	"testing"
)

func TestDiagnosticJSONMarshalling(t *testing.T) {
	t.Run("full diagnostic with all fields", func(t *testing.T) {
		d := Diagnostic{
			Tool:     "golangci-lint",
			File:     "cmd/main.go",
			Line:     115,
			Column:   1,
			Severity: "warning",
			Message:  "cognitive complexity 18 is high (> 15)",
			Native:   json.RawMessage(`{"FromLinter":"gocognit"}`),
		}
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]interface{}
		if err := json.Unmarshal(b, &result); err != nil {
			t.Fatal(err)
		}
		if result["tool"] != "golangci-lint" {
			t.Errorf("tool = %v", result["tool"])
		}
		if result["column"] != float64(1) {
			t.Errorf("column = %v", result["column"])
		}
		if result["native"] == nil {
			t.Error("native should not be null when populated")
		}
	})

	t.Run("zero-value fields serialize to zero", func(t *testing.T) {
		d := Diagnostic{
			Tool:    "nilaway",
			Message: "Potential nil panic detected",
		}
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]interface{}
		if err := json.Unmarshal(b, &result); err != nil {
			t.Fatal(err)
		}
		// File and Line: always present per spec (no omitempty)
		if file, ok := result["file"]; !ok || file != "" {
			t.Errorf("file should be present as \"\", got %v (%v)", file, ok)
		}
		if line, ok := result["line"]; !ok || line != float64(0) {
			t.Errorf("line should be present as 0, got %v (%v)", line, ok)
		}
		// Error: always present, empty string on success
		if errVal, ok := result["error"]; !ok || errVal != "" {
			t.Errorf("error should be present as \"\", got %v (%v)", errVal, ok)
		}
		// Column and Severity: omitempty removes them when zero-valued
		if _, ok := result["column"]; ok {
			t.Error("zero column should be omitted")
		}
		if _, ok := result["severity"]; ok {
			t.Error("empty severity should be omitted")
		}
	})

	t.Run("error diagnostic has zero-valued location and message fields", func(t *testing.T) {
		d := Diagnostic{
			Tool:  "govulncheck",
			Error: "timed out after 5m0s",
		}
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]interface{}
		if err := json.Unmarshal(b, &result); err != nil {
			t.Fatal(err)
		}
		if result["error"] != "timed out after 5m0s" {
			t.Errorf("error = %v", result["error"])
		}
		// File, Line, Message are present with zero values (no omitempty on these fields)
		if file, ok := result["file"]; !ok || file != "" {
			t.Errorf("file should be present as \"\", got %v", file)
		}
		if line, ok := result["line"]; !ok || line != float64(0) {
			t.Errorf("line should be present as 0, got %v", line)
		}
		if msg, ok := result["message"]; !ok || msg != "" {
			t.Errorf("message should be present as \"\", got %v", msg)
		}
	})

	t.Run("native is null when zero-valued", func(t *testing.T) {
		d := Diagnostic{
			Tool:  "nilaway",
			Error: "install failed: go install ...",
		}
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]interface{}
		if err := json.Unmarshal(b, &result); err != nil {
			t.Fatal(err)
		}
		nativeVal, ok := result["native"]
		if !ok {
			t.Error("native field should always be present per spec (no omitempty)")
		}
		if nativeVal != nil {
			t.Errorf("native should be null when zero-valued, got %v", nativeVal)
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/diagnostic/ -v`
Expected: FAIL — undefined: Diagnostic

- [ ] **Step 3: Write minimal implementation**

```go
// internal/diagnostic/diagnostic.go
package diagnostic

import "encoding/json"

type Diagnostic struct {
	Tool     string          `json:"tool"`
	File     string          `json:"file"`
	Line     int             `json:"line"`
	Column   int             `json:"column,omitempty"`
	Severity string          `json:"severity,omitempty"`
	Message  string          `json:"message"`
	Error    string          `json:"error"`
	Native   json.RawMessage `json:"native"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/diagnostic/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/diagnostic/diagnostic.go internal/diagnostic/diagnostic_test.go
git commit -m "feat: add Diagnostic type with correct JSON omitempty semantics"
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

import (
	"runtime/debug"
	"testing"
)

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
		settings []debug.BuildSetting
		want     string
	}{
		{
			name: "clean commit",
			base: "v0.1.0",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abcdef1234567890"},
				{Key: "vcs.modified", Value: "false"},
			},
			want: "v0.1.0 (abcdef1)",
		},
		{
			name: "dirty workspace",
			base: "v0.1.0",
			settings: []debug.BuildSetting{
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
		{
			name: "short commit hash",
			base: "v0.1.0",
			settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc"},
				{Key: "vcs.modified", Value: "false"},
			},
			want: "v0.1.0 (abc)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatVersion(tt.base, tt.settings)
			if got != tt.want {
				t.Errorf("formatVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/version/ -v`
Expected: FAIL — undefined: String, Value, formatVersion

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

### Task 4: Path Utilities

**Files:**
- Create: `internal/pathutil/pathutil.go`
- Create: `internal/pathutil/pathutil_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/pathutil/pathutil_test.go
package pathutil

import "testing"

func TestRel(t *testing.T) {
	projectRoot := "/project/myapp"

	tests := []struct {
		name         string
		absPath      string
		want         string
		wantFallback bool
	}{
		{
			name:    "simple relative",
			absPath: "/project/myapp/cmd/main.go",
			want:    "cmd/main.go",
		},
		{
			name:    "deep relative",
			absPath: "/project/myapp/internal/auth/auth.go",
			want:    "internal/auth/auth.go",
		},
		{
			name:    "already relative path",
			absPath: "cmd/main.go",
			want:    "cmd/main.go",
		},
		{
			name:    "different root (falls back to trimprefix)",
			absPath: "/other/project/file.go",
			want:    "/other/project/file.go",
		},
		{
			name:    "empty path",
			absPath: "",
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Rel(projectRoot, tt.absPath)
			if got != tt.want {
				t.Errorf("Rel(%q, %q) = %q, want %q", projectRoot, tt.absPath, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pathutil/ -v`
Expected: FAIL — undefined: Rel

- [ ] **Step 3: Write implementation**

```go
// internal/pathutil/pathutil.go
package pathutil

import (
	"path/filepath"
	"strings"
)

func Rel(projectRoot, inputPath string) string {
	if inputPath == "" {
		return ""
	}

	cleaned := filepath.Clean(inputPath)

	if !filepath.IsAbs(cleaned) {
		return cleaned
	}

	rel, err := filepath.Rel(projectRoot, cleaned)
	if err == nil {
		rel = filepath.Clean(rel)

		// Reject escape-traversal paths (e.g. "../../../other/file.go").
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}

	// Fallback: try stripping the project root prefix.
	cleanRoot := filepath.Clean(projectRoot)
	if trimmed := strings.TrimPrefix(cleaned, cleanRoot); trimmed != cleaned {
		return strings.TrimPrefix(trimmed, string(filepath.Separator))
	}

	// Preserve the original absolute path.
	return cleaned
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pathutil/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/pathutil/pathutil.go internal/pathutil/pathutil_test.go
git commit -m "feat: add path normalization utility with filepath.Rel fallbacks"
```

---

### Task 5: CommandRunner Interface + ExecRunner

**Files:**
- Create: `internal/runner/runner.go`
- Create: `internal/runner/runner_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/runner/runner_test.go
package runner

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

type mockRunner struct {
	output []byte
	err    error
}

func (m *mockRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.output, nil
}

func TestMockRunner(t *testing.T) {
	r := &mockRunner{output: []byte("hello")}
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

func TestExecRunnerDir(t *testing.T) {
	dir := t.TempDir()
	r := &ExecRunner{Dir: dir}
	ctx := context.Background()
	out, err := r.Run(ctx, "go", "env", "GOMOD")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("GOMOD from %s: %s", dir, out)
}

func TestExecRunnerNonExistentCommand(t *testing.T) {
	r := &ExecRunner{}
	ctx := context.Background()
	_, err := r.Run(ctx, "definitely-not-a-real-command-xyzzy")
	if err == nil {
		t.Error("expected error for nonexistent command")
	}
	var execErr *exec.Error
	if !errors.As(err, &execErr) {
		t.Logf("error type (expected *exec.Error): %T: %v", err, err)
	}
}

func TestExecRunnerExitError(t *testing.T) {
	r := &ExecRunner{}
	ctx := context.Background()
	output, err := r.Run(ctx, "go", "build", "./does-not-exist.go")
	if err == nil {
		t.Error("expected error from go build on nonexistent file")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Logf("error type: %T: %v", err, err)
	} else {
		t.Logf("exit code: %d, stderr: %s", exitErr.ExitCode, exitErr.Stderr)
		// stdout may be empty or contain partial build output
		t.Logf("stdout bytes: %d", len(output))
		if len(output) != len(exitErr.Stdout) {
			t.Error("returned stdout should match ExitError.Stdout")
		}
	}
}

func TestExecRunnerStdoutOnFailure(t *testing.T) {
	r := &ExecRunner{}
	ctx := context.Background()
	// go version should succeed regardless
	output, err := r.Run(ctx, "go", "version")
	if err != nil {
		t.Fatal(err)
	}
	if len(output) == 0 {
		t.Error("expected non-empty output from go version")
	}
}

func TestExitCodeHelper(t *testing.T) {
	t.Run("extracts exit code from ExitError", func(t *testing.T) {
		e := &ExitError{ExitCode: 2, Stderr: "fail", Err: errors.New("test")}
		code, ok := ExitCode(e)
		if !ok || code != 2 {
			t.Errorf("ExitCode = (%d, %v), want (2, true)", code, ok)
		}
	})
	t.Run("returns false for non-exit errors", func(t *testing.T) {
		_, ok := ExitCode(errors.New("plain error"))
		if ok {
			t.Error("expected false for plain error")
		}
	})
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
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type ExecRunner struct {
	Dir string
}

type ExitError struct {
	ExitCode int
	Stderr   string
	Stdout   []byte
	Err      error
}

func (e *ExitError) Error() string {
	stderr := strings.TrimSpace(e.Stderr)
	if stderr == "" {
		return fmt.Sprintf("Tool command failed with exit code %d.", e.ExitCode)
	}
	return fmt.Sprintf("Tool command failed with exit code %d. Stderr: %s", e.ExitCode, stderr)
}

func (e *ExitError) Unwrap() error {
	return e.Err
}

func ExitCode(err error) (int, bool) {
	var e *ExitError
	if errors.As(err, &e) {
		return e.ExitCode, true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}

func Stderr(err error) (string, bool) {
	var e *ExitError
	if errors.As(err, &e) {
		return e.Stderr, true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(exitErr.Stderr), true
	}
	return "", false
}

func (r *ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.Bytes(), &ExitError{
				ExitCode: exitErr.ExitCode(),
				Stderr:   stderr.String(),
				Stdout:   stdout.Bytes(),
				Err:      err,
			}
		}
		return stdout.Bytes(), err
	}

	return stdout.Bytes(), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/runner/ -v`
Expected: PASS (first 3 tests; exit error test is best-effort)

- [ ] **Step 5: Commit**

```bash
git add internal/runner/runner.go internal/runner/runner_test.go
git commit -m "feat: add CommandRunner interface and ExecRunner with Dir support"
```

---

### Task 6: Root Discovery (Two-Pass Walk)

**Files:**
- Create: `internal/root/root.go`
- Create: `internal/root/root_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/root/root_test.go
package root

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverWithGoWork(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "monorepo")
	moduleDir := filepath.Join(workDir, "services", "auth")
	os.MkdirAll(moduleDir, 0755)
	os.WriteFile(filepath.Join(workDir, "go.work"), []byte("go 1.25\nuse ./services/auth\n"), 0644)
	os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module github.com/org/auth\ngo 1.25\n"), 0644)

	got, err := Discover(moduleDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != workDir {
		t.Errorf("root = %q, want %q (go.work location)", got, workDir)
	}
}

func TestDiscoverWithGoMod(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/org/app\ngo 1.25\n"), 0644)

	got, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("root = %q, want %q", got, dir)
	}
}

func TestDiscoverWalkUp(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "pkg", "deep", "path")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/org/app\ngo 1.25\n"), 0644)

	got, err := Discover(subDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("root = %q, want %q (ancestor with go.mod)", got, dir)
	}
}

func TestDiscoverGoWorkWinsOverCloserGoMod(t *testing.T) {
	dir := t.TempDir()
	workDir := filepath.Join(dir, "monorepo")
	moduleDir := filepath.Join(workDir, "sub")
	os.MkdirAll(moduleDir, 0755)
	os.WriteFile(filepath.Join(workDir, "go.work"), []byte("go 1.25\nuse ./sub\n"), 0644)
	os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module github.com/org/sub\ngo 1.25\n"), 0644)

	got, err := Discover(moduleDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != workDir {
		t.Errorf("root = %q, want %q (go.work wins over closer go.mod)", got, workDir)
	}
}

func TestDiscoverNotAGoProject(t *testing.T) {
	dir := t.TempDir()
	_, err := Discover(dir)
	if err == nil {
		t.Error("expected error for directory without go.mod or go.work")
	}
}

func TestWorkspaceModulesSingleModule(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/org/app\ngo 1.25\n"), 0644)
	modules, err := WorkspaceModules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) != 1 || modules[0] != "github.com/org/app" {
		t.Errorf("modules = %v, want [github.com/org/app]", modules)
	}
}

func TestWorkspaceModulesGoWork(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.work"), []byte("go 1.25\nuse ./services/auth\nuse ./lib/common\n"), 0644)
	authDir := filepath.Join(dir, "services", "auth")
	commonDir := filepath.Join(dir, "lib", "common")
	os.MkdirAll(authDir, 0755)
	os.MkdirAll(commonDir, 0755)
	os.WriteFile(filepath.Join(authDir, "go.mod"), []byte("module github.com/org/auth\ngo 1.25\n"), 0644)
	os.WriteFile(filepath.Join(commonDir, "go.mod"), []byte("module github.com/org/common\ngo 1.25\n"), 0644)

	modules, err := WorkspaceModules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) != 2 {
		t.Fatalf("got %d modules, want 2", len(modules))
	}
}

func TestParseUseDirectivesBlockSyntax(t *testing.T) {
	input := `
go 1.25

use (
    ./service/auth
    ./lib/common
)
`
	got, err := parseUseDirectives(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"./service/auth", "./lib/common"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %#v want %#v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/root/ -v`
Expected: FAIL — undefined: Discover

- [ ] **Step 3: Write implementation**

```go
// internal/root/root.go
package root

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrNotGoProject = errors.New("not a Go project: no go.mod or go.work found")

func Discover(startPath string) (string, error) {
	dir, err := filepath.Abs(startPath)
	if err != nil {
		return "", err
	}

	root, found := walkUpFor(dir, "go.work")
	if found {
		return root, nil
	}

	root, found = walkUpFor(dir, "go.mod")
	if found {
		return root, nil
	}

	return "", ErrNotGoProject
}

func walkUpFor(start, filename string) (string, bool) {
	dir := start
	for {
		path := filepath.Join(dir, filename)
		if _, err := os.Stat(path); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func WorkspaceModules(projectRoot string) ([]string, error) {
	workFile := filepath.Join(projectRoot, "go.work")
	data, err := os.ReadFile(workFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			modFile := filepath.Join(projectRoot, "go.mod")
			modData, modErr := os.ReadFile(modFile)
			if modErr != nil {
				return nil, fmt.Errorf("reading go.mod: %w", modErr)
			}
			moduleName := extractModuleName(string(modData))
			if moduleName == "" {
				return nil, nil
			}
			return []string{moduleName}, nil
		}
		return nil, fmt.Errorf("reading go.work: %w", err)
	}

	moduleDirs, err := parseUseDirectives(string(data))
	if err != nil {
		return nil, fmt.Errorf("parsing go.work: %w", err)
	}

	var modules []string
	for _, relPath := range moduleDirs {
		modData, err := os.ReadFile(filepath.Join(projectRoot, relPath, "go.mod"))
		if err != nil {
			continue
		}
		moduleName := extractModuleName(string(modData))
		if moduleName != "" {
			modules = append(modules, moduleName)
		}
	}
	return modules, nil
}

func parseUseDirectives(workContent string) ([]string, error) {
	var dirs []string
	lines := strings.Split(workContent, "\n")
	inBlock := false

	for _, raw := range lines {
		line := strings.TrimSpace(raw)

		// Strip line comments.
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		if line == "" {
			continue
		}

		if line == "use (" {
			inBlock = true
			continue
		}

		if inBlock {
			if line == ")" {
				inBlock = false
				continue
			}
			dirs = append(dirs, strings.Fields(line)[0])
			continue
		}

		if strings.HasPrefix(line, "use ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				dirs = append(dirs, fields[1])
			}
		}
	}

	return dirs, nil
}

func extractModuleName(modContent string) string {
	for _, line := range strings.Split(modContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "module "))
		}
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/root/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/root/root.go internal/root/root_test.go
git commit -m "feat: add two-pass root discovery (go.work then go.mod)"
```

---

### Task 7: Config Loading

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

func TestDefaultConfig(t *testing.T) {
	cfg := Default()
	if cfg.Timeout != 5*time.Minute {
		t.Errorf("default timeout = %v, want 5m", cfg.Timeout)
	}
	if cfg.Tools["golangci-lint"].Version != "v2.11.4" {
		t.Errorf("golangci-lint default version = %q, want v2.11.4", cfg.Tools["golangci-lint"].Version)
	}
	if cfg.Tools["govulncheck"].Version != "latest" {
		t.Errorf("govulncheck default version = %q, want latest", cfg.Tools["govulncheck"].Version)
	}
	if cfg.Tools["nilaway"].Version != "latest" {
		t.Errorf("nilaway default version = %q, want latest", cfg.Tools["nilaway"].Version)
	}
}

func TestLoadFileNotFound(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timeout != 5*time.Minute {
		t.Errorf("timeout = %v", cfg.Timeout)
	}
}

func TestLoadValidFile(t *testing.T) {
	yamlContent := `
timeout: 10m
tools:
  golangci-lint:
    version: v2.11.4
    extra_args: ["--no-config"]
  govulncheck:
    version: latest
    extra_args: []
  nilaway:
    version: latest
    extra_args: ["--exclude-pkgs=github.com/myorg/vendor"]
`
	dir := t.TempDir()
	path := filepath.Join(dir, ".go-quality.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timeout != 10*time.Minute {
		t.Errorf("timeout = %v, want 10m", cfg.Timeout)
	}
	if cfg.Tools["golangci-lint"].Version != "v2.11.4" {
		t.Errorf("golangci-lint version = %q", cfg.Tools["golangci-lint"].Version)
	}
	if len(cfg.Tools["golangci-lint"].ExtraArgs) != 1 {
		t.Errorf("golangci-lint extra_args len = %d, want 1", len(cfg.Tools["golangci-lint"].ExtraArgs))
	}
	if cfg.Tools["golangci-lint"].ExtraArgs[0] != "--no-config" {
		t.Errorf("extra_args[0] = %q", cfg.Tools["golangci-lint"].ExtraArgs[0])
	}
}

func TestValidateExtraArgsReservedFlag(t *testing.T) {
	cfg := Config{
		Timeout: 5 * time.Minute,
		Tools: map[string]ToolConfig{
			"golangci-lint": {
				Version:   "v2.11.4",
				ExtraArgs: []string{"--out-format=text"},
			},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for reserved flag in extra_args")
	}
}

func TestReservedFlags(t *testing.T) {
	flags := ReservedFlags("golangci-lint")
	found := false
	for _, f := range flags {
		if f == "--out-format" {
			found = true
		}
	}
	if !found {
		t.Error("expected --out-format in golangci-lint reserved flags")
	}
}

func TestResolveTimeout(t *testing.T) {
	cfg := Default()
	if cfg.ResolveTimeout() != 5*time.Minute {
		t.Errorf("default resolve = %v", cfg.ResolveTimeout())
	}
	cfg.Timeout = 0
	if cfg.ResolveTimeout() != 5*time.Minute {
		t.Errorf("zero timeout should fall back to default")
	}
	cfg.Timeout = 10 * time.Minute
	if cfg.ResolveTimeout() != 10*time.Minute {
		t.Errorf("explicit timeout = %v", cfg.ResolveTimeout())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — undefined: Config, Default, Load, etc.

- [ ] **Step 3: Write implementation**

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"strings"
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

var reservedByTool = map[string][]string{
	"golangci-lint": {"--out-format"},
	"govulncheck":   {"-json"},
	"nilaway":       {"-json", "-pretty-print"},
}

func ReservedFlags(toolName string) []string {
	return reservedByTool[toolName]
}

func Default() Config {
	return Config{
		Timeout: 5 * time.Minute,
		Tools: map[string]ToolConfig{
			"golangci-lint": {Version: "v2.11.4"},
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

func (c Config) Validate() error {
	for toolName, tc := range c.Tools {
		reserved, ok := reservedByTool[toolName]
		if !ok {
			continue
		}
		for _, arg := range tc.ExtraArgs {
			argName := strings.SplitN(arg, "=", 2)[0]
			for _, r := range reserved {
				if argName == r {
					return fmt.Errorf("config error: extra_args for %s contains reserved flag %s", toolName, r)
				}
			}
		}
	}
	return nil
}

func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("config read error: %w", err)
	}

	var raw Config
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("config parse error: %w", err)
	}

	// Merge: only override fields explicitly present in YAML.
	// Direct unmarshal into cfg would replace the entire Tools map.
	if raw.Timeout > 0 {
		cfg.Timeout = raw.Timeout
	}
	for name, tc := range raw.Tools {
		existing := cfg.Tools[name]
		if tc.Version != "" {
			existing.Version = tc.Version
		}
		if len(tc.ExtraArgs) > 0 {
			existing.ExtraArgs = tc.ExtraArgs
		}
		cfg.Tools[name] = existing
	}

	// Fill empty versions on the merged (defaults-preserving) config.
	for name := range cfg.Tools {
		tc := cfg.Tools[name]
		if tc.Version == "" {
			tc.Version = "latest"
			if name == "golangci-lint" {
				tc.Version = "v2.11.4"
			}
			cfg.Tools[name] = tc
		}
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
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
git commit -m "feat: add config loading with extra_args reserved flag validation"
```

---

### Task 8: Tool Name Constants + Validation

**Files:**
- Create: `internal/toolname/toolname.go`
- Create: `internal/toolname/toolname_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/toolname/toolname_test.go
package toolname

import "testing"

func TestIsValid(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"golangci-lint", true},
		{"govulncheck", true},
		{"nilaway", true},
		{"unknown-tool", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValid(tt.name); got != tt.want {
				t.Errorf("IsValid(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestAll(t *testing.T) {
	all := All()
	if len(all) != 3 {
		t.Fatalf("got %d tools, want 3", len(all))
	}
	seen := map[string]bool{}
	for _, name := range all {
		seen[name] = true
	}
	for _, name := range []string{
		GolangciLint,
		Govulncheck,
		Nilaway,
	} {
		if !seen[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestInstallPath(t *testing.T) {
	tests := []struct {
		tool string
		want string
	}{
		{GolangciLint, "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"},
		{Govulncheck, "golang.org/x/vuln/cmd/govulncheck"},
		{Nilaway, "go.uber.org/nilaway/cmd/nilaway"},
		{"unknown", ""},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			if got := InstallPath(tt.tool); got != tt.want {
				t.Errorf("InstallPath(%q) = %q, want %q", tt.tool, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/toolname/ -v`
Expected: FAIL — undefined symbols

- [ ] **Step 3: Write implementation**

```go
// internal/toolname/toolname.go
package toolname

const (
	GolangciLint = "golangci-lint"
	Govulncheck  = "govulncheck"
	Nilaway      = "nilaway"
)

func IsValid(name string) bool {
	switch name {
	case GolangciLint, Govulncheck, Nilaway:
		return true
	default:
		return false
	}
}

func All() []string {
	return []string{GolangciLint, Govulncheck, Nilaway}
}

func InstallPath(name string) string {
	switch name {
	case GolangciLint:
		return "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
	case Govulncheck:
		return "golang.org/x/vuln/cmd/govulncheck"
	case Nilaway:
		return "go.uber.org/nilaway/cmd/nilaway"
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/toolname/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/toolname/toolname.go internal/toolname/toolname_test.go
git commit -m "feat: add tool name constants and validation"
```

---

### Task 9: Binary Directory Resolution + Tool Discovery & Version-Aware Cache

**Files:**
- Create: `internal/discover/discover.go`
- Create: `internal/discover/discover_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/discover/discover_test.go
package discover

import (
	"testing"
)

func TestResolveGoBinDir(t *testing.T) {
	binDir, err := ResolveGoBinDir()
	if err != nil {
		t.Fatal(err)
	}
	if binDir == "" {
		t.Error("binDir must not be empty")
	}
	t.Logf("resolved binDir: %s", binDir)
}

func TestParseGoVersionOutput(t *testing.T) {
	output := []byte(`/home/user/go/bin/golangci-lint: devel go1.25.9
	path	github.com/golangci/golangci-lint/v2/cmd/golangci-lint
	mod	github.com/golangci/golangci-lint/v2	v2.11.4	h1:abc123=
	dep	github.com/BurntSushi/toml	v1.4.0	h1:def456=
	build	-buildmode=exe
	build	-compiler=gc
`)
	version := ParseModuleVersion(output, "github.com/golangci/golangci-lint/v2")
	if version != "v2.11.4" {
		t.Errorf("version = %q, want v2.11.4", version)
	}
}

func TestParseGoVersionOutputNilaway(t *testing.T) {
	output := []byte(`/home/user/go/bin/nilaway: devel go1.25.9
	path	go.uber.org/nilaway/cmd/nilaway
	mod	go.uber.org/nilaway	v0.0.0-20260515015210-fd187751154f	h1:abc=
`)
	version := ParseModuleVersion(output, "go.uber.org/nilaway")
	if version != "v0.0.0-20260515015210-fd187751154f" {
		t.Errorf("version = %q, want pseudo-version", version)
	}
}

func TestParseGoVersionOutputGovulncheck(t *testing.T) {
	output := []byte(`/home/user/go/bin/govulncheck: devel go1.25.9
	path	golang.org/x/vuln/cmd/govulncheck
	mod	golang.org/x/vuln	v1.3.0	h1:abc=
`)
	version := ParseModuleVersion(output, "golang.org/x/vuln")
	if version != "v1.3.0" {
		t.Errorf("version = %q, want v1.3.0", version)
	}
}

func TestParseGoVersionUnknown(t *testing.T) {
	output := []byte(`/home/user/go/bin/custom-tool: devel go1.25.9
	path	some/custom/tool
`)
	version := ParseModuleVersion(output, "some/custom/tool")
	if version != "unknown" {
		t.Errorf("version = %q, want unknown", version)
	}
}

func TestCacheHitAndMiss(t *testing.T) {
	c := NewCache()
	c.Store("govulncheck", "v1.3.0")
	c.Store("nilaway", "v0.0.0-20260515")

	v, ok := c.Load("govulncheck")
	if !ok || v != "v1.3.0" {
		t.Errorf("govulncheck = (%q, %v)", v, ok)
	}

	_, ok = c.Load("golangci-lint")
	if ok {
		t.Error("golangci-lint should be a cache miss")
	}
}

func TestCacheUnknownVersion(t *testing.T) {
	c := NewCache()
	c.Store("govulncheck", "unknown")
	v, ok := c.Load("govulncheck")
	if !ok || v != "unknown" {
		t.Errorf("unknown version should be stored and retrievable")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/discover/ -v`
Expected: FAIL — undefined symbols

- [ ] **Step 3: Write implementation**

```go
// internal/discover/discover.go
package discover

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

func ResolveGoBinDir() (string, error) {
	out, err := exec.Command("go", "env", "GOBIN").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOBIN: %w", err)
	}
	if binDir := strings.TrimSpace(string(out)); binDir != "" && binDir != "\n" {
		return binDir, nil
	}

	out, err = exec.Command("go", "env", "GOPATH").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOPATH: %w", err)
	}
	gopath := strings.TrimSpace(string(out))
	if gopath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("os.UserHomeDir: %w", err)
		}
		gopath = filepath.Join(homeDir, "go")
	}
	return filepath.Join(gopath, "bin"), nil
}

type Cache struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewCache() *Cache {
	return &Cache{data: make(map[string]string)}
}

// InstallMu serializes all install operations across concurrent requests.
// Held during the entire slow path: re-check → resolve → install → cache update.
var InstallMu sync.Mutex

func (c *Cache) Load(toolName string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[toolName]
	return v, ok
}

func (c *Cache) Store(toolName, version string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[toolName] = version
}

func ParseModuleVersion(goVersionOutput []byte, modulePath string) string {
	scanner := bufio.NewScanner(bytes.NewReader(goVersionOutput))
	prefix := "mod\t" + modulePath + "\t"
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, prefix) {
			rest := strings.TrimPrefix(line, prefix)
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				return fields[0]
			}
		}
	}
	return "unknown"
}

func ReadInstalledVersion(binDir, toolName, modulePath string) (string, error) {
	binaryPath := filepath.Join(binDir, toolName)
	cmd := exec.Command("go", "version", "-m", binaryPath)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go version -m %s: %w", binaryPath, err)
	}
	version := ParseModuleVersion(output, modulePath)
	return version, nil
}

func ResolveLatest(ctx context.Context, modulePath string) (string, error) {
	args := []string{"list", "-m", "-json", modulePath + "@latest"}
	cmd := exec.CommandContext(ctx, "go", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list -m -json %s@latest: %w", modulePath, err)
	}
	var info struct {
		Version string `json:"Version"`
	}
	if err := json.Unmarshal(output, &info); err != nil {
		return "", fmt.Errorf("parsing go list output: %w", err)
	}
	if info.Version == "" {
		return "", fmt.Errorf("empty version from go list for %s", modulePath)
	}
	return info.Version, nil
}

// InstallResult holds the outcome of EnsureInstalled.
type InstallResult struct {
	Version        string
	NewlyInstalled bool
}

// EnsureInstalled follows the canonical double-check sequence per
// docs/superpowers/plans/install-lock-double-check-sequence.md.
//
// Fast path (no contention):
//   1. RLock cache → check for matching version → return if found.
//
// Slow path (install needed):
//   2. Lock InstallMu
//   3. Re-check cache (another request may have installed while we waited)
//   4. Resolve version if "latest"
//   5. Verify binary on disk
//   6. go install if missing or wrong version
//   7. Verify install succeeded (os.Stat binary)
//   8. Update cache
//   9. Unlock InstallMu
//
// Context cancellation is checked before InstallMu acquire and inside resolve/install.
func EnsureInstalled(
	ctx context.Context,
	cache *Cache,
	binDir, toolName, modulePath, installPath, requestedVersion string,
) (InstallResult, error) {

	// — FAST PATH —

	if v, ok := cache.Load(toolName); ok && (v == "unknown" || v == requestedVersion) {
		return InstallResult{Version: v, NewlyInstalled: false}, nil
	}

	if requestedVersion != "latest" {
		installed, err := ReadInstalledVersion(binDir, toolName, modulePath)
		if err == nil && (installed == "unknown" || installed == requestedVersion) {
			cache.Store(toolName, installed)
			return InstallResult{Version: installed, NewlyInstalled: false}, nil
		}
	}

	// — SLOW PATH —

	select {
	case <-ctx.Done():
		return InstallResult{}, ctx.Err()
	default:
	}

	InstallMu.Lock()
	defer InstallMu.Unlock()

	// Re-check after acquiring lock.
	if v, ok := cache.Load(toolName); ok && (v == "unknown" || v == requestedVersion) {
		return InstallResult{Version: v, NewlyInstalled: false}, nil
	}

	if requestedVersion != "latest" {
		installed, err := ReadInstalledVersion(binDir, toolName, modulePath)
		if err == nil && (installed == "unknown" || installed == requestedVersion) {
			cache.Store(toolName, installed)
			return InstallResult{Version: installed, NewlyInstalled: false}, nil
		}
	}

	select {
	case <-ctx.Done():
		return InstallResult{}, ctx.Err()
	default:
	}

	// Resolve version.  "latest" resolution happens inside InstallMu.
	resolved := requestedVersion
	if requestedVersion == "latest" {
		v, err := ResolveLatest(ctx, modulePath)
		if err != nil {
			return InstallResult{}, fmt.Errorf("resolving latest for %s: %w", toolName, err)
		}
		resolved = v

		// Re-check cache with the now-resolved concrete version.
		if v2, ok := cache.Load(toolName); ok && (v2 == "unknown" || v2 == resolved) {
			return InstallResult{Version: v2, NewlyInstalled: false}, nil
		}
	}

	// Install.
	pkgWithVersion := fmt.Sprintf("%s@%s", installPath, resolved)
	cmd := exec.CommandContext(ctx, "go", "install", pkgWithVersion)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Never cache a failed install.
		return InstallResult{}, fmt.Errorf(
			"install failed: go install %s. exit code %d. stderr: %s",
			pkgWithVersion, cmd.ProcessState.ExitCode(), string(output),
		)
	}

	// Verify binary exists on disk (go install may succeed but produce no binary).
	binaryPath := filepath.Join(binDir, toolName)
	if _, err := os.Stat(binaryPath); err != nil {
		return InstallResult{}, fmt.Errorf("installed %s but binary not found at %s: %w", toolName, binaryPath, err)
	}

	cache.Store(toolName, resolved)
	return InstallResult{Version: resolved, NewlyInstalled: true}, nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/checkers/ -run "TestParseGolangciLint|TestGolangciLint|TestPathNorm" -v`
Expected: FAIL — undefined symbols

- [ ] **Step 3: Write implementation**

```go
// internal/checkers/golangci_lint.go
package checkers

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/pathutil"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
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
	SourceLines    []string        `json:"SourceLines"`
	SuggestedFixes json.RawMessage `json:"SuggestedFixes"`
}

type golangciLintOutput struct {
	Issues []golangciLintIssue `json:"Issues"`
}

type GolangciLintHandler struct {
	BinDir    string
	ExtraArgs []string
}

func NewGolangciLintHandler(binDir string) *GolangciLintHandler {
	return &GolangciLintHandler{BinDir: binDir}
}

func (h *GolangciLintHandler) Name() string {
	return toolname.GolangciLint
}

func (h *GolangciLintHandler) Run(ctx context.Context, r runner.CommandRunner, projectPath string) ([]diagnostic.Diagnostic, error) {
	args := []string{"run", "--out-format=json"}
	args = append(args, h.ExtraArgs...)
	args = append(args, "./...")
	binary := filepath.Join(h.BinDir, "golangci-lint")
	output, err := r.Run(ctx, binary, args...)
	if err != nil {
		return nil, fmt.Errorf("golangci-lint: %w", err)
	}
	return parseGolangciLintOutput(output, projectPath)
}

func parseGolangciLintOutput(output []byte, projectRoot string) ([]diagnostic.Diagnostic, error) {
	var result golangciLintOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("unexpected output format from golangci-lint: %w", err)
	}

	diags := make([]diagnostic.Diagnostic, 0, len(result.Issues))
	for _, issue := range result.Issues {
		file := pathutil.Rel(projectRoot, issue.Pos.Filename)

		native, err := json.Marshal(issue)
		if err != nil {
			native = nil
		}

		diags = append(diags, diagnostic.Diagnostic{
			Tool:     toolname.GolangciLint,
			File:     file,
			Line:     issue.Pos.Line,
			Column:   issue.Pos.Column,
			Severity: issue.Severity,
			Message:  issue.Text,
			Native:   native,
		})
	}
	return diags, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/checkers/ -run "TestParseGolangciLint|TestGolangciLint|TestPathNorm" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/checkers/golangci_lint.go internal/checkers/golangci_lint_test.go
git commit -m "feat: add golangci-lint handler and JSON parser"
```

---

### Task 12: govulncheck Handler + NDJSON Parser

**Files:**
- Create: `internal/checkers/govulncheck.go`
- Create: `internal/checkers/govulncheck_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/checkers/govulncheck_test.go
package checkers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
)

func TestParseGovulncheckOutputFindsVulnerabilities(t *testing.T) {
	input := `{"config":{"protocol_version":"v1.0.0"}}
{"osv":{"id":"GO-2026-4918","summary":"Infinite loop in HTTP/2 transport","aliases":["CVE-2026-33814"]}}
{"finding":{"osv":"GO-2026-4918","fixed_version":"v1.25.10","trace":[
  {"module":"github.com/myorg/myapp","version":"v1.0.0","package":"github.com/myorg/myapp/internal/httpclient","function":"Get","position":{"filename":"internal/httpclient/client.go","line":78,"column":25}},
  {"module":"stdlib","version":"v1.25.9","package":"net/http","function":"Do","position":{"filename":"src/net/http/client.go","line":586,"column":18}}
]}}
{"finding":{"osv":"GO-2026-4971","fixed_version":"v1.25.10","trace":[
  {"module":"stdlib","version":"v1.25.9","package":"net"}
]}}
{"progress":{"message":"done"}}
`
	projectRoot := "/project/myapp"
	workspaceModules := []string{"github.com/myorg/myapp"}

	diags, err := parseGovulncheckOutput([]byte(input), projectRoot, workspaceModules)
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics, want 2", len(diags))
	}

	d0 := diags[0]
	if d0.Tool != toolname.Govulncheck {
		t.Errorf("tool = %q, want %s", d0.Tool, toolname.Govulncheck)
	}
	if d0.File != "internal/httpclient/client.go" {
		t.Errorf("file = %q, want internal/httpclient/client.go", d0.File)
	}
	if d0.Line != 78 {
		t.Errorf("line = %d, want 78", d0.Line)
	}
	if d0.Column != 25 {
		t.Errorf("column = %d, want 25", d0.Column)
	}
	if d0.Message == "" {
		t.Error("message is empty")
	}
	if d0.Severity != "" {
		t.Error("severity should be empty for govulncheck (no severity concept)")
	}

	d1 := diags[1]
	if d1.File != "" {
		t.Errorf("file should be empty for module-level finding without position, got %q", d1.File)
	}

	container := NativeContainer(d0.Native)
	if container.Finding == nil {
		t.Error("native container should have finding")
	}
	if container.OSV == nil {
		t.Error("native container should have osv")
	}
}

func TestParseGovulncheckOutputMissingOSVMap(t *testing.T) {
	input := `{"finding":{"osv":"UNKNOWN-ID","fixed_version":"v1.25.10","trace":[
  {"module":"github.com/myorg/myapp","version":"v1.0.0","package":"github.com/myorg/myapp/internal/httpclient","function":"Get","position":{"filename":"internal/httpclient/client.go","line":78,"column":25}}
]}}
`
	diags, err := parseGovulncheckOutput([]byte(input), "/project/myapp", []string{"github.com/myorg/myapp"})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diags))
	}
	if diags[0].Message != "UNKNOWN-ID" {
		t.Errorf("message should fall back to OSV ID, got %q", diags[0].Message)
	}
}

func TestParseGovulncheckOutputNDJSONParseError(t *testing.T) {
	input := `{"config":{"protocol_version":"v1.0.0"}}
{"osv":{"id":"GO-2026-4918","summary":"Test vuln"}}
{"finding":{"osv":"GO-2026-4918","fixed_version":"v1.25.10","trace":[
  {"module":"github.com/myorg/myapp","version":"v1.0.0","package":"github.com/myorg/myapp/internal/httpclient","function":"Get","position":{"filename":"internal/httpclient/client.go","line":78,"column":25}}
]}}
{invalid json line here}
{"finding":{"osv":"GO-2026-4999","fixed_version":"v1.25.10","trace":[
  {"module":"github.com/myorg/myapp","version":"v1.0.0","package":"net/http","function":"Do","position":{"filename":"src/net/http/client.go","line":586,"column":18}}
]}}
`

	diags, err := parseGovulncheckOutput([]byte(input), "/project/myapp", []string{"github.com/myorg/myapp"})
	if err != nil {
		t.Fatal(err)
	}

	hasFindings := false
	hasParseError := false
	for _, d := range diags {
		if d.Error != "" && d.Error != "" {
			hasParseError = true
			if d.Native == nil {
				t.Error("parse error diagnostic should have Native populated with raw content")
			}
		}
		if d.Message != "" {
			hasFindings = true
		}
	}
	if !hasFindings {
		t.Error("should have findings for parseable lines")
	}
	if !hasParseError {
		t.Error("should have parse error diagnostic for malformed line")
	}
}

func TestGovulncheckHandlerWithMock(t *testing.T) {
	input := `{"config":{"protocol_version":"v1.0.0"}}
{"osv":{"id":"GO-2026-4918","summary":"Test vuln"}}
`
	r := &mockRunner{
		outputs: map[string][]byte{
			"govulncheck:-json": []byte(input),
		},
	}
	handler := &GovulncheckHandler{BinDir: "/fake/bin", WorkspaceModules: []string{"github.com/myorg/myapp"}}
	diags, err := handler.Run(context.Background(), r, "/project/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics (no findings), got %d", len(diags))
	}
}

func TestChooseTraceEntry(t *testing.T) {
	trace := []traceEntryJSON{
		{Module: "github.com/myorg/myapp", Package: "github.com/myorg/myapp/internal/httpclient", Position: &positionJSON{Filename: "internal/httpclient/client.go", Line: 78, Column: 25}},
		{Module: "stdlib", Package: "net/http", Position: &positionJSON{Filename: "src/net/http/client.go", Line: 586, Column: 18}},
	}
	workspaceModules := []string{"github.com/myorg/myapp"}
	entry := chooseTraceEntry(trace, workspaceModules)
	if entry == nil || entry.Position.Line != 78 {
		t.Errorf("should pick first workspace-local entry (trace[0]), got line %d", entry.Position.Line)
	}
}

func TestChooseTraceEntryNoWorkspaceMatch(t *testing.T) {
	trace := []traceEntryJSON{
		{Module: "stdlib", Package: "net/http", Position: &positionJSON{Filename: "src/net/http/client.go", Line: 586, Column: 18}},
	}
	entry := chooseTraceEntry(trace, []string{"github.com/myorg/myapp"})
	if entry == nil || entry.Position.Line != 586 {
		t.Error("should fall back to trace[0] when no workspace match")
	}
}

func TestParseGovulncheckFindingBeforeOSV(t *testing.T) {
	// finding arrives before its corresponding osv — deferred resolution must still work.
	input := `{"finding":{"osv":"GO-2026-4918","fixed_version":"v1.25.10","trace":[
  {"module":"github.com/myorg/myapp","version":"v1.0.0","package":"github.com/myorg/myapp/internal/httpclient","function":"Get","position":{"filename":"internal/httpclient/client.go","line":78,"column":25}}
]}}
{"osv":{"id":"GO-2026-4918","summary":"Infinite loop in HTTP/2 transport","aliases":["CVE-2026-33814"]}}
`
	diags, err := parseGovulncheckOutput([]byte(input), "/project/myapp", []string{"github.com/myorg/myapp"})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diags))
	}
	if diags[0].Message != "Infinite loop in HTTP/2 transport" {
		t.Errorf("message = %q, want resolved osv summary", diags[0].Message)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/checkers/ -run "Govulncheck|ChooseTrace" -v`
Expected: FAIL — undefined symbols

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
	"strings"
	"time"

	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/pathutil"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
)

const (
	vulnDBLockPhrase = "database is locked"
	maxVulnRetries   = 3
	vulnRetryBackoff = 2 * time.Second
)

type positionJSON struct {
	Filename string `json:"filename"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

type traceEntryJSON struct {
	Module   string        `json:"module"`
	Version  string        `json:"version"`
	Package  string        `json:"package"`
	Function string        `json:"function"`
	Position *positionJSON `json:"position,omitempty"`
}

type findingJSON struct {
	OSV          string           `json:"osv"`
	FixedVersion string           `json:"fixed_version"`
	Trace        []traceEntryJSON `json:"trace"`
}

type osvJSON struct {
	ID      string   `json:"id"`
	Summary string   `json:"summary"`
	Aliases []string `json:"aliases"`
}

type GovulncheckHandler struct {
	BinDir            string
	WorkspaceModules []string
	ExtraArgs        []string
}

func (h *GovulncheckHandler) Name() string {
	return toolname.Govulncheck
}

func (h *GovulncheckHandler) Run(ctx context.Context, r runner.CommandRunner, projectPath string) ([]diagnostic.Diagnostic, error) {
	args := []string{"-json"}
	args = append(args, h.ExtraArgs...)
	args = append(args, "./...")

	binary := filepath.Join(h.BinDir, "govulncheck")

	var output []byte
	var exitErr error

	for attempt := 0; attempt < maxVulnRetries; attempt++ {
		output, exitErr = r.Run(ctx, binary, args...)

		if exitErr != nil {
			stderr, _ := runner.Stderr(exitErr)
			code, _ := runner.ExitCode(exitErr)
			if code == 1 && strings.Contains(stderr, vulnDBLockPhrase) {
				select {
				case <-time.After(vulnRetryBackoff):
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		}
		break
	}

	diags, parseErr := parseGovulncheckOutput(output, projectPath, h.WorkspaceModules)
	if parseErr != nil {
		return []diagnostic.Diagnostic{{
			Tool:   toolname.Govulncheck,
			Error:  fmt.Sprintf("unexpected output format from govulncheck: %v", parseErr),
		}}, nil
	}

	if exitErr != nil && len(diags) == 0 {
		diags = append(diags, diagnostic.Diagnostic{
			Tool:  toolname.Govulncheck,
			Error: exitErr.Error(),
		})
	}

	return diags, nil
}

type govulnLine struct {
	OSV    *osvJSON    `json:"osv,omitempty"`
	Finding *findingJSON `json:"finding,omitempty"`
}

type GovulncheckNativeContainer struct {
	Finding json.RawMessage `json:"finding"`
	OSV     json.RawMessage `json:"osv"`
}

func NativeContainer(native json.RawMessage) *GovulncheckNativeContainer {
	if native == nil {
		return nil
	}
	var c GovulncheckNativeContainer
	if err := json.Unmarshal(native, &c); err != nil {
		return nil
	}
	return &c
}

func parseGovulncheckOutput(output []byte, projectRoot string, workspaceModules []string) ([]diagnostic.Diagnostic, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	osvMap := make(map[string]*osvJSON)
	var findings []findingJSON
	var parseErrors []string
	var rawParseErrorLines []string

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var entry govulnLine
		if err := json.Unmarshal(line, &entry); err != nil {
			parseErrors = append(parseErrors, err.Error())
			rawParseErrorLines = append(rawParseErrorLines, string(line))
			continue
		}

		if entry.OSV != nil {
			osvMap[entry.OSV.ID] = entry.OSV
		}
		if entry.Finding != nil {
			findings = append(findings, *entry.Finding)
		}
	}

	diags := make([]diagnostic.Diagnostic, 0, len(findings)+1)

	for _, f := range findings {
		message := f.OSV
		if osv, ok := osvMap[f.OSV]; ok && osv.Summary != "" {
			message = osv.Summary
		}

		entry := chooseTraceEntry(f.Trace, workspaceModules)
		file := ""
		line := 0
		column := 0
		if entry != nil {
			file = pathutil.Rel(projectRoot, entry.Position.Filename)
			line = entry.Position.Line
			column = entry.Position.Column
		}

		findingRaw, _ := json.Marshal(f)

		var osvRaw json.RawMessage = json.RawMessage("null")
		if osv, ok := osvMap[f.OSV]; ok {
			osvRaw, _ = json.Marshal(osv)
		}

		container := GovulncheckNativeContainer{
			Finding: findingRaw,
			OSV:     osvRaw,
		}
		native, _ := json.Marshal(container)

		diags = append(diags, diagnostic.Diagnostic{
			Tool:    toolname.Govulncheck,
			File:    file,
			Line:    line,
			Column:  column,
			Message: message,
			Native:  native,
		})
	}

	if len(parseErrors) > 0 {
		rawContent := strings.Join(rawParseErrorLines, "\n")
		rawBytes, _ := json.Marshal(rawContent)

		diags = append(diags, diagnostic.Diagnostic{
			Tool:   toolname.Govulncheck,
			Error:  fmt.Sprintf("%d line(s) failed to parse: %s", len(parseErrors), parseErrors[0]),
			Native: rawBytes,
		})
	}

	return diags, nil
}

func chooseTraceEntry(trace []traceEntryJSON, workspaceModules []string) *traceEntryJSON {
	if len(trace) == 0 {
		return nil
	}
	// Find first entry whose module matches a workspace-local module
	for i := 0; i < len(trace); i++ {
		for _, mod := range workspaceModules {
			if trace[i].Module == mod {
				return &trace[i]
			}
		}
	}
	// No workspace-local match — fall back to first entry that has a position
	for i := 0; i < len(trace); i++ {
		if trace[i].Position != nil && trace[i].Position.Filename != "" {
			return &trace[i]
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/checkers/ -run "Govulncheck|ChooseTrace" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/checkers/govulncheck.go internal/checkers/govulncheck_test.go
git commit -m "feat: add govulncheck handler with NDJSON parser and DB lock retry"
```

---

### Task 13: nilaway Handler + Parser

**Files:**
- Create: `internal/checkers/nilaway.go`
- Create: `internal/checkers/nilaway_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/checkers/nilaway_test.go
package checkers

import (
	"context"
	"encoding/json"
	"testing"
	"unicode"

	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
)

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
        "message": "Potential nil panic detected. Deep read from local variable \\"counts\\"\n"
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
	if d0.Tool != toolname.Nilaway {
		t.Errorf("tool = %q, want %s", d0.Tool, toolname.Nilaway)
	}
	if d0.File != "internal/engine/pulse.go" {
		t.Errorf("file = %q, want internal/engine/pulse.go", d0.File)
	}
	if d0.Line != 42 {
		t.Errorf("line = %d, want 42", d0.Line)
	}
	if d0.Column != 12 {
		t.Errorf("column = %d, want 12", d0.Column)
	}
	if d0.Severity != "" {
		t.Error("severity should be empty for nilaway (no severity concept)")
	}
	if d0.Message == "" {
		t.Error("message is empty")
	}
	if d0.Native == nil {
		t.Error("native is nil")
	}

	var native map[string]interface{}
	if err := json.Unmarshal(d0.Native, &native); err != nil {
		t.Errorf("native should be valid JSON: %v", err)
	}
}

func TestParseNilawayEmptyOutput(t *testing.T) {
	input := `{}`
	diags, err := parseNilawayOutput([]byte(input), "/project/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics for empty {}, got %d", len(diags))
	}
}

func TestParseNilawayEmptyStdout(t *testing.T) {
	input := ``
	_, err := parseNilawayOutput([]byte(input), "/project/myapp")
	if err == nil {
		t.Error("expected error for empty stdout (distinct from {})")
	}
}

func TestParsePosn(t *testing.T) {
	tests := []struct {
		posn     string
		wantFile string
		wantLine int
		wantCol  int
	}{
		{"/project/myapp/pkg/handler.go:123:45", "/project/myapp/pkg/handler.go", 123, 45},
		{"/project/myapp/main.go:10:1", "/project/myapp/main.go", 10, 1},
		{"/project/myapp/file.go:5", "/project/myapp/file.go", 5, 0},
		{"/project/myapp/file.go", "/project/myapp/file.go", 0, 0},
		{"", "", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.posn, func(t *testing.T) {
			file, line, col := parsePosn(tt.posn)
			if file != tt.wantFile {
				t.Errorf("file = %q, want %q", file, tt.wantFile)
			}
			if line != tt.wantLine {
				t.Errorf("line = %d, want %d", line, tt.wantLine)
			}
			if col != tt.wantCol {
				t.Errorf("col = %d, want %d", col, tt.wantCol)
			}
		})
	}
}

func TestExtractFirstSentence(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Potential nil panic detected. More details here.", "Potential nil panic detected."},
		{"Single line with no period", "Single line with no period"},
		{"First. Second. Third.", "First."},
		{"", ""},
		{"No period but newline\n second line", "No period but newline"},
		{"Multiple newlines\n\n after", "Multiple newlines"},
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

func TestNilawayHandlerWithMock(t *testing.T) {
	input := `{}`
	r := &mockRunner{
		outputs: map[string][]byte{
			"nilaway:-json": []byte(input),
		},
	}
	handler := &NilawayHandler{BinDir: "/fake/bin"}
	diags, err := handler.Run(context.Background(), r, "/project/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics, got %d", len(diags))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/checkers/ -run "Nilaway|ParsePosn|ExtractFirst" -v`
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
	"unicode"

	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/pathutil"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
)

type nilawayIssue struct {
	Posn    string `json:"posn"`
	End     string `json:"end"`
	Message string `json:"message"`
}

type nilawayPackageResult struct {
	Nilaway []nilawayIssue `json:"nilaway"`
}

type nilawayOutput map[string]nilawayPackageResult

type NilawayHandler struct {
	BinDir      string
	IncludePkgs string
	ExtraArgs   []string
}

func (h *NilawayHandler) Name() string {
	return toolname.Nilaway
}

func (h *NilawayHandler) Run(ctx context.Context, r runner.CommandRunner, projectPath string) ([]diagnostic.Diagnostic, error) {
	args := []string{"-json", "-pretty-print=false"}
	if h.IncludePkgs != "" {
		args = append(args, "-include-pkgs="+h.IncludePkgs)
	}
	args = append(args, h.ExtraArgs...)
	args = append(args, "./...")

	binary := filepath.Join(h.BinDir, "nilaway")

	output, err := r.Run(ctx, binary, args...)
	if err != nil {
		return nil, fmt.Errorf("nilaway: %w", err)
	}

	return parseNilawayOutput(output, projectPath)
}

func parseNilawayOutput(output []byte, projectRoot string) ([]diagnostic.Diagnostic, error) {
	if len(output) == 0 {
		return nil, fmt.Errorf("unexpected output format from nilaway")
	}

	var raw nilawayOutput
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("unexpected output format from nilaway: %w", err)
	}

	if len(raw) == 0 {
		return []diagnostic.Diagnostic{}, nil
	}

	var diags []diagnostic.Diagnostic
	for _, pkg := range raw {
		for _, issue := range pkg.Nilaway {
			absFile, line, col := parsePosn(issue.Posn)
			if absFile == "" {
				native, _ := json.Marshal(issue)
				diags = append(diags, diagnostic.Diagnostic{
					Tool:   toolname.Nilaway,
					Error:  "unable to parse nilaway position",
					Native: native,
				})
				continue
			}
			file := pathutil.Rel(projectRoot, absFile)

			native, _ := json.Marshal(issue)

			diags = append(diags, diagnostic.Diagnostic{
				Tool:    toolname.Nilaway,
				File:    file,
				Line:    line,
				Column:  col,
				Message: extractFirstSentence(issue.Message),
				Native:  native,
			})
		}
	}
	return diags, nil
}

func parsePosn(posn string) (file string, line int, col int) {
	if posn == "" {
		return "", 0, 0
	}

	lastColon := strings.LastIndex(posn, ":")
	if lastColon < 0 {
		return posn, 0, 0
	}

	secondLastColon := strings.LastIndex(posn[:lastColon], ":")
	if secondLastColon < 0 {
		return posn[:lastColon], 0, 0
	}

	colStr := posn[lastColon+1:]
	col, _ = strconv.Atoi(colStr)

	lineStr := posn[secondLastColon+1 : lastColon]
	line, _ = strconv.Atoi(lineStr)

	file = posn[:secondLastColon]
	return file, line, col
}

func extractFirstSentence(s string) string {
	if s == "" {
		return ""
	}

	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\n' {
			return string(runes[:i])
		}
		if runes[i] == '.' && i+2 < len(runes) && runes[i+1] == ' ' && unicode.IsUpper(runes[i+2]) {
			return string(runes[:i+1])
		}
	}
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/checkers/ -run "Nilaway|ParsePosn|ExtractFirst" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/checkers/nilaway.go internal/checkers/nilaway_test.go
git commit -m "feat: add nilaway handler with posn parser and sentence extraction"
```

---

### Task 14: Parallel Orchestrator (runAll)

**Files:**
- Create: `internal/checkers/orchestrator.go`
- Create: `internal/checkers/orchestrator_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/checkers/orchestrator_test.go
package checkers

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
)

func TestRunAllWithSubset(t *testing.T) {
	lintOutput := `{"Issues": [{"FromLinter":"gocritic","Text":"test","Severity":"warning","Pos":{"Filename":"main.go","Line":1,"Column":1}}]}`
	vulnOutput := `{"config":{"protocol_version":"v1.0.0"}}
`
	nilawayOutput := `{}`

	r := &mockRunner{
		outputs: map[string][]byte{
			"golangci-lint:run": []byte(lintOutput),
			"govulncheck:-json": []byte(vulnOutput),
			"nilaway:-json":     []byte(nilawayOutput),
		},
	}

	handlers := []Checker{
		NewGolangciLintHandler("/fake/bin"),
		&GovulncheckHandler{BinDir: "/fake/bin", WorkspaceModules: []string{"github.com/myorg/myapp"}},
		&NilawayHandler{BinDir: "/fake/bin"},
	}

	diags := RunAllChecks(context.Background(), r, handlers, "/project/myapp", 5*time.Minute)
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1 (only lint has findings)", len(diags))
	}
	if diags[0].Tool != "golangci-lint" {
		t.Errorf("tool = %q", diags[0].Tool)
	}
}

func TestRunAllEmptySubset(t *testing.T) {
	r := &mockRunner{
		outputs: map[string][]byte{},
	}
	diags := RunAllChecks(context.Background(), r, nil, "/project/myapp", 5*time.Minute)
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics for empty subset, got %d", len(diags))
	}
}

func TestRunAllPanicRecovery(t *testing.T) {
	panicChecker := &panicTestChecker{name: "panic-tool"}
	r := &mockRunner{outputs: map[string][]byte{}}
	diags := RunAllChecks(context.Background(), r, []Checker{panicChecker}, "/project/myapp", 5*time.Second)
	foundPanicError := false
	for _, d := range diags {
		if d.Error != "" && d.Tool == "panic-tool" {
			foundPanicError = true
		}
	}
	if !foundPanicError {
		t.Error("expected panic recovery diagnostic, got none")
	}
}

type panicTestChecker struct {
	name string
}

func (p *panicTestChecker) Name() string { return p.name }

func (p *panicTestChecker) Run(ctx context.Context, r runner.CommandRunner, projectPath string) ([]diagnostic.Diagnostic, error) {
	panic("test panic")
}

func TestRunAllSortsResults(t *testing.T) {
	lintOutput := `{"Issues": [
		{"FromLinter":"test","Text":"later","Severity":"warning","Pos":{"Filename":"z.go","Line":10,"Column":1}},
		{"FromLinter":"test","Text":"earlier","Severity":"warning","Pos":{"Filename":"a.go","Line":5,"Column":1}},
		{"FromLinter":"test","Text":"same file","Severity":"warning","Pos":{"Filename":"a.go","Line":1,"Column":1}}
	]}`
	r := &mockRunner{
		outputs: map[string][]byte{
			"golangci-lint:run": []byte(lintOutput),
		},
	}
	handlers := []Checker{NewGolangciLintHandler("/fake/bin")}

	diags := RunAllChecks(context.Background(), r, handlers, "/project/myapp", 5*time.Minute)
	if len(diags) < 3 {
		t.Fatalf("got %d diagnostics, want 3", len(diags))
	}
	if diags[0].File != "a.go" || diags[1].File != "a.go" || diags[2].File != "z.go" {
		t.Errorf("diagnostics not sorted by file: %v", diags)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/checkers/ -run "TestRunAll" -v`
Expected: FAIL — undefined: RunAllChecks

- [ ] **Step 3: Write implementation**

```go
// internal/checkers/orchestrator.go
package checkers

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
)

func RunAllChecks(
	parentCtx context.Context,
	r runner.CommandRunner,
	handlers []Checker,
	projectPath string,
	timeout time.Duration,
) []diagnostic.Diagnostic {
	if len(handlers) == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	results := make(chan runResult, len(handlers))
	var wg sync.WaitGroup

	for _, h := range handlers {
		wg.Add(1)
		go func(checker Checker) {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					results <- runResult{
						diagnostics: []diagnostic.Diagnostic{{
							Tool:  checker.Name(),
							Error: fmt.Sprintf("internal panic: %v", rec),
						}},
					}
				}
			}()

			toolCtx, toolCancel := context.WithTimeout(ctx, timeout)
			defer toolCancel()

			diags, err := checker.Run(toolCtx, r, projectPath)
			if err != nil {
				results <- runResult{
					diagnostics: []diagnostic.Diagnostic{{
						Tool:  checker.Name(),
						Error: formatCheckerError(toolCtx, checker.Name(), timeout, err),
					}},
				}
				return
			}
			results <- runResult{diagnostics: diags}
		}(h)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var allDiags []diagnostic.Diagnostic
	for res := range results {
		allDiags = append(allDiags, res.diagnostics...)
	}

	sort.Slice(allDiags, func(i, j int) bool {
		if allDiags[i].File != allDiags[j].File {
			return allDiags[i].File < allDiags[j].File
		}
		return allDiags[i].Line < allDiags[j].Line
	})

	return allDiags
}

func formatCheckerError(ctx context.Context, toolName string, timeout time.Duration, err error) string {
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("timed out after %s", timeout)
	}
	if ctx.Err() == context.Canceled {
		return "cancelled"
	}
	return err.Error()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/checkers/ -run "TestRunAll" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/checkers/orchestrator.go internal/checkers/orchestrator_test.go
git commit -m "feat: add parallel orchestrator with panic recovery and independent timeouts"
```

---

### Task 15: MCP Server Entry Point

**Files:**
- Create: `cmd/mcp-server-go-quality/main.go`

- [ ] **Step 1: Write the main entry point**

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
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/afshinator/mcp-server-go-quality/internal/checkers"
	"github.com/afshinator/mcp-server-go-quality/internal/config"
	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/discover"
	"github.com/afshinator/mcp-server-go-quality/internal/root"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
	"github.com/afshinator/mcp-server-go-quality/internal/version"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func main() {
	configPath := flag.String("config", "", "path to .go-quality.yaml (default: discovered at workspace root)")
	verbose := flag.Bool("verbose", false, "emit diagnostic logging to stderr")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.String())
		os.Exit(0)
	}

	if *verbose {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	} else {
		log.SetOutput(os.Stderr)
		log.SetFlags(0)
	}

	binDir, err := discover.ResolveGoBinDir()
	if err != nil {
		log.Fatalf("resolving Go binary directory: %v", err)
	}
	log.Printf("[init] Go binary directory: %s", binDir)

	versionCache := discover.NewCache()

	var cfg config.Config
	if *configPath != "" {
		cfg, err = config.Load(*configPath)
		if err != nil {
			log.Fatalf("config error: %v", err)
		}
	} else {
		cwd, _ := os.Getwd()
		cfg, _ = config.Load(filepath.Join(cwd, ".go-quality.yaml"))
	}

	s := mcpserver.NewMCPServer(
		"go-quality",
		version.String(),
		mcpserver.WithToolCapabilities(true),
	)

	registerTools(s, cfg, binDir, versionCache)

	mcpProtocolVersion := "2025-03-26"
	s.SetProtocolVersion(mcpProtocolVersion)

	log.Printf("[init] mcp-server-go-quality %s ready (MCP %s)", version.String(), mcpProtocolVersion)

	if err := mcpserver.ServeStdio(s); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func registerTools(s *mcpserver.MCPServer, cfg config.Config, binDir string, versionCache *discover.Cache) {
	s.AddTool(mcp.NewTool("run_code_checks",
		mcp.WithDescription("Run Go code quality checks in parallel. Returns unified diagnostics with file, line, message, and native tool output."),
		mcp.WithString("project_path", mcp.Description("Path to Go project root (default: server CWD)")),
		mcp.WithArray("tools", mcp.Description("Subset of checkers to run. Valid: golangci-lint, govulncheck, nilaway. Omit for all three.")),
	), makeRunAllHandler(cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool("run_lint",
		mcp.WithDescription("Run golangci-lint only. Returns lint violations, complexity, and security pattern issues."),
		mcp.WithString("project_path", mcp.Description("Path to Go project root (default: server CWD)")),
	), makeSingleHandler(toolname.GolangciLint, cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool("run_vuln_check",
		mcp.WithDescription("Run govulncheck only. Returns known CVEs in the dependency graph via call-graph analysis."),
		mcp.WithString("project_path", mcp.Description("Path to Go project root (default: server CWD)")),
	), makeSingleHandler(toolname.Govulncheck, cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool("run_nil_check",
		mcp.WithDescription("Run nilaway only. Returns potential nil panics detected via static analysis."),
		mcp.WithString("project_path", mcp.Description("Path to Go project root (default: server CWD)")),
	), makeSingleHandler(toolname.Nilaway, cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool("install_tools",
		mcp.WithDescription("Pre-install required Go quality tools with pinned versions. Call at session start."),
		mcp.WithArray("tools", mcp.Description("Subset of tools to install. Valid: golangci-lint, govulncheck, nilaway. Omit for all three.")),
	), makeInstallHandler(cfg, binDir, versionCache))
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

func resolveRequestedTools(request mcp.CallToolRequest) ([]string, error) {
	raw, ok := request.Params.Arguments["tools"]
	if !ok || raw == nil {
		return toolname.All(), nil
	}

	arr, ok := raw.([]interface{})
	if !ok {
		return nil, nil
	}

	if len(arr) == 0 {
		return toolname.All(), nil
	}

	var tools []string
	for _, v := range arr {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if !toolname.IsValid(s) {
			return nil, fmt.Errorf("unknown tool: %q. valid values: golangci-lint, govulncheck, nilaway", s)
		}
		tools = append(tools, s)
	}
	return tools, nil
}

func buildHandlers(toolNames []string, cfg config.Config, projectRoot string, binDir string) []checkers.Checker {
	workspaceModules, _ := root.WorkspaceModules(projectRoot)

	var handlers []checkers.Checker
	for _, name := range toolNames {
		switch name {
		case toolname.GolangciLint:
			h := checkers.NewGolangciLintHandler(binDir)
			if tc, ok := cfg.Tools[name]; ok {
				h.ExtraArgs = tc.ExtraArgs
			}
			handlers = append(handlers, h)
		case toolname.Govulncheck:
			h := &checkers.GovulncheckHandler{
				BinDir:            binDir,
				WorkspaceModules: workspaceModules,
			}
			if tc, ok := cfg.Tools[name]; ok {
				h.ExtraArgs = tc.ExtraArgs
			}
			handlers = append(handlers, h)
		case toolname.Nilaway:
			var includePkgs string
			if len(workspaceModules) > 0 {
				includePkgs = strings.Join(workspaceModules, ",")
			}
			h := &checkers.NilawayHandler{
				BinDir:      binDir,
				IncludePkgs: includePkgs,
			}
			if tc, ok := cfg.Tools[name]; ok {
				h.ExtraArgs = tc.ExtraArgs
			}
			handlers = append(handlers, h)
		}
	}
	return handlers
}

func ensureToolsAvailable(ctx context.Context, toolNames []string, cfg config.Config, binDir string, versionCache *discover.Cache) error {
	for _, name := range toolNames {
		tc, ok := cfg.Tools[name]
		if !ok {
			continue
		}
		result, err := discover.EnsureInstalled(ctx, versionCache, binDir, name,
			resolveModulePath(name), toolname.InstallPath(name), tc.Version)
		if err != nil {
			return fmt.Errorf("installing %s: %w", name, err)
		}
		if result.NewlyInstalled {
			log.Printf("[pre-flight] installed %s@%s", name, result.Version)
		}
	}
	return nil
}

func makeRunAllHandler(cfg config.Config, binDir string, versionCache *discover.Cache) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		projectPath, err := resolveProjectPath(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		projectRoot, err := root.Discover(projectPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("not a Go project: %v", err)), nil
		}

		toolNames, err := resolveRequestedTools(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if err := ensureToolsAvailable(ctx, toolNames, cfg, binDir, versionCache); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		r := &runner.ExecRunner{Dir: projectRoot}
		handlers := buildHandlers(toolNames, cfg, projectRoot, binDir)

		diags := checkers.RunAllChecks(ctx, r, handlers, projectRoot, cfg.ResolveTimeout())

		return marshalDiagnostics(diags)
	}
}

func makeSingleHandler(toolName string, cfg config.Config, binDir string, versionCache *discover.Cache) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		projectPath, err := resolveProjectPath(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		projectRoot, err := root.Discover(projectPath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("not a Go project: %v", err)), nil
		}

		if err := ensureToolsAvailable(ctx, []string{toolName}, cfg, binDir, versionCache); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		timeout := cfg.ResolveTimeout()
		toolCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		r := &runner.ExecRunner{Dir: projectRoot}
		handlers := buildHandlers([]string{toolName}, cfg, projectRoot, binDir)

		var (
			diags []diagnostic.Diagnostic
			runErr  error
		)

		if len(handlers) > 0 {
			diags, runErr = handlers[0].Run(toolCtx, r, projectRoot)
		}

		if runErr != nil {
			diags = []diagnostic.Diagnostic{{
				Tool:  toolName,
				Error: formatHandlerError(toolCtx, toolName, timeout, runErr),
			}}
		}

		return marshalDiagnostics(diags)
	}
}

func makeInstallHandler(cfg config.Config, binDir string, versionCache *discover.Cache) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		toolNames, err := resolveRequestedTools(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		response := InstallResult{}
		for _, name := range toolNames {
			version := "latest"
			if name == toolname.GolangciLint {
				version = "v2.11.4"
			}
			if tc, ok := cfg.Tools[name]; ok && tc.Version != "" {
				version = tc.Version
			}

			instResult, err := discover.EnsureInstalled(ctx, versionCache, binDir, name,
				resolveModulePath(name), toolname.InstallPath(name), version)
			if err != nil {
				response.Failed = append(response.Failed, FailedEntry{
					Tool:    name,
					Version: version,
					Stderr:  err.Error(),
				})
				continue
			}

			entry := ToolEntry{Tool: name, Version: instResult.Version}
			if instResult.NewlyInstalled {
				response.Installed = append(response.Installed, entry)
			} else {
				response.AlreadyPresent = append(response.AlreadyPresent, entry)
			}
		}

		return marshalInstallResult(response)
	}
}

type ToolEntry struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
}

type FailedEntry struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	Command string `json:"command"`
	Stderr  string `json:"stderr"`
}

type InstallResult struct {
	Installed      []ToolEntry   `json:"installed"`
	AlreadyPresent []ToolEntry   `json:"already_present"`
	Failed         []FailedEntry `json:"failed"`
}

func resolveModulePath(toolName string) string {
	switch toolName {
	case toolname.GolangciLint:
		return "github.com/golangci/golangci-lint/v2"
	case toolname.Govulncheck:
		return "golang.org/x/vuln"
	case toolname.Nilaway:
		return "go.uber.org/nilaway"
	default:
		return ""
	}
}

func formatHandlerError(ctx context.Context, toolName string, timeout time.Duration, err error) string {
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("timed out after %s", timeout)
	}
	if ctx.Err() == context.Canceled {
		return "cancelled"
	}
	return err.Error()
}

func marshalDiagnostics(diags []diagnostic.Diagnostic) (*mcp.CallToolResult, error) {
	if diags == nil {
		diags = []diagnostic.Diagnostic{}
	}
	b, err := json.Marshal(diags)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling results: %v", err)), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

func marshalInstallResult(result InstallResult) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(result)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling results: %v", err)), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}
```

- [ ] **Step 2: Tidy dependencies**

```bash
go mod tidy
```
Expected: fetches `mark3labs/mcp-go`

- [ ] **Step 3: Verify it compiles**

```bash
go build ./cmd/mcp-server-go-quality/
```
Expected: builds without errors

- [ ] **Step 4: Verify --version**

```bash
go run ./cmd/mcp-server-go-quality/ --version
```
Expected: prints version string

- [ ] **Step 5: Commit**

```bash
git add cmd/mcp-server-go-quality/main.go go.mod go.sum
git commit -m "feat: add MCP server entry point with 5 registered tools"
```

---

### Task 16: Install Tool Implementation

**Files:**
- Modify: `cmd/mcp-server-go-quality/main.go`
- Modify: `internal/discover/discover.go`

- [ ] **Step 1: Implement the actual go install in cmd main.go**

Replace the `runGoInstall` placeholder in `cmd/mcp-server-go-quality/main.go`:

```go
func runGoInstall(toolName, pkg, version string) error {
	pkgWithVersion := fmt.Sprintf("%s@%s", pkg, version)
	cmd := exec.Command("go", "install", pkgWithVersion)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go install %s: %w\n%s", pkgWithVersion, err, string(output))
	}
	return nil
}
```

Add `"os/exec"` to imports.

- [ ] **Step 2: Add install tool function to discover package**

Add to `internal/discover/discover.go`:

```go
func Install(binDir, toolName, modulePath, version string) error {
	pkgWithVersion := fmt.Sprintf("%s@%s", modulePath, version)
	cmd := exec.Command("go", "install", pkgWithVersion)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("install failed: %s. exit code %d. stderr: %s",
			pkgWithVersion, cmd.ProcessState.ExitCode(), string(output))
	}
	return nil
}
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./cmd/mcp-server-go-quality/
```

- [ ] **Step 4: Commit**

```bash
git add cmd/mcp-server-go-quality/main.go internal/discover/discover.go
git commit -m "feat: implement go install for tool discovery"
```

---

### Task 17: testdata/ Sample Project

**Files:**
- Create: `testdata/sample_project/go.mod`
- Create: `testdata/sample_project/go.sum` (generated)
- Create: `testdata/sample_project/.golangci.yml`
- Create: `testdata/sample_project/main.go`
- Create: `testdata/sample_project/helpers.go`

- [ ] **Step 1: Create go.mod with pinned vulnerable dependency**

```go
// testdata/sample_project/go.mod
module github.com/afshinator/mcp-server-go-quality/testdata/sample_project

go 1.25

require golang.org/x/net v0.0.0-20210226172049-4d89b558e7d3
```

- [ ] **Step 2: Create main.go with high complexity + nil deref + vulnerable import**

```go
// testdata/sample_project/main.go
package main

import (
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/html"
)

type User struct {
	Name  string
	Email string
}

func main() {
	result := getData()
	fmt.Println(result.Name)

	doc, err := html.Parse(strings.NewReader("<html><body>hello</body></html>"))
	if err != nil {
		fmt.Println("parse failed")
		return
	}
	_ = doc
}

func getData() *User {
	return nil
}

type Option func(*Config)

type Config struct {
	Verbose bool
}

func ProcessStream(w io.Writer, data []byte, options ...Option) error {
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

- [ ] **Step 3: Create helpers.go**

```go
// testdata/sample_project/helpers.go
package main

import "io"

func WithVerbose(v bool) Option {
	return func(c *Config) {
		c.Verbose = v
	}
}
```

- [ ] **Step 4: Create .golangci.yml**

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
```

- [ ] **Step 5: Generate go.sum**

```bash
cd testdata/sample_project && go mod tidy && cd -
```
Expected: creates `go.sum` with the pinned `golang.org/x/net` version

- [ ] **Step 6: Verify it compiles**

```bash
cd testdata/sample_project && go build ./... && cd -
```
Expected: compiles successfully

- [ ] **Step 7: Commit**

```bash
git add testdata/sample_project/
git commit -m "test: add sample Go project with intentional issues for all 3 tools"
```

---

### Task 18: Integration Tests

**Files:**
- Modify: `internal/checkers/golangci_lint_test.go`
- Modify: `internal/checkers/govulncheck_test.go`
- Modify: `internal/checkers/nilaway_test.go`
- Modify: `internal/checkers/orchestrator_test.go`
- Modify: `internal/discover/discover_test.go`

- [ ] **Step 1: Write integration tests**

Integration tests must resolve the Go binary directory at runtime and pass it to handlers:

```go
// Add to internal/checkers/golangci_lint_test.go

import (
	"github.com/afshinator/mcp-server-go-quality/internal/discover"
)

func TestIntegrationGolangciLint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	binDir, err := discover.ResolveGoBinDir()
	if err != nil {
		t.Skipf("skipping: cannot resolve Go bin dir: %v", err)
	}
	r := &runner.ExecRunner{Dir: "../../testdata/sample_project"}
	handler := NewGolangciLintHandler(binDir)
	ctx := context.Background()
	diags, err := handler.Run(ctx, r, "../../testdata/sample_project")
	if err != nil {
		t.Fatalf("golangci-lint failed: %v", err)
	}
	t.Logf("golangci-lint found %d issues", len(diags))
	for _, d := range diags {
		t.Logf("  %s:%d:%d [%s] %s", d.File, d.Line, d.Column, d.Severity, d.Message)
	}
}
```

```go
// Add to internal/checkers/govulncheck_test.go

func TestIntegrationGovulncheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	binDir, err := discover.ResolveGoBinDir()
	if err != nil {
		t.Skipf("skipping: cannot resolve Go bin dir: %v", err)
	}
	r := &runner.ExecRunner{Dir: "../../testdata/sample_project"}
	handler := &GovulncheckHandler{BinDir: binDir}
	ctx := context.Background()
	diags, err := handler.Run(ctx, r, "../../testdata/sample_project")
	if err != nil {
		t.Fatalf("govulncheck failed: %v", err)
	}
	t.Logf("govulncheck found %d vulnerabilities", len(diags))
	for _, d := range diags {
		if d.Error != "" {
			t.Logf("  error: %s", d.Error)
			continue
			}
		})
	}
}

func TestRelOutsideProjectReturnsAbsolute(t *testing.T) {
	projectRoot := "/project/app"

	got := Rel(projectRoot, "/other/location/file.go")

	if got != "/other/location/file.go" {
		t.Fatalf("got %q, want /other/location/file.go", got)
	}
}

func TestRelRejectsEscapeTraversal(t *testing.T) {
	projectRoot := "/project/app"

	got := Rel(projectRoot, "/project/other/file.go")

	if strings.HasPrefix(got, "..") {
		t.Fatalf("escape traversal returned: %q", got)
	}
	// Should be absolute when path is outside project tree.
	if got != "/project/other/file.go" {
		t.Fatalf("got %q, want /project/other/file.go", got)
	}
}
```

```go
// Add to internal/checkers/nilaway_test.go

func TestIntegrationNilaway(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	binDir, err := discover.ResolveGoBinDir()
	if err != nil {
		t.Skipf("skipping: cannot resolve Go bin dir: %v", err)
	}
	r := &runner.ExecRunner{Dir: "../../testdata/sample_project"}
	handler := &NilawayHandler{BinDir: binDir}
	ctx := context.Background()
	diags, err := handler.Run(ctx, r, "../../testdata/sample_project")
	if err != nil {
		t.Fatalf("nilaway failed: %v", err)
	}
	t.Logf("nilaway found %d nil panics", len(diags))
	for _, d := range diags {
		t.Logf("  %s:%d:%d %s", d.File, d.Line, d.Column, d.Message)
	}
}
```

```go
// Add to internal/checkers/orchestrator_test.go

func TestIntegrationRunAllChecks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	binDir, err := discover.ResolveGoBinDir()
	if err != nil {
		t.Skipf("skipping: cannot resolve Go bin dir: %v", err)
	}
	r := &runner.ExecRunner{Dir: "../../testdata/sample_project"}
	handlers := []Checker{
		NewGolangciLintHandler(binDir),
		&GovulncheckHandler{BinDir: binDir},
		&NilawayHandler{BinDir: binDir},
	}
	ctx := context.Background()
	diags := RunAllChecks(ctx, r, handlers, "../../testdata/sample_project", 2*time.Minute)
	if len(diags) == 0 {
		t.Error("expected at least one diagnostic from test project")
	}
	for _, d := range diags {
		if d.Error != "" {
			t.Logf("[%s] ERROR: %s", d.Tool, d.Error)
			continue
		}
		t.Logf("[%s] %s:%d:%d %s", d.Tool, d.File, d.Line, d.Column, d.Message)
	}
}
```

Add imports for `"time"`, `"context"`, `"github.com/afshinator/mcp-server-go-quality/internal/runner"`.

```go
// Add to internal/discover/discover_test.go

func TestIntegrationResolveAndInstall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	binDir, err := ResolveGoBinDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("binDir: %s", binDir)

	version, err := ReadInstalledVersion(binDir, "golangci-lint", "github.com/golangci/golangci-lint/v2")
	if err != nil {
		t.Logf("golangci-lint not installed or version check failed: %v", err)
	} else {
		t.Logf("golangci-lint version: %s", version)
	}
}
```

- [ ] **Step 2: Run integration tests**

```bash
go test -timeout 10m -run Integration ./internal/checkers/ ./internal/discover/ -v
```
Expected: PASS — integration tests run against testdata/sample_project

- [ ] **Step 3: Tidy and re-run unit tests**

```bash
go mod tidy
go test -short ./... -v
```
Expected: PASS — all unit tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/checkers/ internal/discover/ go.mod go.sum
git commit -m "test: add integration tests against testdata/sample_project"
```

---

### Task 19: AGENTS.md

**Files:**
- Create: `AGENTS.md`

- [ ] **Step 1: Verify source and copy AGENTS.md**

The AGENTS.md content is defined at the end of `docs/superpowers/specs/spec-v3.md` (the section after the system-reminder block). Confirm the file exists, then copy:

```bash
test -f docs/superpowers/specs/AGENTS.md && cp docs/superpowers/specs/AGENTS.md AGENTS.md || echo "AGENTS.md source not found — content is embedded in spec-v3.md; extract it manually"
```

- [ ] **Step 2: Commit**

```bash
git add AGENTS.md
git commit -m "docs: add AGENTS.md with MCP tool usage guide"
```

---

### Task 20: Makefile + Quality Suite

**Files:**
- Create: `Makefile`

- [ ] **Step 1: Create Makefile**

```makefile
APP_NAME := mcp-server-go-quality
CMD_DIR := ./cmd/$(APP_NAME)

.PHONY: build test test-all lint vet clean fmt run

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
	gofumpt -w ./
	goimports -w ./

run:
	go run $(CMD_DIR)
```

- [ ] **Step 2: Build binary**

```bash
make build
```
Expected: produces `bin/mcp-server-go-quality`

- [ ] **Step 3: Run unit tests**

```bash
make test
```
Expected: PASS (all unit tests, skipping integration)

- [ ] **Step 4: Run go vet**

```bash
make vet
```
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add Makefile
git commit -m "chore: add Makefile with build, test, lint, vet targets"
```

---

### Task 21: Quality Suite (golangci-lint + gofumpt + goimports)

- [ ] **Step 1: Create .golangci.yml for the server itself**

```yaml
# .golangci.yml
version: "2"
run:
  timeout: 2m
linters:
  default: standard
  enable:
    - gocyclo
    - gocognit
    - gosec
```

- [ ] **Step 2: Run formatting**

```bash
gofumpt -w ./
goimports -w ./
```

- [ ] **Step 3: Run vet**

```bash
go vet ./...
```
Expected: no errors

- [ ] **Step 4: Run all tests one final time**

```bash
go test -short ./... -v
```
Expected: all PASS

- [ ] **Step 5: Final build**

```bash
go build ./cmd/mcp-server-go-quality/
```
Expected: builds without errors

- [ ] **Step 6: Commit**

```bash
git add .golangci.yml
git commit -m "chore: add .golangci.yml and run quality suite"
```

---

### Dependency Order

```
Task 1  (module + dirs)
  ↓
Task 2  (Diagnostic)  ──┐
Task 3  (Version)     ──┤
Task 4  (PathUtil)    ──┤
Task 5  (Runner)      ──┤
Task 6  (Root)        ──┤
Task 7  (Config)      ──┤
Task 8  (ToolName)    ──┤
Task 9  (Discover)    ──┤
  ↓                        ↓
Task 10 (Checker interface)
  ↓
Task 11 (golangci-lint) ──┐
Task 12 (govulncheck)  ──┤
Task 13 (nilaway)      ──┤
  ↓                        ↓
Task 14 (Orchestrator)
  ↓
Task 15 (MCP server)  ←── Task 16 (Install impl)
  ↓
Task 17 (testdata)
  ↓
Task 18 (Integration tests)
  ↓
Task 19 (AGENTS.md)
  ↓
Task 20 (Makefile)
  ↓
Task 21 (Quality suite)
```

Tasks 2–9 can be done in any order (no mutual dependencies) and are ideal for parallel subagent dispatch.

---

## Known Implementation Risks

These items are validated against the spec and will need attention during implementation:

1. **MCP protocol version API:** The `SetProtocolVersion("2025-03-26")` call in Task 15 may not exist on the `mark3labs/mcp-go` `MCPServer` type. The correct mechanism may be passing it via `server.WithProtocolVersion("2025-03-26")` in the constructor options. Validate against the installed library version.

2. **Stdout preservation on tool failure:** ExecRunner now returns stdout bytes even when the process exits non-zero. All parsers must handle partial/empty output gracefully. `parseGovulncheckOutput` already handles this (scanner on empty reader produces no findings). `parseNilawayOutput` checks `len(output) == 0` at the top. `parseGolangciLintOutput` will fail `json.Unmarshal` on empty or partial output, which is the correct behavior (error Diagnostic).

3. **BinDir is mandatory:** All handler constructors require `binDir` (resolved via `discover.ResolveGoBinDir()` at server startup). Mock tests pass `"/fake/bin"` — the mock runner matches on base name only. Integration tests resolve the real bin dir at runtime. No fallback to PATH exists anywhere in handler code.

4. **go.work parser limitations:** `parseUseDirectives` handles single-line `use`, block `use (...)`, and `//` comments. It does not handle `/* */` block comments, which are syntactically valid but virtually never used in go.work files.

5. **Config merge edge case:** YAML that explicitly sets a tool's `extra_args: []` (empty array) will not override existing defaults because `len(tc.ExtraArgs) > 0` is the merge gate. An empty array in YAML means "no extra args," which is the same as the default. If a user explicitly wants empty extra_args to override a non-empty default, the merge condition needs adjustment. This is a minor edge case — default extra_args are always empty.

6. **Column 0-indexing assumption:** The `Column` field uses `omitempty` with 0 meaning "unknown." No supported tool uses 0-based indexing. Documented in the spec and Diagnostic type.

7. **Extra_args ordering:** Server-managed flags are prepended before `ExtraArgs` in each handler's `Run` method. Reserved flag validation in `config.Validate()` prevents `ExtraArgs` from containing conflicting flags.

---

### Verification Commands

```bash
# Unit tests only
go test -short ./...

# Full test suite
go test -timeout 10m ./...

# Build
go build ./cmd/mcp-server-go-quality/

# Lint (after golangci-lint is installed)
golangci-lint run ./...

# Vet
go vet ./...

# Format
gofumpt -w ./
goimports -w ./

# Version
go run ./cmd/mcp-server-go-quality/ --version
```
