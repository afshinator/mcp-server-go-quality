<!-- repo image -->

# mcp-server-go-quality

**One MCP server for golangci-lint, govulncheck, and nilaway — a unified `Diagnostic[]` array with consistent file:line:column navigation, parallel execution, and zero-config auto-install.**

---

[![Build](https://img.shields.io/badge/build-passing-brightgreen)]()
[![Go](https://img.shields.io/badge/Go-1.25-blue)]()
[![MCP](https://img.shields.io/badge/MCP-2025--03--26-blueviolet)]()
[![License](https://img.shields.io/badge/license-MIT-green)]()

---

## What it does

Designed for **AI coding agents** (Claude Code, Codex, OpenCode) that need to check Go code quality without managing three separate CLIs, parsing three incompatible output formats, or waiting for tools to install.

This MCP server exposes the three essential Go quality tools as a single interface. Agents call one tool (`run_code_checks`) and receive one flat, sorted `Diagnostic[]` array — regardless of how many checkers ran. Tools that aren't installed are silently fetched in a pre-flight step. All three run in parallel under independent timeout budgets.

It also works in CI pipelines where you want a single source of truth for linting, vulnerability scanning, and nil-panic detection.

---

## Why not just run the tools directly?

| Concern | Raw CLI | This server |
|---|---|---|
| Entry points | 3 separate `go install` + `go run` invocations | 1 MCP tool call |
| Output format | 3 incompatible schemas (JSON, NDJSON, structured map) | 1 unified `Diagnostic[]` array |
| Tool install | Manual per tool, per machine | Pre-flight auto-install with version pinning |
| Concurrency | Sequential by default | Parallel goroutines, per-tool timeouts |
| Error handling | Parse exit codes and stderr manually | Canonical `error` field per diagnostic, panic recovery per handler |
| Path normalization | Raw absolute paths from each tool | Relative to project root, escape-traversal rejected |
| Workspace support | Manual `go.work` parsing | Two-pass root discovery (`go.work` > `go.mod`) |

---

## Tools bundled

| Tool | Version | Checks |
|---|---|---|
| [golangci-lint](https://golangci-lint.run) | **v2.11.4** (pinned) | Lint violations, `gocyclo`/`gocognit` complexity, `gosec` security patterns. Pinned because its JSON schema changed across major versions. |
| [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) | latest | Known CVEs in your dependency graph via call-graph analysis. Only reports vulnerabilities **reachable** from your code. |
| [nilaway](https://github.com/uber-go/nilaway) | latest | Inter-procedural nil-panic paths the Go compiler won't catch. Whole-program analysis, no annotations required to start. |

---

## MCP tools exposed

| Tool | Description |
|---|---|
| `run_code_checks` | Run all 3 checkers in parallel (or a subset via `tools` param). Returns sorted `Diagnostic[]`. |
| `run_lint` | Run golangci-lint only. Lighter incremental re-check after fixing a lint issue. |
| `run_vuln_check` | Run govulncheck only. Lighter incremental re-check after upgrading a dependency. |
| `run_nil_check` | Run nilaway only. Lighter incremental re-check after fixing a nil panic. |
| `install_tools` | Pre-install all three tools with pinned/latest versions. **Call this once at session start.** |

---

## Quick start

### With Claude Code

```bash
claude mcp add go-quality -- go run github.com/afshinator/mcp-server-go-quality/cmd/mcp-server-go-quality@latest
```

Then call `install_tools` once at session start. After that, `run_code_checks` for every Go project.

### With any MCP client

```json
{
  "mcpServers": {
    "go-quality": {
      "command": "mcp-server-go-quality",
      "args": [],
      "transport": "stdio"
    }
  }
}
```

Install the binary with `go install github.com/afshinator/mcp-server-go-quality/cmd/mcp-server-go-quality@latest`.

---

## Recommended agent workflow

1. **Install tools once** — call `install_tools` at session start. If any installation fails, the response includes the failed tool, version, and stderr.
2. **Run checks** — call `run_code_checks` with `project_path` set to the project root (or any subdirectory — the server walks up to find `go.work` or `go.mod`).
3. **Process diagnostics** — iterate the `Diagnostic[]` array, filter by `severity` (`"error"` > `"warning"` > `""`), navigate to `file:line:column`, and consult `native` for full raw output (suggested fixes in golangci-lint, CVE IDs and call traces in govulncheck, nil-propagation chains in nilaway).

---

## Configuration (`.go-quality.yaml`)

Place at the project root. All fields optional — defaults are shown below:

```yaml
# Per-tool timeout budget. Each of the 3 tools gets this independently.
# Default: 5m. Increase for large monorepos or first-run vuln DB downloads.
timeout: 5m

tools:
  golangci-lint:
    version: v2.11.4       # Pinned default. Override with caution — JSON schema changes.
    extra_args: []         # Appended after required flags. Reserved flags rejected.
  govulncheck:
    version: latest
    extra_args: []
  nilaway:
    version: latest
    extra_args: ["--exclude-pkgs=github.com/myorg/vendor"]
```

Precedence: CLI `--config` flag > `.go-quality.yaml` at project root > compiled-in defaults.

---

## Output schema

Every tool returns a flat array of this shape, sorted by `file` then `line`:

```json
[
  {
    "tool": "golangci-lint",
    "file": "cmd/main.go",
    "line": 115,
    "column": 1,
    "severity": "warning",
    "message": "cognitive complexity 18 is high (> 15)",
    "error": "",
    "native": {"FromLinter": "gocognit", "Text": "...", "SourceLines": ["..."], "SuggestedFixes": [...]}
  }
]
```

| Field | Notes |
|---|---|
| `severity` | Absent (not `""`) for govulncheck and nilaway — they have no severity concept |
| `native` | `null` for error diagnostics; full raw tool output otherwise |
| `error` | Non-empty on tool failure or panic. Check this first before reading `file`/`line` |

---

## Go workspace support

Supports single-module `go.mod` projects and `go.work` multi-module workspaces. Pass any subdirectory as `project_path` — the server walks up to find `go.work` first, then `go.mod`. All tools run from the discovered root with `./...` patterns. Nilaway automatically collects module paths from `use` directives and passes them via `-include-pkgs`.

---

## Requirements

- Go **1.22+** on `PATH`
- The three quality tools are **auto-installed** into `$GOBIN` (or `$GOPATH/bin`, or `$HOME/go/bin` — resolved at startup). No manual `go install` needed.

---

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). This project enforces TDD (red-green-refactor); every source file has a companion `_test.go` file in the same package. Integration tests run against [`testdata/sample_project/`](testdata/sample_project/) — a small Go module with intentional issues for all three tools. Run `make test` for unit tests and `make test-all` for the full suite including integration.

---

MIT License
