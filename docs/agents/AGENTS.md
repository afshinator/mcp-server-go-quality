# AGENTS.md — mcp-server-go-quality

This file tells AI agents how to use this MCP server. Load it once per session.
For error handling tables, native output examples, and troubleshooting, see
[reference.md](reference.md).

---

## What this server does

Runs three Go code quality tools against a Go project and returns a unified
`Diagnostic[]` array:

| Tool | What it finds |
|---|---|
| `golangci-lint` | Lint violations, cyclomatic/cognitive complexity, security patterns (gosec) |
| `govulncheck` | Known CVEs in your dependency graph via call-graph analysis |
| `nilaway` | Inter-procedural nil-panic paths the compiler won't catch |

---

## First step: install_tools

**Call `install_tools` at session start.** It's a fast no-op if tools are already
installed. It returns `installed`, `already_present`, and `failed` lists.

```json
{ "tool": "install_tools" }
{ "tool": "install_tools", "tools": ["golangci-lint", "govulncheck"] }
```

**If `failed` is non-empty:** report each entry's `command` and `stderr` to the user.
Do not proceed with checks for the failed tool.

---

## Running checks

All check tools take an optional `project_path` (defaults to server CWD). The server
walks up from `project_path` to find `go.work` or `go.mod`.

```json
// Full scan (all 3 tools)
{ "tool": "run_code_checks", "project_path": "/home/user/myproject" }

// Subset scan
{ "tool": "run_code_checks", "tools": ["golangci-lint", "govulncheck"] }

// Single-tool shortcuts
{ "tool": "run_lint",       "project_path": "/home/user/myproject" }
{ "tool": "run_vuln_check", "project_path": "/home/user/myproject" }
{ "tool": "run_nil_check",  "project_path": "/home/user/myproject" }
```

Omitting `tools` or passing an empty array runs all three.

---

## Response: the Diagnostic array

Every check call returns a flat `Diagnostic[]` sorted by `file` then `line`.

```typescript
interface Diagnostic {
  tool:     "golangci-lint" | "govulncheck" | "nilaway";
  file:     string;          // relative path from project root; "" if unknown
  line:     number;          // 0 if unknown
  column:   number;          // 0 if unknown; absent from JSON when 0 (omitempty)
  severity: "error" | "warning" | "";  // "" for govulncheck/nilaway; absent when empty
  message:  string;          // human-readable summary
  error:    string;          // "" on success; non-empty on tool failure
  native:   object;          // full raw tool output for remediation
}
```

**Critical:** `column` and `severity` use `omitempty` — they are **absent from JSON**
when zero/empty. Use `.get("column")` / `.get("severity")`, not `["column"]` / `["severity"]`.

---

## Processing loop

```python
diagnostics = call("run_code_checks", project_path="/home/user/myproject")

for d in diagnostics:
    # Check error FIRST — tool-level failure, other tools may have succeeded
    if d["error"]:
        report_tool_failure(d["tool"], d["error"])
        continue

    location = f"{d['file']}:{d['line']}"
    if d.get("column"):
        location += f":{d['column']}"

    severity = d.get("severity", "")
    print(f"[{d['tool']}] {severity or 'info'} {location}: {d['message']}")

    # d["native"] contains full raw output:
    #   golangci-lint → SuggestedFixes, FromLinter, SourceLines
    #   govulncheck   → fixed_version, CVE aliases, full call trace
    #   nilaway       → full nil-propagation chain
```

An empty array `[]` means no issues found. It is not an error.

---

## Filtering by severity

```python
errors   = [d for d in diagnostics if not d["error"] and d.get("severity") == "error"]
warnings = [d for d in diagnostics if not d["error"] and d.get("severity") == "warning"]
info     = [d for d in diagnostics if not d["error"] and not d.get("severity")]
```

---

## What can go wrong

Tool-level errors appear as Diagnostic entries with a non-empty `error` field — other
tools' results are still present. Fatal errors (no `go.mod`, bad config, unknown tool
name) return as JSON-RPC errors instead. Full error tables and troubleshooting in
[reference.md](reference.md).

---

## Configuration

Create `.go-quality.yaml` in the project root (or server CWD) to override defaults:

```yaml
timeout: 5m          # per-tool deadline; increase for large monorepos or first vuln DB download

tools:
  golangci-lint:
    version: v2.11.4   # pinned; override at your own risk
    extra_args: []
  govulncheck:
    version: latest
    extra_args: []
  nilaway:
    version: latest
    extra_args: ["--exclude-pkgs=github.com/myorg/vendor"]
```

Precedence: `--config` flag > `.go-quality.yaml` at server CWD > compiled-in defaults.
All fields optional.
