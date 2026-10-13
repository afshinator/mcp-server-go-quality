# AGENTS.md — mcp-server-go-quality

This file tells AI agents how to use this MCP server. Load it once per session.
For error handling tables, native output examples, and troubleshooting, see
[reference.md](reference.md).

---

## What this server does

Runs three Go code quality tools against a Go project and returns results as a
`{"diagnostics": [...]}` envelope containing a unified `Diagnostic[]` array:

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

Every check call returns an object with a `diagnostics` array sorted by `file`
then `line`. The array is available as `structuredContent.diagnostics`; the
text content block carries the same JSON.

```json
{ "diagnostics": [ /* Diagnostic[] */ ] }
```

```typescript
interface Diagnostic {
  tool:     "golangci-lint" | "govulncheck" | "nilaway";
  file:     string;          // relative path from project root; "" if unknown
  line:     number;          // 0 if unknown
  column:   number;          // 0 if unknown; absent from JSON when 0 (omitempty)
  severity: "error" | "warning";  // always present on a finding; see note below
  message:  string;          // human-readable summary
  error:    string;          // "" on success; non-empty on tool failure
  native:   object;          // full raw tool output for remediation
}
```

**Critical:** `column` uses `omitempty` — it is **absent from JSON** when zero. Use
`.get("column")`, not `["column"]`.

**`severity` is always present on a finding.** govulncheck and nilaway have no
severity concept, and golangci-lint reports one only when the repository's config
defines severity rules. A finding whose tool reports none is labelled `"error"`,
because an unlabelled finding would otherwise drop out of any severity-filtered
view — sorting a reachable CVE below a style warning. It is only absent on
**error** diagnostics (a non-empty `error` field), which are tool failures rather
than findings and are never severity-classified.

---

## Processing loop

```python
response    = call("run_code_checks", project_path="/home/user/myproject")
diagnostics = response["diagnostics"]

for d in diagnostics:
    # Check error FIRST — tool-level failure, other tools may have succeeded
    if d["error"]:
        report_tool_failure(d["tool"], d["error"])
        continue

    location = f"{d['file']}:{d['line']}"
    if d.get("column"):
        location += f":{d['column']}"

    severity = d.get("severity", "error")   # always set on a finding; "error" only on a failure row
    print(f"[{d['tool']}] {severity} {location}: {d['message']}")

    # d["native"] contains full raw output:
    #   golangci-lint → SuggestedFixes, FromLinter, SourceLines
    #   govulncheck   → fixed_version, CVE aliases, full call trace
    #   nilaway       → full nil-propagation chain
```

An empty `diagnostics` array means no issues found. It is not an error.

---

## Prefer the CLI? Same result without MCP

If you have shell access to the target machine, the `go-quality` binary runs the
same three checkers, resolves the same `.go-quality.yaml` and returns the same
`{"diagnostics": [...]}` envelope as `structuredContent` — so the processing loop
above is identical.

```bash
go-quality --json /path/to/project     # payload on stdout, summary on stderr
go-quality --tools=golangci-lint /p    # fast subset
go-quality --install-only              # pre-warm the pinned tools
```

Prefer MCP when the client manages the server lifecycle and you want
`structuredContent`. Prefer the CLI for one-shot runs, scripts and CI, where an
MCP session is overhead: no protocol handshake, no tool schema in context.

Exit codes: `0` no finding reached the threshold, `1` a finding reached it, `2`
the run did not complete (a checker failed to run, bad config, bad flag, missing
root). A tool failure is `2` unconditionally, even under `--fail-on=none`.

The tool is published on the
[releases page](https://github.com/afshinator/mcp-server-go-quality/releases) and
`go install .../cmd/go-quality@v0.1.0` works.

---

## Filtering by severity

Every finding carries a severity, so these buckets are exact. Do **not** treat a
missing severity as "info" — a missing one means the tool has no severity concept
(govulncheck, nilaway), which is a security finding, not an informational one.

```python
# Drop failure rows first: they carry `error`, not a severity.
findings = [d for d in diagnostics if not d["error"]]
failures = [d for d in diagnostics if d["error"]]

errors   = [d for d in findings if d.get("severity") == "error"]
warnings = [d for d in findings if d.get("severity") == "warning"]
```

A gate that must not miss a vulnerability should not filter on severity at all —
fail on any finding, or on any `error` diagnostic.

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
    version: v2.14.0   # pinned; override at your own risk
    extra_args: []
  govulncheck:
    version: v1.9.0   # pinned
    extra_args: []
  nilaway:
    version: v0.0.0-20260918162853-acb8859b9031   # pinned
    extra_args: ["--exclude-pkgs=github.com/myorg/vendor"]
```

Precedence: `--config` flag > `.go-quality.yaml` at server CWD > compiled-in defaults.
All fields optional.
