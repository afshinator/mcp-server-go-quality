# Spec v2 Review — Adversarial Analysis

**Date:** 2026-05-23
**Reviewer:** User (Human) + Agent (OpenCode)
**Spec:** `spec-v2.md`

---

## Critical gaps (would block correct implementation)

### C1 — Timeout code example is logically broken

**Issue:** The spec shows `context.WithTimeout(macroCtx, cfg.Timeout)` for each tool — giving
each tool the *full* macro budget derived from a parent that *also* has the full budget.
In practice both contexts expire at approximately the same moment (parent deadline was set
slightly earlier, so it wins), meaning the "independent per-tool context" provides no
isolation. All three tools die at exactly the same wall-clock time.

**Root cause:** The original spec text says `timeout` is "a macro-budget for the entire
run_code_checks operation" but also says "each tool runs in an independent derived context
so that a single slow tool does not cancel the others." These goals conflict when `timeout`
is a total budget.

**Proposed resolution:** Redefine `timeout` as **a per-tool budget**. Each checker goroutine
gets `context.WithTimeout(ctx, cfg.Timeout)` from a `context.Background()` or
`context.WithCancel` parent. No macro-budget timeout. The macro context exists only for
explicit cancellation (client disconnect). This means:

- A slow tool times out independently; other tools keep running
- Total wall clock is bounded by the slowest tool (max `cfg.Timeout`)
- The code example becomes correct
- YAML field docs change from "total macro-budget" to "per-tool deadline"

### C2 — `install_tools` failure format is underspecified

**Issue:** The schema shows `"failed": []` but doesn't define what a failed entry looks like.

**Proposed resolution:** Each failed entry is an object:
```json
{
  "tool": "golangci-lint",
  "version": "v2.11.4",
  "command": "go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4",
  "stderr": "<go install error output>"
}
```
This gives agents enough to self-remediate.

### C3 — Single-tool timeout behaviour unspecified

**Issue:** The timeout model section only describes `run_code_checks` (3 goroutines, macro
budget). What timeout applies when an agent calls `run_lint` directly?

**Proposed resolution:** Single-tool handlers (`run_lint`, `run_vuln_check`, `run_nil_check`)
each derive their own `context.WithTimeout(ctx, cfg.Timeout)` — same per-tool budget,
same model, one goroutine. No macro context needed (there's only one).

### C4 — NDJSON error Diagnostic has nowhere to put raw unparseable content

**Issue:** The spec says put raw unparseable content in `Native`, but `Native` is
`json.RawMessage` — it must be valid JSON. A line that failed `json.Unmarshal` is
*not* valid JSON.

**Proposed resolution:** Wrap the raw line as a JSON-encoded string in `Native`:
```go
json.RawMessage(jsonMarshal(rawLine))
```
This preserves the raw content without adding a new Diagnostic field. The `error` field
already signals that this is a parse error.

---

## Significant holes (implementation ambiguity)

### S1 — `project_path` optional but `CommandRunner.Dir` set at construction

**Issue:** If a runner is constructed once and reused across requests, and `project_path`
differs per call, the runner's `Dir` is stale.

**Proposed resolution:** The runner is constructed **fresh per request** in the MCP handler
layer, using the resolved `project_path`. `ExecRunner{dir: projectPath}`. The interface
stays clean; per-request construction is cheap (zero allocations beyond the struct).

### S2 — `project_path` pointing into a go.work subdirectory

**Issue:** What if `project_path` points to one module *inside* a workspace? Does the
server walk up to find `go.work`?

**Proposed resolution:** The server walks **up** from `project_path` looking for `go.work`,
then `go.mod`. It always operates from the discovered root (workspace or module). This
handles the common case where `/project/monorepo/services/auth` resolves to
`/project/monorepo/go.work`. The resolved root is what's used for `cmd.Dir` and
`go list -m`.

### S3 — No `Severity` field in Diagnostic

**Issue:** golangci-lint emits `"Severity": "warning"` on each issue. An agent triaging
hundreds of findings needs to filter by severity without parsing `Native`.

**Proposed resolution:** Add `Severity string` to `Diagnostic`. Map known values:
- golangci-lint: from `issue.Severity` ("warning", "error")
- govulncheck: from `osv` severity if available, else ""
- nilaway: always "" (nilaway doesn't emit severity)

This is free for golangci-lint (already parsing the field) and low-cost for govulncheck.

### S4 — `Column` is extracted but not in Diagnostic

**Issue:** nilaway extraction rules mention parsing column from `posn` but `Diagnostic`
only has `Line`.

**Proposed resolution:** Add `Column int` to `Diagnostic` with `json:"column,omitempty"`.
Extract from all three tools:
- golangci-lint: `Pos.Column`
- govulncheck: `position.column`
- nilaway: from `posn` string
Zero = unknown/not applicable.

### S5 — `--config` CLI flag and precedence misleading

**Issue:** "if the MCP client passes `--config /path/to/override.yaml`, that file governs"
is misleading because that file is still level 2, not level 1.

**Proposed resolution:** Clarify the hierarchy:
1. **CLI flags / process env** — `--config` changes which file to read, but the file's
   content is still level 2. Only explicit flags like `--timeout` are level 1.
2. **YAML file content** — wherever it lives (`--config` path or default `.go-quality.yaml`)
3. **Compiled-in defaults**

The `--config` flag controls the *source* of level 2, not its precedence.

### S6 — govulncheck severity in `osv`

**Issue:** govulncheck findings have different urgency; not exposing this makes the server
less useful than running govulncheck directly.

**Proposed resolution:** The `Native` field already carries the full `osv` object including
`aliases` (CVE IDs) and any severity info govulncheck emits. Adding `Severity` to
`Diagnostic` (resolved in S3) covers the normalisation layer. For CVE-specific severity,
agents should inspect `Native` — the server normalises what's common across all tools,
not what's unique to one.

---

## User/agent experience problems

### U1 — No progress signal for agents during auto-install

**Issue:** Auto-install can take 2-5 minutes. The agent gets silence, then either results
or a timeout.

**Proposed resolution:** Document `install_tools` as the **recommended first step** in
agent workflows. The spec should say: "Agents should call `install_tools` once at session
start to force a synchronous install with progress. Check tools (`run_lint` etc.) will
auto-install as a safety net but should not be relied on for first-time use." This
respects the current design while making the happy path predictable.

### U2 — `"cancelled"` message doesn't say which sibling caused it

**Issue:** An agent receiving `"cancelled"` doesn't know if it should retry or raise timeout.

**Proposed resolution:** Since `"cancelled"` means the macro context was cancelled (not a
sibling timeout — that's covered by the independent per-tool contexts in C1), the only
source of cancellation is the caller disconnecting. Keep the message as `"cancelled"` but
document that it means the client cancelled, not a sibling timeout.

### U3 — No way to run a subset via `run_code_checks`

**Issue:** If an agent wants lint+vuln but not nilaway, it calls two tools and merges.

**Proposed resolution:** Add an optional `tools` parameter to `run_code_checks`:
```json
"tools": ["golangci-lint", "govulncheck"]
```
Omitted or empty = all three. This is a convenience that costs little.

### U4 — Version override without re-install

**Issue:** If `.go-quality.yaml` sets `golangci-lint: v2.3.0` but v2.11.4 is installed,
the server uses the wrong version.

**Proposed resolution:** The server checks the installed version:
1. Use `exec.LookPath` to find the binary
2. Run `<tool> --version` and parse the version string
3. If version doesn't match the requested version (from config or default), re-install

This is done during pre-flight tool discovery, not on every invocation. `sync.Once` still
applies — but the Once-check includes version matching. Add a `ResolutionResult` type to
`ToolInfo` that captures `{path, version, action: "cached"|"installed"|"upgraded"}`.

Actually: `golangci-lint --version` output format varies by version and isn't reliably
machine-parseable. Safer approach: track the *intended* version in-process and only
re-install when the intended version changes. `sync.Once` becomes `sync.OnceValues` keyed
on `(toolName, version)`, or use a `sync.Map` keyed by `toolName@version`. When the
version requested differs from what was installed by a previous request, install the new
one. The old binary stays on disk (Go toolchain manages that via `GOPATH/bin`) but the
new version is what gets executed.

**Decision:** Use `sync.Map` (or a plain mutex-guarded map) to track installed `toolName@version`
pairs. `IsInstalled(toolName, version)` returns false when version differs, triggering
re-install.

### U5 — `already_present` should include versions

**Issue:** `"already_present": ["govulncheck", "nilaway"]` doesn't tell what version.

**Proposed resolution:** Since `exec.LookPath` only gives the binary path, determining the
version would require running `<tool> --version` and parsing output — which is fragile
(see U4). Instead, store the version we *installed* at install time and report that:
```json
"already_present": ["govulncheck@latest", "nilaway@latest"]
```
This is the version the server believes is installed, not necessarily what's on disk.

---

## Minor / editorial

### M1 — No data flow for single-tool paths

**Resolution:** Add a one-sentence note: "Single-tool handlers (`run_lint`, `run_vuln_check`,
`run_nil_check`) follow the same handler flow but skip the goroutine dispatch — they call
the handler directly with a per-tool timeout context."

### M2 — "First sentence" from nilaway is brittle

**Resolution:** Add: "A 'sentence' is everything up to the first `. ` (period + space)
followed by an uppercase ASCII letter `[A-Z]`. If no such boundary is found, use the
full message."

### M3 — Architecture diagram shows `installTools` as peer handler

**Resolution:** Move `installTools` to the transport layer (it's an MCP tool handler, not
a checker). Add a note that it's the only tool that doesn't flow through the subprocess
layer — it calls discovery directly.

### M4 — testdata sample project underspecified

**Resolution:** Add minimum content spec:
- One function with cyclomatic complexity > 15 (triggers gocyclo/gocognit)
- One nil dereference path (triggers nilaway)
- One `net/http` dependency (govulncheck may flag known CVEs — depends on installed
  Go version's vulndb; not guaranteed but ideal)
- `go.mod` with a real module path

---

## Resolution summary

| # | Severity | Topic | Resolution |
|---|---|---|---|
| C1 | Critical | Timeout code broken | **Timeout = per-tool budget.** Macro context is `WithCancel` only. |
| C2 | Critical | Install failure format | Object: `{tool, version, command, stderr}` |
| C3 | Critical | Single-tool timeout | Same per-tool budget, derived directly |
| C4 | Critical | NDJSON error in Native | Wrap raw line as JSON-encoded string |
| S1 | High | Runner construction | Per-request construction from resolved path |
| S2 | High | go.work subdirectory | Walk up from project_path to find root |
| S3 | High | Severity field | Add `Severity string` to Diagnostic |
| S4 | High | Column field | Add `Column int` to Diagnostic |
| S5 | High | --config precedence | Clarify: flag controls source, not rank |
| S6 | Medium | govulncheck severity | Covered by S3 + Native field |
| U1 | Medium | Progress for agents | Document install_tools as recommended first step |
| U2 | Medium | "cancelled" message | Document it means caller disconnect |
| U3 | Medium | Subset running | Add optional `tools` param to run_code_checks |
| U4 | Medium | Version mismatch | Track per-(tool,version) install state |
| U5 | Medium | already_present versions | Report installed version, not on-disk |
| M1 | Minor | Single-tool data flow | Add one-line note |
| M2 | Minor | nilaway sentence | Define `. ` + uppercase boundary rule |
| M3 | Minor | Architecture diagram | Move installTools, add note |
| M4 | Minor | testdata spec | Define minimum issues |

---

## Decision calls pending

These require explicit user sign-off (marked in the resolution summary):

- **C1/D1:** Timeout redefined as per-tool budget (changes behavior from v2)
- **S3:** `Severity` field added to Diagnostic
- **S4:** `Column` field added to Diagnostic
- **U3:** `tools` parameter on `run_code_checks`
- **U4:** Per-(tool, version) install tracking (adds complexity to discovery)
