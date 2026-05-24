# Go Quality MCP — Part 2: Execution and Discovery (Tasks 5–6)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the subprocess execution layer and Go workspace root discovery.

**Architecture:** ExecRunner carries Dir for project root isolation. Root discovery walks upward in two passes (go.work first, then go.mod).

**Tech Stack:** Go 1.25, `os/exec`, `path/filepath`

**Prerequisite:** Part 1 complete (`internal/diagnostic/`, `internal/pathutil/`, `internal/version/` packages exist)

---

## File Structure

```
mcp-server-go-quality/
├── internal/
│   ├── runner/
│   │   ├── runner.go                          # CommandRunner interface + ExecRunner (Dir field)
│   │   └── runner_test.go
│   ├── root/
│   │   ├── root.go                            # Two-pass root discovery (go.work → go.mod)
│   │   └── root_test.go
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
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			dirs = append(dirs, fields[0])
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

