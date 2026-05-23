# mcp-server-go-quality — Design Spec v3


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
- **Parallel by default** — `runAll` fires up to 3 goroutines with independent per-tool timeout contexts; a cancel-only parent context supports clean shutdown.
- **Lazy install with version-aware caching** — tool discovery runs on the first incoming request from any check tool. The cache tracks `toolName → resolvedVersion` (e.g. `govulncheck → v1.3.0`), not the requested specifier (`latest`). The resolved version is read from the installed binary via `go version -m`. When `.go-quality.yaml` requests a different version, a re-install is triggered and the cache is updated with the new resolved version. `install_tools` bypasses the cache and force-reinstalls the requested subset of tools, then updates only those cache entries with the newly resolved versions — guaranteeing the next check call has a warm cache. The cache is protected by a `sync.RWMutex` — read locks for lookups, write locks for installs and cache updates — so concurrent requests don't trigger duplicate installs.
- **One file per tool** — `golangci_lint.go`, `govulncheck.go`, `nilaway.go`
- **Single entry point** — `cmd/mcp-server-go-quality/main.go`
- **Logging to stderr only** — all progress and install messages go to `stderr`; `stdout` carries only JSON-RPC traffic.

### MCP Tools Registered

| Tool Name | Description |
|---|---|
| `run_code_checks` | Run all 3 checkers in parallel (configurable subset), return unified results |
| `run_lint` | Run golangci-lint only |
| `run_vuln_check` | Run govulncheck only |
| `run_nil_check` | Run nilaway only |
| `install_tools` | Pre-install required Go tools (configurable subset, pinned versions) |

Each check tool accepts:
- `project_path` (optional, string) — defaults to server's CWD

`run_code_checks` also accepts:
- `tools` (optional, array of strings) — subset of checkers to run. Valid values:
  `"golangci-lint"`, `"govulncheck"`, `"nilaway"`. Omitted or empty = all three.

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
  Walk up directory tree to find go.work or go.mod (root discovery)
  Validate root is a Go project (go.mod or go.work exists)           ← fatal if missing
  Pre-flight: synchronous discovery of requested tools (version-aware cache)
    → install any missing/wrong-version requested tools sequentially ← avoids go install race
    → log progress to stderr only
  Derive cancel-only parent context: context.WithCancel(parent)
  Filter to requested tools (from `tools` param, default all)
  Fire goroutines only for requested tools; loop variable passed as argument
  to avoid closure capture bug:
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
    Severity string          `json:"severity,omitempty"`// "error" | "warning" | "" if tool has no concept
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

**Severity mapping by tool:**
- golangci-lint: `issue.Severity` (`"warning"` or `"error"`)
- govulncheck: always `""` (Go vulnerability database has no severity field)
- nilaway: always `""` (nilaway has no severity concept)

**When `Error` is non-empty**, the Diagnostic carries a tool-level failure:
- `File`, `Line`, `Column`, `Severity`, `Message` are zero-valued (not applicable).
- `Native` is zero-valued *unless* the error is an NDJSON parse failure — in that one
  case, `Native` carries a JSON-encoded string of the raw unparseable content so a
  developer can inspect what govulncheck emitted (see govulncheck section).

**When `Error` is empty**, the Diagnostic carries a finding:
- All fields are populated normally from the tool's native output.
- `Severity` may still be `""` for tools that have no severity concept.

### Standardised Error String Format

All `error` field values use this exact format:

```
Tool command failed with exit code <N>. Stderr: <content>
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
    {"module": "stdlib", "package": "net/http",
     "position": {"filename": "src/net/http/client.go", "line": 586, "column": 18}},
    {"module": "github.com/user/project", "package": "...httpclient",
     "position": {"filename": "internal/httpclient/client.go", "line": 78, "column": 25}}
  ]
}}
```

Extraction rules for the unified `Diagnostic` fields:
- `File`/`Line`/`Column`: from the last `finding.trace` entry that has a `position` (the call site in user code)
- `Severity`: always `""` (Go vulnerability database has no severity field)
- `Message`: from `osv.summary`. If `osv.summary` is absent, fall back to the `finding.osv`
  ID (e.g. `"GO-2026-4918"`). Do not use `osv.details` as a fallback — govulncheck's
  streamed NDJSON output omits the `details` field even when present in the full OSV
  database record, making it unreliable as a fallback source.
- `Native`: the raw `finding` object + associated `osv` object

**Note on multi-module workspaces:** the "last entry with a position" heuristic picks the
deepest call site in user code. In a workspace with multiple modules, this may point to a
shared library module rather than the specific service an agent is working on. Agents
should check `Native.finding.trace` for the full call chain when the file location appears
to be in a dependency.

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
- `File`: strip project root prefix using `filepath.Rel(projectRoot, absPath)`. If `filepath.Rel` returns an error (e.g. cross-drive or symlink boundary), fall back to `strings.TrimPrefix(absPath, projectRoot)`. If the path is already relative or stripping still yields an absolute path, preserve it as-is rather than returning `""`.
- `Line`/`Column`: parsed from `posn` (`file.go:line:col`)
- `Severity`: always `""` (nilaway has no severity concept)
- `Message`: first sentence of the `message` field. A "sentence" is everything up to the first `. ` (period + space) followed by an uppercase ASCII letter `[A-Z]`, or up to the first `\n`. If no such boundary is found, use the full `message`. This avoids false boundaries from function signatures containing dots (e.g. `pkg.Type.Method`).
- `Native`: the raw nilaway error object

---

## Tool Discovery & Installation

### Auto-Install Rules

1. On the **first incoming request from any check tool** (`run_code_checks`, `run_lint`,
   `run_vuln_check`, `run_nil_check`), discover each **requested** tool via `exec.LookPath`.
   The server reads the installed version via `go version -m` on the discovered binary path
   (see install_tools Output Format for the two-step Go pattern) and caches the resolved
   version (`toolName → v1.3.0`). When `.go-quality.yaml` requests a different version
   than the cached one, a re-install is triggered.
2. If any requested tool is missing (not on PATH) or has the wrong version, install all
   such tools **sequentially** to avoid Go module cache lock races from concurrent
   `go install` calls.
3. Log a clear message to **stderr**: `"Installing <tool>@<version>... this happens once."`
   — never to stdout, which carries JSON-RPC traffic.
4. If install fails, return a Diagnostic with the `error` field set to the exact `go install`
   command that failed and its stderr output.
5. The `install_tools` MCP tool lets agents pre-install proactively before any analysis runs.
   It is the **recommended first step** in agent workflows — calling it at session start
   forces a synchronous install with progress feedback, avoiding a silent 2-5 minute wait
   on the first check call. `install_tools` bypasses the version cache and force-reinstalls
   the requested `tools` subset (or all three if omitted), then updates only those cache
   entries with the newly resolved versions. After `install_tools` completes, the next check
   call has a warm cache and zero pre-flight overhead.

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

- `installed`: tools that were missing and now installed successfully, with resolved version.
- `already_present`: tools already on PATH with a matching version, with resolved version.
- `failed`: each entry includes the exact `command` that failed and its `stderr`. The
  `version` field carries the requested specifier (e.g. `"latest"`) when resolution failed
  before install could begin, or the resolved version when the install itself failed after
  a successful `go list`.

Tools found on PATH at the wrong version are treated as missing and attempted re-install;
they appear in `installed` on success or `failed` on failure, never in `already_present`.

**Version discovery for `already_present`:** the server uses `go version -m` on the
binary path discovered via `exec.LookPath`. This works universally for all three tools
(no per-tool flag guessing). The implementation is a two-step Go pattern, not a shell
one-liner:

```go
binaryPath, err := exec.LookPath(toolName)
if err != nil { /* tool not on PATH */ }
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
on PATH and is presumed usable. `install_tools` can still force a re-install.

### Version Pinning

Versions are read from `.go-quality.yaml` in the target project. Falls back to the pinned
defaults shown in the Tools table above (not `latest` for golangci-lint).

```yaml
# .go-quality.yaml (project root)
timeout: 5m                          # per-tool deadline (default: 5m)

tools:
  golangci-lint:
    version: v2.11.4                 # pinned default; override with care
    extra_args: ["--no-config"]      # valid v2 flag; appended after required flags
  govulncheck:
    version: latest
    extra_args: []                   # positional targets (e.g. ./...) are added automatically
  nilaway:
    version: latest
    extra_args: ["--exclude-pkgs=github.com/myorg/vendor"]
```

No file present → all tools run at their pinned defaults with zero extra args.
Server's required flags (like `--out-format=json`) are always applied and cannot be
overridden via `extra_args`.

### `latest` Resolution Policy

When `.go-quality.yaml` specifies `version: latest` (the default for govulncheck and
nilaway), the server resolves `latest` to a concrete version **once per process lifetime**:
on the first request that needs this tool's version (if it isn't already cached), the
server logs to stderr `"Resolving <tool>@latest..."`, then runs
`go list -m -json <pkg>@latest` and stores the resolved semver (e.g. `latest` → `v1.3.0`).
If the tool is not yet on PATH (not installed), skip `go list` resolution and proceed
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

1. **Call `install_tools` once at session start.** This force-installs all three tools
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
at most `cfg.Timeout`.

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
        // Independent per-tool timeout from the same clock-start moment.
        // If this tool times out, only this goroutine's subprocess is killed.
        toolCtx, toolCancel := context.WithTimeout(ctx, cfg.Timeout)
        defer toolCancel()

        diags, err := chk.Run(toolCtx, projectPath)
        results <- runResult{diags, err}
    }(check) // check passed by value — avoids loop-variable closure capture bug
}

wg.Wait()
close(results)
// drain results channel after close
```

**Single-tool handlers** (`run_lint`, `run_vuln_check`, `run_nil_check`) derive their own
`context.WithTimeout(ctx, cfg.Timeout)` directly — same per-tool budget, one goroutine.
No parent cancel context is needed (there is only one checker running).

**`context.Canceled`** (parent context cancelled, e.g. client disconnect) produces a
Diagnostic with `error: "cancelled"`. This is distinct from `DeadlineExceeded`
("timed out after 5m0s"). Cancellation tells the agent the caller disconnected, not
that this tool was slow.

**Note on first-run latency:** govulncheck downloads the Go vulnerability database
(~40 MB) on its first invocation. This happens inside tool execution, not during
`install_tools`. On a slow network this can take 1-2 minutes in addition to analysis
time. The database is cached locally afterward. Agents should call `install_tools` at
session start and consider increasing `timeout` to `10m` for the first scan.

---

## Go Workspace Support

When the target directory contains a `go.work` file (Go 1.18+ workspace), the server
runs tools from that directory's context.

### Root Discovery

The server walks **up** from `project_path` looking for `go.work`, then `go.mod`. If
`project_path` is `/project/monorepo/services/auth` and a `go.work` exists at
`/project/monorepo/go.work`, the resolved root is `/project/monorepo`. This is the
directory used for `cmd.Dir` and `go list -m`. The walk stops at the filesystem root
(`/`). If no `go.work` or `go.mod` is found before reaching the root, the server returns
a fatal error ("not a Go project").

### Single-module projects

The server checks for `go.work` first, falling back to `go.mod`. Tool invocations use
`./...` from the project root; this resolves correctly in both cases.

### Multi-module workspaces (go.work with multiple `use` directives)

For **nilaway**, which requires `-include-pkgs` to avoid scanning vendor code or the
standard library, a single `go list -m` from the root is insufficient — it returns only
the module containing CWD.

Resolution:
1. Detect `go.work` at the project root.
2. Parse the `use` directives (or run `go list -m -json all`) to collect all workspace module paths.
3. Build `-include-pkgs=<mod1>,<mod2>,...` from all collected module paths.
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
| Tool not installed | Diagnostic | Auto-install once (pre-flight, sequential). If install fails → `Diagnostic.Error` |
| Tool exit code ≠ 0 | Diagnostic | `Diagnostic.Error`: `"Tool command failed with exit code N. Stderr: <content>"` |
| Tool times out | Diagnostic | `Diagnostic.Error`: `"timed out after <duration>"` |
| Tool cancelled | Diagnostic | `Diagnostic.Error`: `"cancelled"` (parent context cancelled, e.g. client disconnect) |
| Tool returns valid JSON, no findings | — | Empty result slice — success |
| Tool returns non-JSON on stdout | Diagnostic | `Diagnostic.Error`: `"unexpected output format from <tool>"` |
| Tool exits non-zero with compiler/syntax errors in stderr | Diagnostic | Same as any non-zero exit; agent should check stderr for `syntax error` or `undefined:` patterns to distinguish build failure from tool failure. nilaway is the most common case — it requires a fully type-checked program. |
| `project_path` doesn't exist | Fatal | Top-level error, no diagnostics |
| `project_path` has no `go.mod`/`go.work` | Fatal | Top-level error: `"not a Go project"` |
| `.go-quality.yaml` unparseable | Fatal | Top-level error: `"config parse error: <detail>"` |
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
- `spec-v2-review.md` — first adversarial review
- `spec-v2-review2.md` — second adversarial review with research findings