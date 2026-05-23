This is the proposed AGENTS.md
---
# AGENTS.md — mcp-server-go-quality

This file tells AI agents (Claude Code, Codex, etc.) how to use this MCP server
correctly. Read it before calling any tool.

---

## What this server does

Runs three Go code quality tools against a Go project and returns a unified
`Diagnostic[]` array you can iterate with a single loop:

| Tool | What it finds |
|---|---|
| `golangci-lint` | Lint violations, cyclomatic/cognitive complexity, security patterns (gosec) |
| `govulncheck` | Known CVEs in your dependency graph via call-graph analysis |
| `nilaway` | Inter-procedural nil-panic paths the compiler won't catch |

---

## Required first step: install_tools

**Call `install_tools` once at the start of every session before running any checks.**

This installs tools synchronously and tells you whether they succeeded.
Skipping this means the first check call may silently spend 2–5 minutes installing
tools (including the govulncheck vulnerability database download) with no feedback.

```json
// Request — install all three tools
{
  "tool": "install_tools"
}

// Request — install only what you need (skip nilaway permanently)
{
  "tool": "install_tools",
  "tools": ["golangci-lint", "govulncheck"]
}

// Success response
{
  "installed": [
    { "tool": "golangci-lint", "version": "v2.11.4" }
  ],
  "already_present": [
    { "tool": "govulncheck",   "version": "v1.3.0" },
    { "tool": "nilaway",       "version": "v0.0.0-20260515015210-fd187751154f" }
  ],
  "failed": []
}

// Partial failure response — report this to the user and stop
{
  "installed": [
    { "tool": "golangci-lint", "version": "v2.11.4" }
  ],
  "already_present": [],
  "failed": [
    {
      "tool": "nilaway",
      "version": "latest",
      "command": "go install go.uber.org/nilaway/cmd/nilaway@latest",
      "stderr": "go: module lookup disabled by GONOSUMCHECK\n"
    }
  ]
}
```

**If `failed` is non-empty:** report each entry to the user with the `command` and
`stderr` fields. Do not proceed with checks for the failed tool — the server will
return a Diagnostic error for it anyway, but an early report is clearer.

---

## Running checks

### Full scan (recommended)

```json
// Request
{
  "tool": "run_code_checks",
  "project_path": "/home/user/myproject"
}
```

`project_path` is optional — omit it to use the server's working directory.
The server walks up from `project_path` to find the nearest `go.work` or `go.mod`,
so you can pass any subdirectory of a workspace.

### Subset scan

Use the `tools` parameter to skip a checker you don't need. Valid values:
`"golangci-lint"`, `"govulncheck"`, `"nilaway"`.

```json
// Skip nilaway (slow on unannotated monorepos)
{
  "tool": "run_code_checks",
  "project_path": "/home/user/myproject",
  "tools": ["golangci-lint", "govulncheck"]
}
```

Omitting `tools` or passing an empty array runs all three.

### Single-tool shortcuts

Prefer these for incremental re-checks after a fix — they are lighter because they
only install and discover the one needed tool:

```json
{ "tool": "run_lint",       "project_path": "/home/user/myproject" }
{ "tool": "run_vuln_check", "project_path": "/home/user/myproject" }
{ "tool": "run_nil_check",  "project_path": "/home/user/myproject" }
```

---

## Response: the Diagnostic array

Every check call returns a flat `Diagnostic[]` sorted by `file` then `line`.

```typescript
interface Diagnostic {
  tool:     "golangci-lint" | "govulncheck" | "nilaway";
  file:     string;          // relative path from project root; "" if unknown
  line:     number;          // 0 if unknown
  column:   number;          // 0 if unknown or not applicable (omitted if 0)
  severity: "error" | "warning" | "";  // "" means tool has no severity concept
  message:  string;          // human-readable summary
  error:    string;          // "" on success; non-empty on tool failure
  native:   object;          // full raw output from the tool — use for remediation
}
```

### Processing loop

```python
diagnostics = call("run_code_checks", project_path="/home/user/myproject")

for d in diagnostics:
    if d["error"]:
        # Tool-level failure — report and continue; other tools may have succeeded
        report_tool_failure(d["tool"], d["error"])
        continue

    location = f"{d['file']}:{d['line']}"
    if d["column"]:
        location += f":{d['column']}"

    print(f"[{d['tool']}] {d['severity'] or 'info'} {location}: {d['message']}")

    # Consult d["native"] for:
    #   golangci-lint → SuggestedFixes, FromLinter, SourceLines
    #   govulncheck   → fixed_version, CVE aliases, full call trace
    #   nilaway       → full nil-propagation chain
```

### Filtering by severity

```python
errors   = [d for d in diagnostics if not d["error"] and d["severity"] == "error"]
warnings = [d for d in diagnostics if not d["error"] and d["severity"] == "warning"]
info     = [d for d in diagnostics if not d["error"] and not d["severity"]]
```

### Success with no findings

An empty array `[]` is valid and means no issues were found. It is not an error.

---

## Error handling

### Tool-level errors (in the Diagnostic array)

These appear as entries with a non-empty `error` field. Other tools' results are
still present in the same response.

| `error` value | Meaning | What to do |
|---|---|---|
| `"Tool command failed with exit code N. Stderr: ..."` | Tool crashed or found a config problem | Show stderr to user; check `.golangci.yml` or project config |
| `"timed out after 5m0s"` | Tool exceeded per-tool deadline | Increase `timeout` in `.go-quality.yaml`; see timeout guidance below |
| `"cancelled"` | Client disconnected while tool was running | Safe to retry |
| `"unexpected output format from <tool>"` | Tool version mismatch with server's parser | Run `install_tools` to reinstall to the pinned version |
| `"N line(s) failed to parse: ..."` | govulncheck output had malformed lines | Likely a govulncheck version issue; run `install_tools` |

### Fatal errors (top-level, no Diagnostic array)

These are returned as a JSON-RPC error, not a Diagnostic array:

| Error message | Meaning | What to do |
|---|---|---|
| `"not a Go project"` | No `go.mod` or `go.work` found walking up from `project_path` | Verify the path is inside a Go module |
| `"go binary not found"` | `go` is not on PATH | The Go toolchain must be installed separately |
| `"config parse error: ..."` | `.go-quality.yaml` is present but malformed YAML | Fix the YAML or delete the file to use defaults |
| `"unknown tool: \"<value>\". valid values: golangci-lint, govulncheck, nilaway"` | Bad value in `tools` parameter | Check the `tools` array for typos |

---

## Timeout guidance

The `timeout` field in `.go-quality.yaml` is a **per-tool deadline** — each of the
three tools gets this budget independently. The default is 5 minutes.

```yaml
# .go-quality.yaml
timeout: 10m   # give each tool up to 10 minutes
```

**Why govulncheck is slow the first time:** govulncheck downloads the Go vulnerability
database (~40 MB) on its first invocation. This happens inside the tool execution,
not during `install_tools`. On a slow network this can take 1–2 minutes. The database
is cached locally afterward. If govulncheck times out on first run, increase `timeout`
to `10m` for the first scan, then revert.

**Why nilaway is slow on large projects:** nilaway performs whole-program analysis.
On a monorepo with many packages it routinely takes 3–10 minutes. Consider using the
`tools` subset parameter to skip nilaway during iterative development.

---

## Using native output for remediation

### golangci-lint — apply suggested fixes

```python
for d in diagnostics:
    if d["tool"] != "golangci-lint":
        continue
    fixes = d["native"].get("SuggestedFixes", [])
    for fix in fixes:
        # fix contains TextEdits with exact byte ranges — apply them directly
        apply_text_edits(fix["TextEdits"])
```

### govulncheck — report CVE with fix version

```python
for d in diagnostics:
    if d["tool"] != "govulncheck":
        continue
    native = d["native"]
    osv_id   = native["finding"]["osv"]
    fixed_at = native["finding"].get("fixed_version", "no fix available")
    cve_ids  = native.get("osv", {}).get("aliases", [])
    cve_str  = ", ".join(c for c in cve_ids if c.startswith("CVE-"))
    print(f"Vulnerability {osv_id} ({cve_str}): fix available in {fixed_at}")
    # Show the call trace so the user knows this vuln is actually reachable:
    for frame in native["finding"]["trace"]:
        pos = frame.get("position", {})
        print(f"  {frame['package']} {pos.get('filename','')}:{pos.get('line','')}")
```

### nilaway — show the nil propagation chain

nilaway's `message` field contains the full nil-propagation chain. The `Native` object
has the `posn` of the panic site and `end` of the nil origin. Show both to the user:

```python
for d in diagnostics:
    if d["tool"] != "nilaway":
        continue
    print(f"Nil panic at {d['file']}:{d['line']}")
    print(f"  {d['message']}")   # may span multiple sentences for complex chains
```

---

## Configuration reference

Create `.go-quality.yaml` in the project root to override defaults:

```yaml
timeout: 5m          # per-tool deadline; increase for large monorepos

tools:
  golangci-lint:
    version: v2.11.4   # pinned; override only if you need a specific version
    extra_args: []     # appended after required flags; cannot override --out-format=json
  govulncheck:
    version: latest
    extra_args: []
  nilaway:
    version: latest
    extra_args: ["--exclude-pkgs=github.com/myorg/vendor"]
```

The server's required flags (`--out-format=json`, `-json`, `-pretty-print=false`) are
always applied and cannot be overridden via `extra_args`.

---

## Multi-module workspaces

The server fully supports `go.work` workspaces. Pass any directory within the workspace
as `project_path` — the server walks up to find `go.work` automatically.

For nilaway in a multi-module workspace, the server automatically collects all module
paths from the `use` directives and passes them as `-include-pkgs` so nilaway scans all
local modules and ignores vendor/stdlib. No manual configuration is needed.

---

## Troubleshooting

**"unexpected output format from golangci-lint"**
The installed golangci-lint version doesn't match the server's parser (pinned to
v2.11.4). Run `install_tools` to restore the pinned version.

**govulncheck returns no findings on a project with known old dependencies**
govulncheck only reports vulnerabilities that are reachable via your call graph. A
dependency may have a CVE but if your code never calls the vulnerable function,
govulncheck correctly reports nothing. This is by design.

**nilaway reports too many false positives**
Add `--exclude-pkgs=<pattern>` to `extra_args` in `.go-quality.yaml` to skip
packages you don't control or haven't annotated yet. Generated code packages are a
common exclusion.

**All three tools time out in CI**
CI environments often have cold caches and slow disk. Set `timeout: 15m` in
`.go-quality.yaml` and call `install_tools` explicitly in the CI setup step before
running checks, to separate install time from analysis time.

**nilaway exits with build errors**
nilaway requires a fully type-checked program. If the project has syntax errors or
missing imports, nilaway will exit non-zero with compiler errors in stderr. This looks
like a tool failure but is really a project build failure. Check the `error` field for
`syntax error` or `undefined:` patterns; fix the build errors then re-run.

**govulncheck exits with "missing go.sum entry"**
govulncheck requires an up-to-date `go.sum`. If `go mod tidy` was never run, govulncheck
exits non-zero with a checksum verification error. The agent should prompt the user to
run `go mod tidy` and retry.

**Large projects produce very large responses**
golangci-lint on a large project with many linters enabled can return thousands of
issues, each with a `native` field containing the full raw JSON. Responses may exceed the
transport's message size limit. The server has no pagination or truncation — if you hit
size limits, reduce the linter set in `.golangci.yml` or run individual tools separately.