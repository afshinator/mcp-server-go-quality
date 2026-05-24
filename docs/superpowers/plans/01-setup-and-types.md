# Go Quality MCP — Part 1: Setup and Types (Tasks 1–4)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Initialize Go module, directory structure, and foundational types (Diagnostic, Version, Path utilities).

**Architecture:** Pure types and utilities with zero internal dependencies. These packages form the base layer that all subsequent sub-plans depend on.

**Tech Stack:** Go 1.25, `encoding/json`, `runtime/debug`, `path/filepath`

**Prerequisite:** None — this is the starting point for the entire project.

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
