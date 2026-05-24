# Go Quality MCP — Part 5: Orchestration and Server (Tasks 14–21)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire everything together — parallel orchestrator, MCP server with 5 registered tools, install handler, testdata project, integration tests, AGENTS.md, Makefile, quality suite.

**Architecture:** Orchestrator dispatches per-tool goroutines with independent timeouts and panic recovery. MCP server uses mark3labs/mcp-go for stdio JSON-RPC transport.

**Tech Stack:** Go 1.25, `mark3labs/mcp-go`, golangci-lint, gofumpt, goimports

**Prerequisite:** Parts 1–4 complete (all internal packages exist)

---

## File Structure

```
mcp-server-go-quality/
├── cmd/mcp-server-go-quality/main.go          # Entry point, MCP server bootstrap, CLI flags
├── internal/
│   └── checkers/
│       ├── orchestrator.go                    # runAll parallel dispatch + pre-flight + panic recovery
│       └── orchestrator_test.go
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

audit:
	govulncheck -json ./...

nilcheck:
	nilaway -json -pretty-print=false ./...

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

### Task 21: Quality Suite (golangci-lint + govulncheck + nilaway + formatting)

**Note:** This task runs the tools on the project itself. The tools must be installed first — this is a one-shot manual quality pass during development, not an exercise of the MCP server (which is now built). The server will handle tool lifecycle in production; here we just `go install` directly.

- [ ] **Step 1: Install the quality tools**

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4
go install golang.org/x/vuln/cmd/govulncheck@latest
go install go.uber.org/nilaway/cmd/nilaway@latest
```

- [ ] **Step 2: Create .golangci.yml for the server itself**

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

- [ ] **Step 3: Run formatting**

```bash
gofumpt -w ./
goimports -w ./
```

- [ ] **Step 4: Run vet**

```bash
go vet ./...
```
Expected: no errors

- [ ] **Step 5: Run all tests one final time**

```bash
go test -short ./... -v
```
Expected: all PASS

- [ ] **Step 6: Run govulncheck on the project itself**

```bash
govulncheck -json ./...
```
Expected: checks the project's own dependency graph for known CVEs. There may be findings against pinned or indirect dependencies; review and address or document.

- [ ] **Step 7: Run nilaway on the project itself**

```bash
nilaway -json -pretty-print=false ./...
```
Expected: nilaway scans the project for nil-panic paths. Findings against this project's own code must be fixed before considering the quality suite complete.

- [ ] **Step 8: Run golangci-lint**

```bash
golangci-lint run ./...
```
Expected: no lint violations.

- [ ] **Step 9: Final build**

```bash
go build ./cmd/mcp-server-go-quality/
```
Expected: builds without errors

- [ ] **Step 10: Commit**

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
