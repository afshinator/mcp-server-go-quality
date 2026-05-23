# mcp-server-go-quality — Design Spec v3


## Overview

An MCP (Model Context Protocol) server that wraps Go code quality tools for consumption
by AI agents (Claude Code, Codex, etc.) and CI pipelines. The server runs linting,
vulnerability scanning, and nil-panic detection on any Go project, returning structured
JSON diagnostics an agent can parse and act on programmatically.

**MCP protocol version:** This server targets MCP specification `2025-03-26`. The
`initialize` response must declare this version. Verify against the current published
spec at implementation time.

### Why MCP Wrapping

Individual CLI tools (`golangci-lint`, `govulncheck`, `nilaway`) are developer-facing.
An LLM agent needs a machine-facing API with:
- Single entry point instead of 3 different CLI invocations
- Auto-discovery and auto-installation of missing tools
- Parallel execution with unified results
- Consistent error handling and timeouts

---

## Tools Included

Based on overlap analysis (see `docs/tools-research.md`):

| Tool | Purpose | Output Format | Built Into golangci-lint? | Pinned Default Version |
|---|---|---|---|---|
| `golangci-lint` | Linting + complexity (`gocyclo`, `gocognit`) + security patterns (`gosec`) | JSON (`--out-format=json`) | N/A | `v2.11.4` |
| `govulncheck` | CVE scanning via call-graph analysis | JSON-lines (`-json`) | No | `latest` |
| `nilaway` | Deep inter-procedural nil-panic detection | JSON (`-json -pretty-print=false`) | No (plugin possible) | `latest` |

`gocyclo` and `gocognit` are excluded as standalone tools — they are built into
golangci-lint and activated via `.golangci.yml` config.

### Module Paths for Installation and Resolution

These paths are used by `go install` and `go list -m -json`:

| Tool | Module Path |
|---|---|
| golangci-lint | `github.com/golangci/golangci-lint/v2/cmd/golangci-lint` |
| govulncheck | `golang.org/x/vuln/cmd/govulncheck` |
| nilaway | `go.uber.org/nilaway/cmd/nilaway` |

**Rationale for pinning golangci-lint:** golangci-lint's JSON output schema has changed
across major versions. v2.11.4 is the validated reference. Workspace configs may override
this, but the default is pinned so the output parser doesn't silently break on a future
`latest` that ships a breaking schema change. govulncheck and nilaway have stable output
formats and may track `latest`.

---

## Architecture

Three-layer functional design:

```
Transport Layer          Tool Handlers                   Subprocess & Parsing
┌──────────────────┐    ┌──────────────────────────┐    ┌──────────────────────┐
│ JSON-RPC / stdio  │    │ runLint(ctx, r, path)    │    │ exec(golangci-lint)  │
│ 5 registered tools│───▶│ runVuln(ctx, r, path)   │───▶│ exec(govulncheck)    │
│ Tool dispatch     │    │ runNil(ctx, r, path)     │    │ exec(nilaway)        │
│ installTools calls│    │ runAll(ctx, r, path,    │    │ parse → normalize    │
│ discovery directly│    │          tools[])        │    │ gather results       │
└──────────────────┘    └──────────────────────────┘    └──────────────────────┘
```

**Note:** `install_tools` is the only tool that does not flow through the handler layer —
it calls the discovery subsystem directly to force-install tools. It is not a checker.

### Design Rules

- **Idiomatic signatures** — handlers are `func runX(ctx context.Context, r CommandRunner, path string) ([]Diagnostic, error)`. Context carries the per-tool deadline; `CommandRunner` is the mockable executor.
- **Runner carries directory** — `ExecRunner` stores `Dir string` set at construction time; `CommandRunner` interface stays clean (`Run(ctx, name, args...)` — no dir parameter). The handler layer creates a fresh runner per request from the resolved project path. This is cheap (zero allocations beyond the struct) and avoids stale state across requests with different `project_path` values.
- **Parallel by default** — `runAll` fires up to `len(requestedTools)` goroutines (maximum 3) with independent per-tool timeout contexts; a cancel-only parent context supports clean shutdown.
- **Version-aware caching** — the cache tracks `toolName → resolvedVersion` (e.g. `govulncheck → v1.3.0`). The resolved version is read from the installed binary via `go version -m`. When `.go-quality.yaml` requests a different version, a re-install is triggered and the cache is updated. The cache is protected by a `sync.RWMutex` — read locks for lookups, write locks for installs and cache updates. **Double-check lock pattern required:** after acquiring the write lock, re-stat the binary and re-read the cache entry before installing. A concurrent request may have completed the install while this goroutine waited for the write lock; if the re-check confirms the correct version is now cached, release the lock and skip the install. This prevents redundant `go install` calls and module cache file corruption.
- **Uniform path normalization** — all handlers must normalize file paths to be relative
  to `projectRoot` using `filepath.Rel(projectRoot, path)`. Although `golangci-lint`
  reliably emits paths relative to `cmd.Dir` when invoked with `./...`, and govulncheck
  generally does likewise, neither is guaranteed across all versions and workspace
  configurations. Every handler must run its extracted file path through the same
  `filepath.Rel` resolver (with the same fallbacks specified for nilaway) so the agent
  receives a consistent relative path regardless of which tool produced the diagnostic.
- **Single entry point** — `cmd/mcp-server-go-quality/main.go`
- **Logging to stderr only** — all progress and install messages go to `stderr`; `stdout` carries only JSON-RPC traffic.

### Concurrency Model

The server uses stdio transport, meaning a single MCP client connects to the process.
The MCP protocol permits multiple concurrent tool calls from that client. The server
implements no global request queue or concurrency limit: N simultaneous `run_code_checks`
calls produce up to N×3 subprocesses. On constrained machines (Docker containers with
CPU or memory limits), agents should serialize check calls or use the `tools` parameter
to reduce per-call parallelism. The pre-flight sequential install ensures `go install`
calls never race regardless of request concurrency.

### MCP Tools Registered

| Tool Name | Description |
|---|---|
| `run_code_checks` | Run all 3 checkers in parallel (configurable subset), return unified results |
| `run_lint` | Run golangci-lint only |
| `run_vuln_check` | Run govulncheck only |
| `run_nil_check` | Run nilaway only |
| `install_tools` | Pre-install required Go tools (configurable subset, pinned versions) |

Each check tool accepts:
- `project_path` (optional, string) — defaults to server's CWD. When launched as an
  MCP server, the host process (e.g. Claude Code) sets the server's working directory
  to the project workspace root. Agents should pass an explicit `project_path` when
  targeting a specific sub-module to avoid ambiguity.

`run_code_checks` also accepts:
- `tools` (optional, array of strings) — subset of checkers to run. Valid values:
  `"golangci-lint"`, `"govulncheck"`, `"nilaway"`. Omitted or empty = all three.
  **Design note:** An explicit empty array (`[]`) is intentionally equivalent to omitting
  the parameter — both run all three checkers. There is no way to run zero checkers via
  `run_code_checks`; use a single-tool handler if only one checker is needed.

`install_tools` also accepts:
- `tools` (optional, array of strings) — subset of tools to install. Same valid values
  as `run_code_checks`. Omitted or empty = all three. Useful when an agent permanently
  skips a tool (e.g. nilaway in unannotated monorepos) and doesn't want to pay its
  install cost.

---

## Data Flow

```
Agent calls run_code_checks →
  Parse project_path (default CWD)
  Two-pass walk to find project root (Pass 1: go.work; Pass 2: go.mod)
  Validate root is a Go project (go.mod or go.work exists)           ← fatal if missing
  Pre-flight: synchronous discovery of requested tools (version-aware cache)
    → install any missing/wrong-version requested tools sequentially ← avoids go install race
    → log progress to stderr only
  Derive cancel-only parent context: context.WithCancel(parent)
  Filter to requested tools (from `tools` param, default all)
  Fire goroutines only for requested tools; loop variable passed by value
  (explicit pass is idiomatic; loop-variable capture is fixed in Go ≥1.22):
    for _, tool := range requestedTools {
        go func(t Tool) {
            toolCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
            defer cancel()
            // exec.Command under toolCtx → parse → send result to typed channel
        }(tool)
    }
  Each handler:
    1. exec.Command under its independent toolCtx
    2. Parse tool's native JSON → extract Location/Severity/Message fields
    3. Send ([]Diagnostic, error) to results channel — timeout/error captured as Diagnostic
  Collect all results from channel (closed after wg.Wait())
  Sort by file→line
  Return JSON array

Single-tool handlers (run_lint, run_vuln_check, run_nil_check) follow the same flow
but skip the goroutine dispatch — they call the handler directly with a per-tool timeout
context. Pre-flight install still applies (guarded by version-aware cache).
```

---

## Output Schema

Each diagnostic carries extracted location fields for uniform agent navigation,
plus the complete native tool output for deep context:

```go
type Diagnostic struct {
    Tool     string          `json:"tool"`              // "golangci-lint" | "govulncheck" | "nilaway"
    File     string          `json:"file"`              // relative path from project root ("" if unknown)
    Line     int             `json:"line"`              // 0 if unknown
    Column   int             `json:"column,omitempty"`  // 0 if unknown / not applicable
    Severity string          `json:"severity,omitempty"`// "error" | "warning" | absent if tool has no concept
    Message  string          `json:"message"`           // human-readable summary extracted from native output
    Error    string          `json:"error"`             // "" on success; standardised message on failure (see below)
    Native   json.RawMessage `json:"native"`            // tool's complete native JSON output (or JSON-encoded raw string on parse failure)
}
```

The `File`, `Line`, `Column`, `Severity`, and `Message` fields are extracted from each
tool's native output so the AI agent can navigate to every issue using a single consistent
loop. The `Native` field preserves the full raw output for deep context, remediation
instructions (govulncheck references, golangci-lint SuggestedFixes), and tool-specific
fields the agent may need.

**Field encoding notes for JSON consumers:**
- `Column` uses 1-based indexing. A value of `0` means "unknown or not applicable" and
  is omitted from JSON output via `omitempty`. No supported tool uses 0-based column
  indices, so `0` unambiguously means "not reported."
- `Severity` with `omitempty` means the field is **absent** (not `""`) in JSON output
  for govulncheck and nilaway diagnostics — tools that have no severity concept. Agents
  must use `hasOwnProperty("severity")` (or equivalent) rather than testing for an empty
  string.
- `Native` is `json.RawMessage`. A zero-valued (nil) `Native` marshals as JSON `null`.
  This occurs for error diagnostics where no native output was captured. Agents must
  null-check `native` before accessing its contents.

**Severity mapping by tool:**
- golangci-lint: `issue.Severity` (`"warning"` or `"error"`)
- govulncheck: field absent (Go vulnerability database has no severity field)
- nilaway: field absent (nilaway has no severity concept)

**When `Error` is non-empty**, the Diagnostic carries a tool-level failure:
- `File`, `Line`, `Column`, `Severity`, `Message` are zero-valued (not applicable).
- `Native` is zero-valued (marshals as `null`) *unless* the error is an NDJSON parse
  failure — in that one case, `Native` carries a JSON-encoded string of the raw
  unparseable content so a developer can inspect what govulncheck emitted (see
  govulncheck section).

**When `Error` is empty**, the Diagnostic carries a finding:
- All fields are populated normally from the tool's native output.
- `Severity` is absent for tools that have no severity concept.

### Standardised Error String Format

All `error` field values use this exact format:

```
Tool command failed with exit code <N>. Stderr: <content>
```

For install failures:

```
install failed: <exact-go-install-command>. exit code <N>. stderr: <content>
```

For timeouts:

```
timed out after <duration>
```

For cancellation:

```
cancelled
```

For unexpected output:

```
unexpected output format from <tool>
```

This allows agents to parse error strings programmatically without regex.

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

Note: The `--out-format=json` flag format is documented for golangci-lint v2.
The `Issues` wrapper structure and gosec field names above reflect v2.11.4 (the pinned
default). If a workspace overrides the version, the parser should be validated against
that version's output before deployment.

Extraction rules:
- `File`/`Line`/`Column`: from `Pos.Filename`, `Pos.Line`, `Pos.Column`
- `Severity`: from `Severity` (`"warning"` or `"error"`)
- `Message`: from `Text`
- `Native`: the raw issue object

**govulncheck** (`-json` — JSON-lines, one object per line):

The handler must read stdout line-by-line with `bufio.Scanner` since govulncheck emits
newline-delimited JSON (NDJSON). Each line is an independent object — they cannot be
unmarshalled as a single JSON document. The multi-line formatting in the example below
is for readability; in actual output each object is emitted as a single line.

```jsonl
{"config": {...}}
{"SBOM": {...}}
{"osv": {"id": "GO-2026-4918", "summary": "...", "aliases": ["CVE-..."]}}
{"finding": {
  "osv": "GO-2026-4918",
  "fixed_version": "v1.25.10",
  "trace": [
    {"module": "github.com/user/project", "package": "...httpclient",
     "position": {"filename": "internal/httpclient/client.go", "line": 78, "column": 25}},
    {"module": "stdlib", "package": "net/http",
     "position": {"filename": "src/net/http/client.go", "line": 586, "column": 18}}
  ]
}}
```

**Parser implementation note:** Because `osv` objects and `finding` objects arrive on
separate, independent NDJSON lines, the parser must build an in-memory map of
`osvID → summary` as it reads lines sequentially. When it encounters a `finding` line,
it looks up `finding.osv` in this map to resolve the `Message`. A `finding` that
arrives before its corresponding `osv` line (which govulncheck does not currently emit,
but is not ruled out by the format) must still resolve correctly — the map lookup
should be deferred until all lines are consumed, not performed inline during streaming.

Unknown object types in the NDJSON stream (e.g. `progress`, `message`, or future fields)
must be silently skipped. Only `osv` and `finding` keys are processed.

Extraction rules for the unified `Diagnostic` fields:
- `File`/`Line`/`Column`: govulncheck orders `trace` **chronologically from Caller to
  Callee** — `trace[0]` is the user-space entry point (the outermost call in local code
  that starts the vulnerable call chain), and the last entry is the vulnerable sink inside
  a dependency or the standard library. Do **not** use the last trace entry — it points
  to read-only module cache paths (e.g. `pkg/mod/...`) that an agent cannot edit.
  Instead, traverse the array from index `0` forward and extract `filename`, `line`, and
  `column` from the **first** `trace` entry whose `module` field matches a workspace-local
  module (declared in a `use` directive of `go.work`, or the root module of a
  single-module project). As shown in the example above, `github.com/user/project` is
  `trace[0]` (the editable caller in user code) and `stdlib/net/http` is the last entry
  (the read-only vulnerable sink). If no workspace-local entry is found (unusual),
  fall back to `trace[0]`.
- `Severity`: field absent (Go vulnerability database has no severity field)
- `Message`: look up `finding.osv` ID in the accumulated `osvID → summary` map. If the
  ID is absent from the map or the `summary` field is empty, fall back to the raw
  `finding.osv` ID string (e.g. `"GO-2026-4918"`). Do not use `osv.details` as a
  fallback — govulncheck's streamed NDJSON output omits the `details` field even when
  present in the full OSV database record, making it unreliable as a fallback source.
- `Native`: pack the `finding` and its correlated `osv` block into a rigid container
  struct to provide an unambiguous schema without dynamic JSON deep-merging:
  ```go
  type GovulncheckNativeContainer struct {
      Finding json.RawMessage `json:"finding"`
      OSV     json.RawMessage `json:"osv"`
  }
  ```
  Serialise this struct as the `Native` field. Agents access `native.finding` and
  `native.osv` as independent JSON objects. If no matching `osv` entry exists in the
  accumulated map (rare), set `OSV` to `json.RawMessage("null")`.

**Note on multi-module workspaces:** the "first workspace-local entry" heuristic picks
the correct editable call site even when multiple modules are present. Agents can inspect
`native.finding.trace` for the full call chain from the user's entry point down to the
vulnerable sink when more context is needed.

**Vulnerability database lock retry:** On the first invocation after a cold install,
govulncheck downloads and caches the Go vulnerability database (approximately 40 MB —
and growing — at `~/.cache/govulncheck`). If two concurrent `govulncheck` processes race
on this download (e.g. two parallel MCP requests both trigger `run_vuln_check` before the
cache is warm), the second process may exit with code 1 and a filesystem locking error
in stderr. The handler must detect this specific failure and retry:

```go
const vulnDBLockPhrase = "database is locked"  // substring match on stderr
const maxVulnRetries   = 3
const vulnRetryBackoff = 2 * time.Second

for attempt := 0; attempt < maxVulnRetries; attempt++ {
    result, stderr, exitCode = runGovulncheck(toolCtx, ...)
    if exitCode == 1 && strings.Contains(stderr, vulnDBLockPhrase) {
        select {
        case <-time.After(vulnRetryBackoff):
            // backoff elapsed, try again
        case <-toolCtx.Done():
            return nil, toolCtx.Err() // deadline or cancellation — stop retrying
        }
        continue
    }
    break
}
```

This keeps the execution path fully concurrent for the 99% case where the local DB is
already warm. The retry only fires on the rare cold-start collision. If all retry
attempts fail with a lock error, the final attempt's error is returned as a normal
Diagnostic (exit code 1). The backoff is context-aware: if `toolCtx` is cancelled or
its deadline expires during the sleep, the retry loop exits immediately. Note that each
backoff interval counts against the per-tool timeout budget — three retries at 2s each
consume 6s before any govulncheck analysis begins. Calling `install_tools` at session
start is recommended, as it primes the DB download sequentially before any parallel
check calls begin.

**NDJSON parse error handling:** The parser must accumulate `json.Unmarshal` errors per
line rather than silently skipping them. If any lines failed to parse, append a single
Diagnostic with `Error` set (not `Message`), `Native` populated with the JSON-encoded
raw content, and all other fields zero-valued:

```go
Diagnostic{
    Tool:    "govulncheck",
    Error:   fmt.Sprintf("%d line(s) failed to parse: %s", n, firstErr),
    Native:  json.RawMessage(mustMarshal(rawLinesAsString)),
    // File, Line, Column, Severity, Message are zero-valued
}
```

This is a tool-execution failure — the diagnostic is about govulncheck's output format,
not about the user's code. `Native` carries the raw content so a developer can debug.

**nilaway** (`-json -pretty-print=false` — verified against `go.uber.org/nilaway` at the
pseudo-version pinned in the version cache; re-validate this flag interface when bumping
the nilaway version via `install_tools`, as nilaway is pre-1.0 and its CLI is not yet
stable):

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

The top-level JSON object is a dynamic map keyed by Go package import path (e.g.
`"github.com/myorg/repo/services/auth/pkg/db"`). The parser must treat this as
`map[string]NilawayPackageResult` — not a fixed struct — since the keys are arbitrary
package paths that vary per project:

```go
type NilawayIssue struct {
    Posn    string `json:"posn"`
    End     string `json:"end"`
    Message string `json:"message"`
}
type NilawayPackageResult struct {
    Nilaway []NilawayIssue `json:"nilaway"`
}
type NilawayOutput map[string]NilawayPackageResult
```

**Zero-findings case:** When nilaway finds no nil-safety issues, it emits an empty JSON
object `{}`. The parser maps over an empty `NilawayOutput` and returns `[]Diagnostic{}`
(success, no findings). Empty stdout (no output at all) is distinct from `{}` and must
be treated as `"unexpected output format from nilaway"` — return a Diagnostic with
`Error` set.

Extraction rules:
- `File`: strip project root prefix using `filepath.Rel(projectRoot, absPath)`. If
  `filepath.Rel` returns an error (e.g. cross-drive or symlink boundary), fall back to
  `strings.TrimPrefix(absPath, projectRoot)`. If the path is already relative or
  stripping still yields an absolute path, preserve it as-is rather than returning `""`.
- `Line`/`Column`: parsed from `posn` (`file.go:line:col`)
- `Severity`: field absent (nilaway has no severity concept)
- `Message`: first sentence of the `message` field. A "sentence" is everything up to the
  first `. ` (period + space) followed by an uppercase ASCII letter `[A-Z]`, or up to
  the first `\n`. If no such boundary is found, use the full `message`. This avoids
  false boundaries from function signatures containing dots (e.g. `pkg.Type.Method`).
  Known limitation: abbreviations ending in uppercase (e.g. `"e.g. Something"`) will
  produce a false sentence split at `"e.g."`. This is accepted as a minor cosmetic
  defect — the full message is always available in `native`.
- `Native`: the raw nilaway error object

---

## Tool Discovery & Installation

### Binary Directory Resolution (applies everywhere, resolved at startup)

The server must never rely solely on `exec.LookPath` for tool presence checks or
subprocess invocations. `$GOPATH/bin` and `$GOBIN` are frequently absent from `$PATH`
in Docker images, CI containers, and sandboxed environments. Using `LookPath` in these
environments causes a silent infinite reinstall loop: the server runs `go install`,
places the binary in `$GOPATH/bin`, and then on the next request `LookPath` fails again
because `$PATH` hasn't changed.

At server startup, resolve the install directory once using the Go toolchain:

```go
func resolveGoBinDir() (string, error) {
    out, err := exec.Command("go", "env", "GOBIN").Output()
    if err != nil {
        return "", fmt.Errorf("go env GOBIN: %w", err)
    }
    if binDir := strings.TrimSpace(string(out)); binDir != "" {
        return binDir, nil
    }
    out, err = exec.Command("go", "env", "GOPATH").Output()
    if err != nil {
        return "", fmt.Errorf("go env GOPATH: %w", err)
    }
    gopath := strings.TrimSpace(string(out))
    if gopath == "" {
        homeDir, err := os.UserHomeDir()
        if err != nil {
            return "", fmt.Errorf("os.UserHomeDir: %w", err)
        }
        gopath = filepath.Join(homeDir, "go")
    }
    return filepath.Join(gopath, "bin"), nil
}
```

Store this `binDir` at startup. All subsequent tool presence checks, version reads, and
`exec.Command` invocations use `filepath.Join(binDir, toolName)` directly — never
`exec.LookPath`. This eliminates the silent reinstall loop that occurs when `$GOPATH/bin`
is absent from `$PATH`.

### Auto-Install Rules

1. On every request from any check tool, a version-aware cache lookup is performed for
   each requested tool. The cache tracks `toolName → resolvedVersion`. On a **cache hit**
   with a matching version, skip directly to execution. On a **cache miss**, proceed to
   discovery (rule 2).
2. **Discovery:** stat `filepath.Join(binDir, toolName)`. If the binary exists, read its
   installed version via `go version -m <path>` and compare against the requested version.
   If it matches, store in cache and skip install. If missing or wrong version, proceed
   to install (rule 3).
3. If any requested tool is missing or has the wrong version, install all such tools
   **sequentially** to avoid Go module cache lock races from concurrent `go install` calls.
   **Double-check lock pattern:** after acquiring the write lock on the version cache,
   re-stat the binary before installing. A concurrent request may have completed the
   install while this goroutine waited for the lock. If the re-check confirms the correct
   version is now present, release the lock and skip the install.
4. Log a clear message to **stderr**: `"Installing <tool>@<version>... this happens once."`
   — never to stdout, which carries JSON-RPC traffic.
5. If install fails, return a Diagnostic with the `error` field formatted as:
   `"install failed: <exact-go-install-command>. exit code <N>. stderr: <content>"`
6. The `install_tools` MCP tool lets agents pre-install proactively before any analysis
   runs. It is the **recommended first step** in agent workflows — calling it at session
   start forces a synchronous install with progress feedback, avoiding a silent 2–5
   minute wait on the first check call. `install_tools` bypasses the in-memory version
   cache and re-stats the binary on disk for each requested tool (or all three if `tools`
   is omitted or empty). Tools already at the correct version on disk appear in
   `already_present`. Tools at the wrong version or missing are re-installed and appear
   in `installed` on success or `failed` on failure. After completion, the cache entries
   for all re-checked tools are updated with the freshly resolved versions — guaranteeing
   the next check call has a warm cache.

### install_tools Output Format

`install_tools` returns a structured JSON object so agents can act on it
programmatically. All three arrays use the same per-entry object shape — `{ tool, version }`
for success entries, `{ tool, version, command, stderr }` for failures:

```json
{
  "installed": [
    { "tool": "golangci-lint", "version": "v2.11.4" }
  ],
  "already_present": [
    { "tool": "govulncheck", "version": "v1.3.0" },
    { "tool": "nilaway",     "version": "v0.0.0-20260515015210-fd187751154f" }
  ],
  "failed": [
    {
      "tool": "govulncheck",
      "version": "latest",
      "command": "go install golang.org/x/vuln/cmd/govulncheck@latest",
      "stderr": "go: module lookup disabled by GONOSUMCHECK\n"
    }
  ]
}
```

- `installed`: tools that were missing or at the wrong version and now installed
  successfully, with resolved version.
- `already_present`: tools whose binary was found at `filepath.Join(binDir, toolName)`
  with a matching version, with resolved version. A tool in `GOPATH/bin` that is not on
  `$PATH` can still appear here — `install_tools` uses the resolved `binDir`, not
  `exec.LookPath`.
- `failed`: each entry includes the exact `command` that failed and its `stderr`. The
  `version` field carries the requested specifier (e.g. `"latest"`) when resolution failed
  before install could begin, or the resolved version when the install itself failed after
  a successful `go list`.

Tools found at the wrong version are treated as missing and attempted re-install;
they appear in `installed` on success or `failed` on failure, never in `already_present`.

**Version discovery:** the server uses `go version -m` on the binary path at
`filepath.Join(binDir, toolName)`. This works universally for all three tools
(no per-tool flag guessing). The implementation is a two-step Go pattern, not a shell
one-liner:

```go
binaryPath := filepath.Join(binDir, toolName)
cmd := exec.Command("go", "version", "-m", binaryPath)
output, err := cmd.Output()
// parse "mod <module-path> <version>" lines
```

Example parsed versions:
- `mod github.com/golangci/golangci-lint/v2 v2.11.4` → `"v2.11.4"`
- `mod golang.org/x/vuln v1.3.0` → `"v1.3.0"`
- `mod go.uber.org/nilaway v0.0.0-20260515015210-fd187751154f` → `"v0.0.0-20260515015210-fd187751154f"`

If `go version -m` fails (e.g. binary was hand-built without module info), the entry
reports `{ "tool": "<name>", "version": "unknown" }` rather than omitting it or
fabricating a version. If the cached version is `unknown`, the server treats it as
always-matching the requested version to avoid re-install loops — the tool was found
and is presumed usable. `install_tools` can still force a re-install.

### Version Pinning

Versions are read from `.go-quality.yaml` in the target project. Falls back to the pinned
defaults shown in the Tools table above (not `latest` for golangci-lint).

```yaml
# .go-quality.yaml (project root)
timeout: 5m                          # per-tool deadline (default: 5m)

tools:
  golangci-lint:
    version: v2.11.4                 # pinned default; override with care
    extra_args: ["--no-config"]      # valid v2 flag; prepended before required flags
  govulncheck:
    version: latest
    extra_args: []                   # positional targets (e.g. ./...) are added automatically
  nilaway:
    version: latest
    extra_args: ["--exclude-pkgs=github.com/myorg/vendor"]
```

No file present → all tools run at their pinned defaults with zero extra args.

Server's required flags (like `--out-format=json`) are always prepended to the command
before `extra_args`. Because some tools use last-flag-wins flag parsing (e.g. golangci-lint
with pflag), a conflicting flag in `extra_args` appearing after a required flag could
override it. To prevent this, the server validates `extra_args` at config load time and
rejects any argument that conflicts with a server-managed flag, returning a fatal config
error: `"config error: extra_args for <tool> contains reserved flag <flag>"`.

### `latest` Resolution Policy

When `.go-quality.yaml` specifies `version: latest` (the default for govulncheck and
nilaway), the server resolves `latest` to a concrete version **once per cache lifetime**
(i.e., once per process, unless `install_tools` is called to force a fresh resolution):
on the first request that needs this tool's version (if it isn't already cached), the
server logs to stderr `"Resolving <tool>@latest..."`, then runs
`go list -m -json <pkg>@latest` and stores the resolved semver (e.g. `latest` → `v1.3.0`).
If the tool is not yet installed, skip `go list` resolution and proceed
directly to `go install <pkg>@latest`; after install, read the version from the new
binary via `go version -m`. All subsequent requests use the cached resolved version —
no network call on every check. Call `install_tools` to force a fresh resolution.

This avoids both problems:
- No network call penalty on every request (the cache is authoritative)
- No stale-tool eternity (`install_tools` forces a refresh)
- The nilaway pseudo-version problem disappears: `latest` resolves to a concrete
  pseudo-version (e.g. `v0.0.0-20260515...`) once, and it stays cached until
  `install_tools` is called

If `go list -m -json <pkg>@latest` succeeds and returns a version newer than what is
installed, a re-install is triggered via `go install <pkg>@<resolved>`. If that re-install
fails (e.g. network disappears between the `go list` and `go install` calls), the server
falls back to the already-installed version, logs the failure to stderr, and caches the
installed version as `unknown` to prevent re-install loops on subsequent requests. The
tool remains usable at its current version.

If `go list -m -json <pkg>@latest` fails outright (e.g. network unavailable, private
proxy misconfigured), the server falls back to whatever is installed and sets the version
to `unknown` in the cache — treating it as always-matching. The tool is still usable;
`install_tools` will also fail in this environment, which the agent will see in the
`failed` array.

Tools pinned to a specific semver (e.g. `golangci-lint: v2.11.4`) do not need
resolution — the cache stores the exact version directly.

---

## Configuration

### Precedence Hierarchy

When settings conflict, the following order applies (highest to lowest priority):

1. **CLI flags / process environment** — arguments passed by the MCP client when launching
   the binary (e.g. `--verbose`, env vars). Always wins. The `--config` flag controls which
   file to read for level 2 — it is not itself a setting override. Only explicit flags like
   `--verbose` are level 1.
2. **YAML file content** — whichever file `--config` points to (default: discovered at
   workspace root). Contains tool versions, `extra_args`, and `timeout`.
3. **Compiled-in defaults** — pinned tool versions and 5-minute timeout baked into the binary.

This means: if the MCP client passes `--config /path/to/custom.yaml`, that file's content
is still level 2 — it can be overridden by explicit CLI flags at level 1. The `--config`
flag selects the *source* of level 2, not its rank.

### Config Sources

The server uses two independent paths for config loading and root discovery:

1. **Config file source:** controlled by `--config`. If `--config /custom/path.yaml` is
   passed, that file IS the config — root discovery does not override it. If `--config` is
   absent (default), the server uses root discovery to find the workspace root and looks for
   `.go-quality.yaml` there. The two mechanisms are mutually exclusive for config loading.
2. **Root discovery:** always runs regardless of `--config`, because the discovered root
   determines `cmd.Dir` for tool execution. `--config` only affects which YAML file is read —
   it has no bearing on where the tools are invoked from.

The server does **not** duplicate golangci-lint's linter config. That lives in
`.golangci.yml` as normal.

---

## Recommended Agent Workflow

The correct order for agentic consumption is:

1. **Call `install_tools` once at session start.** This re-checks all three tools on disk
   synchronously and returns a structured result. The agent should check the `failed`
   array and notify the user if any tool could not be installed.
2. **Call `run_code_checks` for each Go project.** The server auto-discovers the workspace
   root, validates it, and runs all (or a subset of) checkers in parallel.
3. **Process the unified `Diagnostic[]` array** — filter by `severity`, navigate by
   `file:line:column`, read `message` for a summary, and inspect `native` for
   tool-specific remediation instructions (SuggestedFixes, CVE aliases, trace data).

Single-tool shortcuts (`run_lint`, `run_vuln_check`, `run_nil_check`) are available for
incremental use (e.g., re-running only lint after a fix), but the initial scan should
use `run_code_checks` for parallel execution. The `tools` parameter on `run_code_checks`
lets agents skip a tool that's known to be problematic (e.g. nilaway in a monorepo that
hasn't been annotated yet).

The project includes an `AGENTS.md` with executable examples of each MCP call, expected
inputs/outputs, and troubleshooting guidance.

---

## CLI

```
mcp-server-go-quality
  --config PATH     path to .go-quality.yaml (default: .go-quality.yaml at discovered workspace root)
  --verbose         emit diagnostic logging to stderr
  --version         print version (+ commit hash, + dirty flag)
```

The `--version` flag uses `runtime/debug.ReadBuildInfo()` for VCS metadata, matching
the pattern used by cryptospect-cli.

---

## Timeout Model

**The `timeout` field is a per-tool budget.** Each checker goroutine gets an independent
deadline. A slow tool times out without cancelling siblings. The total wall clock is
at most `cfg.Timeout`. The timeout governs subprocess execution only — it does not cover
the pre-flight install phase. A cold `go install` of all three tools on a slow network
can take 5–10 minutes and runs before the per-tool budget starts. Call `install_tools`
at session start to move the install cost outside of check request latency.

```go
type runResult struct {
    diagnostics []Diagnostic
    err         error
}

// Parent context carries cancellation only — no deadline.
// This lets each tool run independently without a shared clock.
ctx, cancel := context.WithCancel(parentCtx)
defer cancel()

results := make(chan runResult, len(activeCheckers))
var wg sync.WaitGroup

for _, check := range activeCheckers {
    wg.Add(1)
    go func(chk Checker) {
        defer wg.Done()
        defer func() {
            if r := recover(); r != nil {
                results <- runResult{
                    diagnostics: []Diagnostic{{
                        Tool:  chk.Name(),
                        Error: fmt.Sprintf("internal panic: %v", r),
                    }},
                }
            }
        }()
        // Independent per-tool timeout from the same clock-start moment.
        // If this tool times out, only this goroutine's subprocess is killed.
        toolCtx, toolCancel := context.WithTimeout(ctx, cfg.Timeout)
        defer toolCancel()

        diags, err := chk.Run(toolCtx, projectPath)
        results <- runResult{diags, err}
    }(check) // passed by value — idiomatic; loop-variable capture fixed in Go ≥1.22
}

wg.Wait()
close(results)
// drain results channel after close
```

**Panic recovery** is required in every goroutine. Without it, a panic fires
`defer wg.Done()` but skips the `results <- runResult{...}` send — `wg.Wait()` unblocks
with fewer results than expected and no indication of the missing tool's output.

**Single-tool handlers** (`run_lint`, `run_vuln_check`, `run_nil_check`) derive their own
`context.WithTimeout(ctx, cfg.Timeout)` directly — same per-tool budget, one goroutine.
No parent cancel context is needed (there is only one checker running).

**`context.Canceled`** (parent context cancelled, e.g. client disconnect) produces a
Diagnostic with `error: "cancelled"`. This is distinct from `DeadlineExceeded`
("timed out after 5m0s"). Cancellation tells the agent the caller disconnected, not
that this tool was slow.

**Note on first-run latency:** govulncheck downloads the Go vulnerability database
(approximately 40 MB, growing over time) on its first invocation. This happens inside
tool execution, not during `install_tools`. On a slow network this can take 1-2 minutes
in addition to analysis time. The database is cached locally afterward. Agents should
call `install_tools` at session start and consider increasing `timeout` to `10m` for
the first scan.

---

## Go Workspace Support

When the target directory contains a `go.work` file (Go 1.18+ workspace), the server
runs tools from that directory's context.

### Root Discovery

The server uses a **two-pass upward walk** from `project_path`:

**Pass 1 — workspace root:** walk from `project_path` toward `/`, checking each directory
for a `go.work` file. Stop at the first directory that contains `go.work`. If found,
that directory is the project root.

**Pass 2 — module root (only if Pass 1 finds nothing):** reset to `project_path` and
walk upward again, checking each directory for a `go.mod` file. Stop at the first
directory that contains `go.mod`. If found, that directory is the project root.

If both passes reach `/` without a match, return a fatal error ("not a Go project").

**Why two passes, not one:** a single combined pass (stop at the first `go.work` or
`go.mod`, whichever is closer) would incorrectly stop at a module-local `go.mod` when a
`go.work` exists higher in the tree. For example, if `project_path` is
`/project/monorepo/services/auth` (which contains a `go.mod`) and a `go.work` exists at
`/project/monorepo/go.work`, a single-pass algorithm stops at
`/project/monorepo/services/auth/go.mod` and misses the workspace root. The two-pass
approach guarantees `go.work` wins over any closer `go.mod`.

The resolved root is used as `cmd.Dir` for all tool invocations and for `go list -m`.

**Path traversal safety:** `project_path` is validated by the two-pass walk — only
directories containing a `go.mod` or `go.work` are accepted as roots. A path such as
`../../../../etc` finds no Go project files and returns a fatal error, not a security
bypass.

**Monorepos without `go.work`:** Some Go monorepos group multiple modules in subdirectories
but do not use a `go.work` file (relying instead on `replace` directives or separate
build pipelines). In this case root discovery will stop at the nearest `go.mod` ancestor.
The agent must target `project_path` at the specific module directory containing the
`go.mod` it wants analyzed — there is no upward discovery across module boundaries
without `go.work`. This is by design: analyzing an unrelated parent directory would
produce incorrect results. When working with such a monorepo, run a separate check for
each module directory.

### Single-module projects

The server checks for `go.work` first (Pass 1), falling back to `go.mod` (Pass 2). Tool
invocations use `./...` from the project root; this resolves correctly in both cases.

### Multi-module workspaces (go.work with multiple `use` directives)

For **nilaway**, which requires `-include-pkgs` to avoid scanning vendor code or the
standard library, a single `go list -m` from the root is insufficient — it returns only
the module containing CWD.

Resolution:
1. Detect `go.work` at the project root.
2. Parse the `use` directives directly from the `go.work` file to get the relative paths
   of each member module (e.g. `use ./services/auth`). For each relative path, read the
   corresponding `go.mod` file and extract its `module` declaration. Do **not** use
   `go list -m -json all` — that returns all transitive dependencies (potentially hundreds
   of modules), not just workspace members. Passing them all to `-include-pkgs` would
   instruct nilaway to scan vendor code and the standard library, causing out-of-memory
   crashes or multi-minute hangs.
3. Build `-include-pkgs=<mod1>,<mod2>,...` from all collected module paths. nilaway
   treats these values as **import path prefixes** — passing `github.com/myorg/repo/services/auth`
   correctly covers all packages within that module (e.g.
   `github.com/myorg/repo/services/auth/pkg/db`). Use the root module path from each
   `go.mod`'s `module` directive, not individual package paths.
4. Pass `./...` as the target so nilaway evaluates all modules.

For **golangci-lint** and **govulncheck**, `./...` in a workspace context resolves
against all workspace modules natively — no extra flags needed.

---

## Error Handling

### Decision Rule: Top-Level Error vs Diagnostic Error

**Infrastructure and validation failures are fatal** — they return a top-level JSON-RPC
error with no `Diagnostics` array:

- `project_path` does not exist
- `project_path` has no `go.mod` or `go.work`
- `.go-quality.yaml` present but unparseable
- `.go-quality.yaml` `extra_args` contains a reserved server-managed flag
- `go` binary not on PATH (required for auto-install)
- `tools` parameter contains an unrecognised checker name

**Tool execution failures are informational** — they are returned as `Diagnostic` objects
with the `Error` field set; other tools' results are still collected and returned:

- Tool not installed (after attempted auto-install)
- Tool exit code ≠ 0
- Tool timeout
- Tool returns non-JSON output

### Error Table

| Scenario | Classification | Behavior |
|---|---|---|
| Tool not installed | Diagnostic | Auto-install once (pre-flight, sequential). If install fails → `Diagnostic.Error` with `"install failed: ..."` format |
| Tool exit code ≠ 0 | Diagnostic | `Diagnostic.Error`: `"Tool command failed with exit code N. Stderr: <content>"` |
| Tool times out | Diagnostic | `Diagnostic.Error`: `"timed out after <duration>"` |
| Tool cancelled | Diagnostic | `Diagnostic.Error`: `"cancelled"` (parent context cancelled, e.g. client disconnect) |
| Tool returns valid JSON, no findings | — | Empty result slice — success |
| Tool returns non-JSON on stdout | Diagnostic | `Diagnostic.Error`: `"unexpected output format from <tool>"` |
| Tool exits non-zero with compiler/syntax errors in stderr | Diagnostic | Same as any non-zero exit; agent should check stderr for `syntax error` or `undefined:` patterns to distinguish build failure from tool failure. nilaway is the most common case — it requires a fully type-checked program. |
| `project_path` doesn't exist | Fatal | Top-level error, no diagnostics |
| `project_path` has no `go.mod`/`go.work` | Fatal | Top-level error: `"not a Go project"` |
| `.go-quality.yaml` unparseable | Fatal | Top-level error: `"config parse error: <detail>"` |
| `extra_args` contains reserved flag | Fatal | Top-level error: `"config error: extra_args for <tool> contains reserved flag <flag>"` |
| `go` binary not on PATH | Fatal | Top-level error: `"go binary not found"` |
| `tools` contains unrecognised value | Fatal | Top-level error: `"unknown tool: \"<value>\". valid values: golangci-lint, govulncheck, nilaway"` |

### Pre-Flight Sequential Install

Before the check goroutines run, a synchronous pass discovers and installs **only the tools
that are about to run**. For `run_code_checks`, this means the tools in the `tools` subset
(or all three if omitted). For single-tool handlers (`run_lint`, `run_vuln_check`,
`run_nil_check`), only the one tool is discovered and installed. A `run_code_checks` with
`tools: ["golangci-lint"]` is identical in cost and behaviour to `run_lint` — neither pays
nilaway's install cost. Missing or wrong-version tools are installed **sequentially** to
avoid Go module cache lock races.

### Stderr Capture

Stderr is always captured and included verbatim in `Diagnostic.Error` on non-zero exit.
Install progress messages go to the server's own stderr (the MCP client captures these
as out-of-band notifications).

---

## Testing Strategy (TDD)

- Red-green-refactor cycle enforced for every feature.
- **Unit tests:** pure functions — parsers, path resolution, config loading, error formatting.
- **Integration tests:** real `exec.Command` against `testdata/sample_project/` — a small Go project with intentional issues for each tool to detect.
- **Edge cases:** missing tools, broken config, empty project, non-Go directory, timeout, multi-module workspace.
- **Mockable `CommandRunner` interface** — handlers accept `CommandRunner` so tests can swap real subprocess execution for deterministic test doubles without running the actual tools.
- **`extra_args` test data:** use only verified flags (`--no-config` or `--disable-all` for golangci-lint; `./...` positional target for govulncheck). Do not use `--fast` or `--scan=package` — these are not valid flags in their respective tools' current versions.

### testdata/sample_project Minimum Content

The sample project must exercise all three checkers:

- **golangci-lint (gocognit/gocyclo):** One function with cyclomatic complexity > 15
  (deeply nested conditionals / loops)
- **nilaway:** One nil-dereference path (e.g. `return nil` for a `*T` return type,
  then dereference the result without checking)
- **govulncheck:** A `go.mod` with a pinned dependency known to have a recorded
  vulnerability, e.g. `golang.org/x/net v0.0.0-20210226172049-4d89b558e7d3` (has
  multiple GO-YYYY-NNNN entries). The test project must include at least one `.go`
  source file that imports and calls a symbol from the vulnerable package (e.g.
  `import "net/http"` + `http.Get(...)`, or `import "golang.org/x/net/http2"` +
  `http2.ConfigureServer(...)`) — govulncheck only reports vulnerabilities reachable
  via the call graph; a blank identifier import (`import _ "..."`) does not create
  a call-graph edge and will produce no findings. Pin the version explicitly in
  `go.mod` and commit a `go.sum`. This makes the test deterministic regardless of
  the local vuln DB version — the vulnerability is historical and will always be
  present.

---

## Implementation Plan

Will be generated by the `writing-plans` skill after this spec is approved.

---

## References

- `docs/tools-research.md` — detailed tool overlap analysis and output format research
- `docs/superpowers/specs/AGENTS.md` — agent usage guide with executable MCP call examples
- `/project/cryptospect-cli/internal/version/version.go` — version pattern to follow
- `/project/cryptospect-cli/.golangci.yml` — existing golangci-lint config (reference)
- `spec-v1.md` — superseded first draft
- `spec-v1-ambiguities.md` — review that drove the v1→v2 changes
- `spec-v2-review1.md` — first adversarial review
- `spec-v2-review2.md` — second adversarial review with research findings
- `spec-v2-review3.md` through `spec-v2-review6.md` — subsequent review iterations
- `spec-v3-review1.md` — adversarial review of v3 (32 findings)
