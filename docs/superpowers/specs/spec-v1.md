# mcp-server-go-quality — Design Spec v1

**Date:** 2026-05-23
**Status:** draft

## Overview

An MCP (Model Context Protocol) server that wraps Go code quality tools for consumption
by AI agents (Claude Code, Codex, etc.) and CI pipelines. The server runs linting,
vulnerability scanning, and nil-panic detection on any Go project, returning structured
JSON diagnostics an agent can parse and act on programmatically.

### Why MCP Wrapping

Individual CLI tools (`golangci-lint`, `govulncheck`, `nilaway`) are developer-facing.
An LLM agent needs a machine-facing API with:
- Single entry point instead of 3 different CLI invocations
- Auto-discovery and auto-installation of missing tools
- Parallel execution with unified results
- Consistent error handling and timeouts

## Tools Included

Based on overlap analysis (see `docs/tools-research.md`):

| Tool | Purpose | Output Format | Built Into golangci-lint? |
|---|---|---|---|
| `golangci-lint` | Linting + complexity (`gocyclo`, `gocognit`) + security patterns (`gosec`) | JSON (`--out-format=json`) | N/A |
| `govulncheck` | CVE scanning via call-graph analysis | JSON-lines (`-json`) | No |
| `nilaway` | Deep inter-procedural nil-panic detection | JSON (`-json -pretty-print=false`) | No (plugin possible) |

`gocyclo` and `gocognit` are excluded as standalone tools — they are built into
golangci-lint and activated via `.golangci.yml` config.

## Architecture

Three-layer functional design:

```
Transport Layer          Tool Handlers             Subprocess & Parsing
┌──────────────────┐    ┌────────────────┐    ┌──────────────────────┐
│ JSON-RPC / stdio  │    │ runLint(path)  │    │ exec(glangci-lint)   │
│ 5 registered tools│───▶│ runVuln(path)  │───▶│ exec(govulncheck)    │
│ Tool dispatch     │    │ runNil(path)   │    │ exec(nilaway)        │
│                   │    │ runAll(path)   │    │ parse → normalize   │
│                   │    │ installTools() │    │ gather results       │
└──────────────────┘    └────────────────┘    └──────────────────────┘
```

### Design Rules
- **Prefer pure functions** — handlers are designed as `func runX(path string) ([]Diagnostic, error)` with no hidden state per call
- **Parallel by default** — `runAll` fires 3 goroutines, merges results
- **One file per tool** — `golangci_lint.go`, `govulncheck.go`, `nilaway.go`
- **Single entry point** — `cmd/mcp-server-go-quality/main.go`

### MCP Tools Registered

| Tool Name | Description |
|---|---|
| `run_code_checks` | Run all 3 checkers in parallel, return unified results |
| `run_lint` | Run golangci-lint only |
| `run_vuln_check` | Run govulncheck only |
| `run_nil_check` | Run nilaway only |
| `install_tools` | Pre-install all required Go tools (pinned versions) |

Each tool accepts:
- `project_path` (optional, string) — defaults to server's CWD

## Data Flow

```
Agent calls run_code_checks →
  Parse project_path (default CWD)
  Validate Go project (go.mod or go.work exists)
  Pre-flight: synchronous tool discovery
    → install any missing tools sequentially (avoids go install race)
  Fire 3 goroutines:
    go runLint(path)
    go runVuln(path)
    go runNil(path)
  Each handler:
    1. exec.Command with timeout context
    2. Parse tool's native JSON → extract Location fields
    3. Return wrapped Diagnostic
  Collect all results from channels
  Sort by file→line
  Return JSON array
```

## Output Schema

Each diagnostic carries extracted location fields for uniform agent navigation,
plus the complete native tool output for deep context:

```go
type Diagnostic struct {
    Tool    string          `json:"tool"`    // "golangci-lint" | "govulncheck" | "nilaway"
    File    string          `json:"file"`    // relative path from project root ("" if unknown)
    Line    int             `json:"line"`    // 0 if unknown
    Message string          `json:"message"` // human-readable summary extracted from native output
    Error   string          `json:"error"`   // "" on success; error message on failure
    Native  json.RawMessage `json:"native"`  // tool's complete native JSON output
}
```

The `File`, `Line`, and `Message` fields are extracted from each tool's native output
so the AI agent can navigate to every issue using a single consistent loop.
The `Native` field preserves the full raw output for deep context, remediation
instructions (govulncheck references, golangci-lint SuggestedFixes), and tool-specific
fields the agent may need.

When `Error` is set, `File`/`Line`/`Message`/`Native` are zero-valued.

### Tool-Specific Native Formats

**golangci-lint** (`--out-format=json`):
```json
{
  "Issues": [{
    "FromLinter": "gocognit",
    "Text": "cognitive complexity 18 of func ... is high (> 15)",
    "Severity": "warning",
    "Pos": {"Filename": "cmd/main.go", "Line": 115, "Column": 1},
    "SourceLines": ["func ProcessStream(...) {"],
    "SuggestedFixes": []
  }]
}
```
Note: The `--out-format=json` flag format was verified against golangci-lint v2 docs.
Exact `Issues` wrapper structure and gosec field names must be validated against
installed v2.11.4 on first run.

**govulncheck** (`-json` — JSON-lines, one object per line):

The handler must read stdout line-by-line with `bufio.Scanner` since govulncheck emits
newline-delimited JSON (NDJSON). Each line is an independent object — they cannot be
unmarshalled as a single JSON document.

```jsonl
{"config": {...}}
{"SBOM": {...}}
{"osv": {"id": "GO-2026-4918", "summary": "...", "aliases": ["CVE-..."]}}
{"finding": {
  "osv": "GO-2026-4918",
  "fixed_version": "v1.25.10",
  "trace": [
    {"module": "stdlib", "package": "net/http",
     "position": {"filename": "src/net/http/client.go", "line": 586, "column": 18}},
    {"module": "github.com/user/project", "package": "...httpclient",
     "position": {"filename": "internal/httpclient/client.go", "line": 78, "column": 25}}
  ]
}}
```

Extraction rules for the unified `Diagnostic` fields:
- `File`/`Line`: from the last `finding.trace` entry that has a `position` (the call site in user code)
- `Message`: from `osv.summary` or `finding.osv` ID
- `Native`: the raw `finding` object + associated `osv` object

**nilaway** (`-json -pretty-print=false` — verified working on installed version):

```json
{
  "<package-path>": {
    "nilaway": [{
      "posn": "/absolute/path/to/file.go:288:4",
      "end": "/absolute/path/to/file.go:288:4",
      "message": "Potential nil panic detected..."
    }]
  }
}
```

Extraction rules:
- `File`: strip project root prefix from `posn`, parse out filename
- `Line`/`Column`: parsed from `posn` (`file.go:line:col`)
- `Message`: from `message` field, first sentence
- `Native`: the raw nilaway error object

## Tool Discovery & Installation

### Auto-Install Rules

1. On first call, discover each tool via `exec.LookPath`
2. If missing, run `go install <pkg>@<version>` once
3. Log a clear message: "Installing <tool>@<version>... this happens once."
4. If install fails, return error with the exact `go install` command to run
5. The `install_tools` MCP tool lets agents pre-install proactively

### Version Pinning

Versions are read from `.go-quality.yaml` in the target project. Falls back to `latest`.

```yaml
# .go-quality.yaml (project root)
timeout: 5m                     # per-tool deadline (default: 5m)

tools:
  golangci-lint:
    version: latest              # or "v2.3.0"
    extra_args: ["--fast"]       # appended to required flags
  govulncheck:
    version: latest
    extra_args: []
  nilaway:
    version: latest              # or "v0.19.0"
    extra_args: ["--exclude-pkgs=github.com/myorg/vendor"]
```

No file present → all tools run at latest with zero extra args.
Server's required flags (like `--out-format=json`) are always applied and cannot be overridden.

## Configuration

The server has two config sources:

1. **MCP client config** (`.mcp.json` or equivalent) — launches the binary, may pass env vars
2. **`.go-quality.yaml`** in the target project — tool versions and extra args only

The server does NOT duplicate golangci-lint's linter config. That lives in `.golangci.yml`
as normal.

## CLI

```
mcp-server-go-quality
  --config PATH     path to .go-quality.yaml (default: ./.go-quality.yaml)
  --verbose         emit diagnostic logging to stderr
  --version         print version (+ commit hash, + dirty flag)
```

The `--version` flag uses `runtime/debug.ReadBuildInfo()` for VCS metadata, matching
the pattern used by cryptospect-cli.

## Error Handling

| Scenario | Behavior |
|---|---|
| Tool not installed | Auto-install once (sequentially, pre-flight — see below). If install fails → Diagnostic with `error` field |
| Tool exit code ≠ 0 | Diagnostic with `error` field + stderr content |
| Tool times out | Diagnostic with `error`: "timed out after <duration>" |
| Tool returns valid JSON, no findings | Empty `Native` — success |
| Tool returns non-JSON on stdout | Diagnostic with `error`: "unexpected output format from <tool>" |
| `project_path` doesn't exist | Top-level error response, no diagnostics |
| `project_path` has no `go.mod` or `go.work` | Top-level error: "not a Go project" |
| Tool not installed + go not on PATH | Top-level error: "go binary not found" |

### Pre-Flight Sequential Install

Before `run_code_checks` spawns parallel goroutines, a synchronous pass discovers all
required tools. If any are missing, they are installed **sequentially** (one at a time)
to avoid Go module cache lock races from concurrent `go install` calls. The diagnostic
goroutines use already-installed tools — they never trigger install inside the parallel
phase.

### Go Workspace Support

When the target directory contains a `go.work` file (Go 1.18+ workspace), the server
runs tools from that directory's context. All `./...` patterns resolve against the
modules listed in `go.work`. The server checks for `go.work` first, falling back to
`go.mod`.

### Configurable Timeout

```yaml
# .go-quality.yaml
timeout: 10m  # default: 5m
```

Per-tool timeout in the config file. Useful for large monorepos where golangci-lint
or govulncheck database downloads can exceed the default 5 minutes.

Stderr is always captured and included in the error message on failure.

## Testing Strategy (TDD)

- Red-green-refactor cycle enforced for every feature
- **Unit tests:** pure functions (parsers, path resolution, config loading)
- **Integration tests:** real `exec.Command` against a small `testdata/` Go project
- **Tool-specific test helpers:** `testdata/sample_project/` with intentional issues for
  each tool to detect
- **Edge cases:** missing tools, broken config, empty project, non-Go directory, timeout
- Mockable `CommandRunner` interface for handlers to swap real exec with test doubles

## Implementation Plan

Will be generated by the `writing-plans` skill after this spec is approved.

## References

- `docs/tools-research.md` — detailed tool overlap analysis and output format research
- `/project/cryptospect-cli/internal/version/version.go` — version pattern to follow
- `/project/cryptospect-cli/.golangci.yml` — existing golangci-lint config (reference)
