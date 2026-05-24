# Go Quality MCP — Part 4: Checker Handlers (Tasks 10–13)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement all three tool handlers: golangci-lint JSON parser, govulncheck NDJSON parser with vulnerability DB lock retry, nilaway parser with workspace module resolution.

**Architecture:** Each handler satisfies the `Checker` interface. Handlers receive a `CommandRunner` for subprocess execution and `projectPath` for path normalization. ExtraArgs from config are appended to required flags.

**Tech Stack:** Go 1.25, `bufio.Scanner` for NDJSON, `path/filepath` for path normalization

**Prerequisite:** Parts 1–3 complete (all foundational packages exist)

---

## File Structure

```
mcp-server-go-quality/
├── internal/
│   └── checkers/
│       ├── checker.go                         # Checker interface + runResult type
│       ├── golangci_lint.go                   # golangci-lint handler + parser
│       ├── golangci_lint_test.go
│       ├── govulncheck.go                     # govulncheck handler + NDJSON parser + DB lock retry
│       ├── govulncheck_test.go
│       ├── nilaway.go                         # nilaway handler + parser + workspace module resolution
│       └── nilaway_test.go
```

---

### Task 10: Checker Interface

**Files:**
- Create: `internal/checkers/checker.go`

The Checker interface is the contract every tool handler must satisfy. Defined once here, consumed by Tasks 11-13 handler implementations and Task 14 orchestrator. No tests file needed — the interface is exercised by handler and orchestrator tests.

- [ ] **Step 1: Write the interface**

```go
// internal/checkers/checker.go
package checkers

import (
	"context"

	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
)

type Checker interface {
	Name() string
	Run(ctx context.Context, r runner.CommandRunner, projectPath string) ([]diagnostic.Diagnostic, error)
}

type runResult struct {
	diagnostics []diagnostic.Diagnostic
	err         error
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/checkers/checker.go
git commit -m "feat: add Checker interface contract for tool handlers"
```

---

### Task 11: golangci-lint Handler + JSON Parser

**Files:**
- Create: `internal/checkers/golangci_lint.go`
- Create: `internal/checkers/golangci_lint_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/checkers/golangci_lint_test.go
package checkers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
)

// mockRunner is a shared test double used by all checker tests (Tasks 11-14).
// It maps command+first-arg to deterministic output bytes.
type mockRunner struct {
	outputs map[string][]byte
	err     error
}

func (m *mockRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	if len(args) > 0 {
		key := name + ":" + args[0]
		if out, ok := m.outputs[key]; ok {
			return out, nil
		}
	}
	return nil, nil
}

func TestParseGolangciLintOutput(t *testing.T) {
	input := `{"Issues": [
		{"FromLinter":"gocognit","Text":"cognitive complexity 18 is high (> 15)","Severity":"warning","Pos":{"Filename":"cmd/main.go","Line":115,"Column":1}},
		{"FromLinter":"gosec","Text":"G601: Implicit memory aliasing in for loop","Severity":"error","Pos":{"Filename":"internal/auth/auth.go","Line":42,"Column":10}}
	]}`

	diags, err := parseGolangciLintOutput([]byte(input), "/project/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics, want 2", len(diags))
	}

	d0 := diags[0]
	if d0.Tool != toolname.GolangciLint {
		t.Errorf("tool = %q, want %s", d0.Tool, toolname.GolangciLint)
	}
	if d0.File != "cmd/main.go" {
		t.Errorf("file = %q, want cmd/main.go", d0.File)
	}
	if d0.Line != 115 {
		t.Errorf("line = %d, want 115", d0.Line)
	}
	if d0.Column != 1 {
		t.Errorf("column = %d, want 1", d0.Column)
	}
	if d0.Severity != "warning" {
		t.Errorf("severity = %q, want warning", d0.Severity)
	}
	if d0.Message != "cognitive complexity 18 is high (> 15)" {
		t.Errorf("message = %q", d0.Message)
	}
	if d0.Native == nil {
		t.Error("native should not be nil")
	}
}

func TestParseGolangciLintEmptyOutput(t *testing.T) {
	input := `{"Issues": []}`
	diags, err := parseGolangciLintOutput([]byte(input), "/project/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Errorf("got %d diagnostics, want 0", len(diags))
	}
}

func TestGolangciLintHandlerWithMock(t *testing.T) {
	input := `{"Issues": [{"FromLinter":"gocritic","Text":"test","Severity":"warning","Pos":{"Filename":"main.go","Line":1,"Column":1}}]}`
	r := &mockRunner{
		outputs: map[string][]byte{
			"golangci-lint:run": []byte(input),
		},
	}
	handler := NewGolangciLintHandler("/fake/bin")
	diags, err := handler.Run(context.Background(), r, "/project/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diags))
	}
	if diags[0].Tool != toolname.GolangciLint {
		t.Errorf("tool = %q", diags[0].Tool)
	}
}

func TestParseGolangciLintPathNormalization(t *testing.T) {
	input := `{"Issues": [
		{"FromLinter":"test","Text":"test","Severity":"warning","Pos":{"Filename":"/project/myapp/cmd/main.go","Line":1,"Column":1}}
	]}`
	diags, err := parseGolangciLintOutput([]byte(input), "/project/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if diags[0].File != "cmd/main.go" {
		t.Errorf("file = %q, want cmd/main.go", diags[0].File)
	}
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

