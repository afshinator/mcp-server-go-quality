

## Spec Ambiguities — Problems in the Spec Itself

These are issues not with the plan but with the spec — ambiguous or contradictory requirements that would lead to inconsistent implementation.

### A — Timeout semantics: per-tool or shared?
- **Spec says:** "timeout: 5m — per-tool deadline (default: 5m)"
- **Spec rationale:** "for large monorepos where golangci-lint or govulncheck database downloads can exceed the default 5 minutes" — suggests per-tool
- **Plan does:** Single shared `context.WithTimeout` applied to all 3 goroutines
- **Ambiguity:** If one tool hits the deadline, it cancels the others via shared context. Is this the intent? If the 5-minute budget is per-tool, each should get its own independent timeout. The spec never clarifies whether the timeout is per-tool or for the entire operation.
- **Recommendation:** Clarify: "timeout is a shared deadline for all tools, not per-tool"

Verdict: Highly Appropriate. * Analysis: The plan’s use of a single context.WithTimeout directly attached to the parent group means that if any tool reaches the 5-minute threshold, it signals the context cancellation channel. Because all three tools share that exact context, the remaining running tools are instantly aborted mid-flight. This completely undermines your goal of collecting partial results from faster tools when a slow database synchronization or a giant monorepo run drags one tool down.

Best Practice Resolution: Use a hybrid approach. Give the orchestrator a total macro-budget timeout, but pass independent, derived sub-contexts to each tool runner. If a tool fails due to its specific timeout, capture its individual diagnostic error string and preserve the successful results of the concurrent processes.

```
// Best Practice Implementation Pattern
ctx, macroCancel := context.WithTimeout(parentCtx, totalTimeout)
defer macroCancel()

for _, check := range activeCheckers {
    go func(chk Checker) {
        // Individualized, non-cascading execution
        toolCtx, toolCancel := context.WithTimeout(ctx, totalTimeout)
        defer toolCancel()
        
        res, err := chk.Run(toolCtx, projectPath)
        // If toolCtx times out, only this loop's chk.Run is dropped
    }(check)
}
```


### B — go.work + nilaway module resolution
- **Spec says:** "When the target directory contains a go.work file... all ./... patterns resolve against the modules listed in go.work"
- **Ambiguity:** When `go list -m` is called from a go.work context, it returns the module containing the CWD. If the project has multiple modules, which one is returned? nilaway's `-include-pkgs` flag is given the module name — but a workspace could have many.
- **Plan behavior:** `readModuleName` calls `go list -m` from the server's CWD (bug: #6), which would return that directory's module. Unclear what this means in a multi-module workspace.
- **Recommendation:** Clarify behavior for go.work. Does nilaway run against all workspace modules or just one?

Verdict: Completely Accurate & Structural Gap.

Analysis: If the target repository has a multi-module go.work file layout (e.g., matching a microservices monorepo format), executing go list -m blindly from the project root will fail or return incomplete scope parameters. Furthermore, nilaway requires explicit prefixes in its -include-pkgs flag to ensure it doesn't spend 10 minutes scanning third-party vendor code or standard libraries. If you only provide one module prefix inside a multi-module workspace, nilaway will bypass scanning your other local packages entirely.

Best Practice Resolution: Do not rely on single runtime directory resolution flags like go list -m if a go.work file is discovered. Instead, follow standard Go workspace tool conventions:

Check for the presence of a go.work file at the root.

If found, parse the file strings programmatically (or execute go list -m -json all) to extract the absolute module prefixes for every workspace-declared package directory.

Join those collected package paths into a comma-delimited string array passed to nilaway: -include-pkgs="github.com/my/mod1,github.com/my/mod2".

Pass ./... to evaluate all modules inside the workspace context natively.



### C — Auto-install trigger and "on first call" meaning
- **Spec says:** "On first call, discover each tool via exec.LookPath"
- **Spec also says:** "install any missing tools sequentially (pre-flight)"
- **Ambiguity:** What does "first call" mean — per server process, per tool, or per invocation? The plan calls `ensureToolsInstalled` on every `RunAllChecks` call (not just first). But `ToolInfo.IsInstalled()` caches its result, so subsequent calls skip install. This is effectively correct behavior but doesn't match the spec's "once" wording.
- **Also:** `install_tools` MCP tool iterates tools, checks `IsInstalled()`, and adds to "installed" slice — but never re-installs already-present tools. This is correct, but the plan's message format (#11) is broken.

Verdict: Appropriate (Exposes a Semantic Disconnect).

Analysis: The spec uses conversational phrasing ("On first call"), whereas the plan uses concrete, runtime caching logic (ToolInfo.IsInstalled()). Your criticism identifies that the plan's message format in Task 11 didn't track state cleanly, exposing why the wording was ambiguous.

Best Practice Resolution: For background service processes like an MCP server, the standard convention is Lazy Execution with Atomic State Verification.

The server should not execute slow shell-outs or network operations (LookPath or go install) during its startup sequence (main.go), as that delays transport connection mapping over stdio.

Instead, initialize an internal state struct tracking installation status when RunAllChecks is triggered. Protect this structural validation using a standard concurrency control primitive like a sync.Once wrapper or an explicit thread-safe atomic boolean. Once verified or executed on the initial incoming tool request, the server can rely safely on its internal memory map for the lifecycle of that specific process run.



### D — MCP client config vs .go-quality.yaml precedence
- **Spec says:** "The server has two config sources: 1. MCP client config (.mcp.json or equivalent) — launches the binary, may pass env vars 2. .go-quality.yaml in the target project — tool versions and extra args only"
- **Ambiguity:** When both are present and conflict (e.g., different versions), which wins? Can MCP client config set the config file path? Can it set timeout?
- **Recommendation:** Define precedence. Suggest: CLI flags (--config) > .go-quality.yaml > defaults

Verdict: Highly Appropriate.

Analysis: Without an explicit precedence hierarchy, configuration management cascades into unpredictable states. For example, if an orchestration environment specifies a 2-minute total budget but a local repo's .go-quality.yaml sets timeout: 10m, the server will drift out of sync with its parent client.

Best-Practice Rule: Implement a conventional strict hierarchy:

Dynamic CLI Flags / Process Env: Arguments explicitly passed by the launching MCP client process always take absolute top priority.

Local Workspace Profile: The target repository's .go-quality.yaml file overrides core system fallbacks.

Internal Baseline Defaults: Hardcoded platform constraints (like the default 5-minute timeout).




### E — Handler signature spec vs implementation needs
- **Spec says:** "handlers are designed as `func runX(path string) ([]Diagnostic, error)` with no hidden state per call"
- **Spec data flow:** handlers receive timeout context implicitly
- **Reality:** Handlers need `context.Context` (for timeout), `runner.CommandRunner` (for mockability), and `projectPath`. The plan adds both.
- **Ambiguity:** The spec's pure function signature `func runX(path string)` is too simple — it omits runtime dependencies. The plan's `func runGolangciLint(ctx context.Context, r runner.CommandRunner, projectPath string)` is the correct design, but it contradicts the spec.
- **Recommendation:** Update spec signature to `func runX(ctx context.Context, r CommandRunner, path string) ([]Diagnostic, error)` or acknowledge that context is implicit via the data flow

Verdict: Completely Accurate.

Analysis: The spec's idealized func runX(path string) is completely un-testable and lacks proper resource management. In production Go, any blocking call or subprocess fork must accept a context.Context to handle upstream cancellations, along with a mockable executor mechanism to avoid running expensive, mutating system calls inside pure unit tests.

Best-Practice Rule: Update the spec directly to match Go's idiomatic concurrency signatures: func runX(ctx context.Context, runner CommandRunner, projectPath string) ([]Issue, error).


### F — Auto-install log message never implemented
- **Spec says:** "Log a clear message: 'Installing <tool>@<version>... this happens once.'"
- **Plan:** No logging code anywhere in the implementation plan. The spec requires this but it never appears in any task step.
- **Impact:** The spec promises user feedback during auto-install, but an agent implementing this plan would have no idea to add it.
- **Recommendation:** Add install logging to `ensureToolsInstalled` or `ToolInfo.Install()`

Auto-Install Log Message Never Implemented
Verdict: Highly Appropriate.

Analysis: This is a classic gap where a plan drops user experience requirements stated in the design. Because MCP communication uses stdio streams for structural JSON RPC traffic, standard fmt.Println debugging or logging commands directly to stdout will corrupt the JSON pipe, causing the parent client to crash instantly.

Best-Practice Rule: The spec and plan must explicitly require that these auto-install progress logs be piped directly to stderr (log.New(os.Stderr, "", 0)), which MCP environments natively capture as out-of-band user notifications.



### G — golangci-lint version spec vs config example
- **Spec says:** "Exact Issues wrapper structure and gosec field names must be validated against installed v2.11.4 on first run"
- **Spec config example:** `version: latest`
- **Ambiguity:** The spec treats v2.11.4 as the reference but the config default is `latest`. If `latest` is v2.12+ or v3.x, the output format may differ from what's documented. The spec acknowledges this gap ("must be validated on first run") but provides no plan for it.
- **Recommendation:** Pin the default to v2.11.4 or document the validation procedure

Verdict: Highly Appropriate.

Analysis: Relying on latest for external complex linters is highly volatile. If latest pulls down a breaking major structural version upgrade, the structural JSON parsing assumptions coded into your server's handler will fail silently or throw parsing errors.

Best-Practice Rule: The default configuration blueprint must explicitly pin the tool versions to a fixed manifest (e.g., golangci-lint: v1.64.0, nilaway: v0.0.0-2026...). It should only switch versions if explicitly overridden by a workspace configuration profile.



### H — File path stripping edge cases for nilaway
- **Spec says:** "File: strip project root prefix from posn, parse out filename"
- **Plan does:** `if rel, err := filepath.Rel(projectPath, file); err == nil { file = rel }` — uses relative path only if no error
- **Ambiguity:** If `filepath.Rel` returns an error (e.g., file is on a different drive in Windows, or paths cross symlink boundaries), the plan keeps the original absolute path. Should it return `File: ""`? The spec doesn't define behavior when stripping fails.
- **Also:** nilaway outputs absolute paths (e.g., `/project/myapp/internal/engine/pulse.go:42:12`) per spec example. The plan's test data uses absolute paths too. But if nilaway ever outputs a relative path, `filepath.Rel` would return it unchanged — not stripped.
- **Recommendation:** Define fallback behavior: if stripping fails or path is already relative, keep as-is or set File=""
Verdict: Highly Appropriate.

Analysis: Path mutation logic frequently blows up across platform boundaries, network mounts, or symlinks. If filepath.Rel encounters an evaluation error, or if the tool outputs a path that is already clean and relative, treating an execution warning as a total failure breaks usability.

Best-Practice Rule: Fallback gracefully. If path resolution fails or returns a formatting error, strip the known project path string prefix using standard strings.TrimPrefix(path, projectRoot), and if that still yields an absolute path, preserve it as-is rather than truncating it to an empty string.




### I — Error field format for stderr
- **Spec says:** "Tool exit code ≠ 0 → Diagnostic with error field + stderr content"
- **Ambiguity:** Is the error field just the stderr string? A concatenation? A structured format?
- **Plan does:** `fmt.Errorf("%s: %w\n%s", name, exitErr, string(exitErr.Stderr))` — wraps exit error with stderr appended as string. This matches a reasonable interpretation but isn't explicit in the spec.
- **Recommendation:** Specify the exact format: "Tool command failed: <exit code>. stderr: <content>"

Verdict: Appropriate (Needs Explicit Simplification).

Analysis: An AI agent programmatically reading your diagnostics needs a highly standardized message interface to diagnose an environment error versus a code style violation.

Best-Practice Rule: Standardize the string format clearly: Tool command failed with exit code X. Stderr: <content>.


### J — TDD claim in spec vs plan
- **Spec says:** "Testing Strategy (TDD) — Red-green-refactor cycle enforced for every feature"
- **Plan:** Each task follows "write failing test → run to verify fails → write implementation → run to verify passes" which IS red-green-refactor
- **Assessment:** This is actually consistent. The plan does follow TDD as described. My earlier note in the review doc (marked as partial) overstates the inconsistency — the spec and plan align on this point.
- **Correction:** Remove from ambiguities — this one is actually fine.
Verdict: Correct Assessment.

Analysis: Your self-correction is right on the money. The implementation plan matches standard Test-Driven Development loops perfectly. No structural remediation is required here.



### K — Spec examples may not be valid flags
- **Spec config example:** `extra_args: ["--fast"]` for golangci-lint
- **Spec config example:** `extra_args: ["--scan=package"]` for govulncheck
- **Uncertain:** Is `--fast` a valid golangci-lint flag in v2? Is `--scan=package` valid for govulncheck? Neither is widely documented as a top-level govulncheck flag (it's `./...` or package path, not `--scan=package`)
- **Impact:** If these examples are invalid, they can't be used as test data for ExtraArgs testing
- **Recommendation:** Verify or replace with real flag examples
Verdict: Completely Accurate & Critical Fix.

Analysis: You are absolutely correct about flag drift. --scan=package is not a valid CLI flag for govulncheck; it accepts directories, packages, or binary paths natively as naked positional arguments. Similarly, --fast is an old, highly debated flag in golangci-lint that can cause inconsistent cache readings. Using invalid flags in documentation or tests will break execution instantly.

Best-Practice Rule: Swap them for universally supported, deterministic flags. Use --no-config or --disable-all for golangci-lint testing, and target standard naked source directory paths (./...) for govulncheck.



### L — Data flow order: validate vs discover
- **Spec data flow diagram:** Shows arrows from handlers to subprocess — no explicit ordering shown
- **Spec data flow text:** "Validate Go project (go.mod or go.work exists)" then "Pre-flight: synchronous tool discovery → install any missing tools sequentially"
- **Spec architecture diagram:** Shows `installTools()` alongside `runLint`, `runNil` — implies discover is part of handler layer
- **Plan does:** `validateProjectPath` first, then `ensureToolsInstalled`, then parallel goroutines — matches the spec's text order
- **Assessment:** This is consistent. The plan correctly follows the spec's stated order. No ambiguity here — I originally flagged this but was wrong.
- **Correction:** Remove from ambiguities — this one is fine.

Verdict: Correct Assessment.

Analysis: The alignment between the written order in the spec and the step execution sequence in the plan is solid. No architectural revision is required.



### M — Top-level error vs Diagnostic error: decision rule unclear
- **Spec says:** "project_path doesn't exist → Top-level error response, no diagnostics"
- **Spec also says:** "Tool not installed → Diagnostic with error field"
- **Ambiguity:** The spec provides a table of scenarios but the "decision rule" for choosing top-level vs Diagnostic-level errors is never stated. For example: "tool returns non-JSON" → Diagnostic error. But "project has go.mod but it's malformed" → top-level error? Or Diagnostic?
- **Plan does:** `RunAllChecks` returns `nil, err` for validation errors; returns `all, first` (diags + first error) for tool failures. This is a reasonable heuristic: validation errors are fatal and stop execution; tool errors are collected as diagnostics.
- **Recommendation:** State the rule explicitly: "Validation errors (bad path, not a Go project, config parse failure) are fatal and return top-level errors. Tool execution errors are returned as Diagnostics with the Error field set."
Verdict: Highly Appropriate.

Analysis: The spec lacks a clear algorithmic choice matrix explaining why something is a fatal structural break versus an actionable collection error.

Best-Practice Rule: Establish the explicit decision boundary: Infrastructure and validation failures are fatal; tool analysis failures are informational. If the environment cannot parse the input argument, cannot locate the project directory, or cannot read the required configurations, return a top-level JSON-RPC error. If the platform successfully runs the tool suite, but a tool hits a local syntax error, times out, or returns a non-zero exit code, collect that failure cleanly inside a Diagnostic envelope.