# Adversarial Review: spec-v3.md

---

## CONTRADICTIONS

**1. `install_tools` "force-reinstalls" vs. the output schema showing `already_present`**
The Discovery section says `install_tools` "bypasses the cache and force-reinstalls the requested subset." The Output Format section shows an `already_present` array — implying `install_tools` checks the binary first and skips if correct. These cannot both be true. "Force-reinstall" means unconditional; `already_present` means conditional. Pick one and define it precisely.

**2. Pre-flight discovery: "first incoming request" vs. "before check goroutines run"**
Auto-Install Rule #1 says discovery happens "on the first incoming request from any check tool." The Pre-Flight Sequential Install section says "Before the check goroutines run, a synchronous pass discovers and installs only the tools that are about to run." The first implies one-time lazy init; the second implies per-request. With a version cache this is fast after init, but the spec never defines the trigger precisely: is it a `sync.Once`? A mutex-guarded flag? Concurrent second requests before the first finishes can see an uninitialized cache either way.

**3. Install-failure error format**
Auto-Install Rule #4 says failure Diagnostics contain "the exact `go install` command that failed and its stderr output." The Standardised Error String Format section defines the format as `"Tool command failed with exit code <N>. Stderr: <content>"`. These are different formats. Which governs install failures?

**4. `latest` resolution: "once per process lifetime" vs. cache-only semantics**
The `latest` Resolution Policy says the server resolves `latest` to a concrete version "once per process lifetime: on the first request that needs this tool's version (if it isn't already cached)." But the same section also says `install_tools` can "force a fresh resolution." If `install_tools` force-reinstalls and updates the cache, subsequent check calls see the new cached version — this is consistent. But the "once per process lifetime" phrasing is wrong. Call it "once per process lifetime unless `install_tools` is called."

---

## HOLES

**5. `project_path` defaults to server CWD — but CWD is unspecified**
"Defaults to server's CWD" — MCP servers are launched by the MCP client (e.g., Claude Code). The server's CWD depends entirely on how the client starts the process, which is unspecified. An agent relying on the default could silently analyze the wrong project. The spec should either require `project_path` explicitly or define how agents discover what the server's CWD is (e.g., an `info` tool, or documenting that Claude Code sets CWD to the workspace root).

**6. `tools: []` (empty array) runs all three tools — counterintuitive**
"Omitted or empty = all three." Most array-based APIs treat an explicit empty list as "none." An agent that passes `tools: []` to disable all tools will get a full three-tool run. This should be prominently documented as a deliberate design choice with a warning, or reconsidered.

**7. `GOPATH/bin` not guaranteed on PATH after `go install`**
`go install` places binaries in `$GOPATH/bin` (or `$GOBIN`). `exec.LookPath` scans `$PATH`. If `$GOPATH/bin` is not in `$PATH` (common in Docker images, CI containers, sandboxed environments), the server installs the tool successfully but then immediately fails to find it on the next `exec.LookPath` call — causing an infinite reinstall loop. The spec must define what to do: either use the known `$GOPATH/bin/<tool>` path directly after install, or check `$GOPATH/bin` explicitly.

**8. `extra_args` ordering relative to required flags**
"Server's required flags (like `--out-format=json`) are always applied and cannot be overridden via `extra_args`." If `extra_args` contains `--out-format=text`, the parser breaks. The spec doesn't say whether required flags are prepended or appended to the command, nor does it say which position "wins" for each tool's flag parser. For golangci-lint using pflag, last flag typically wins. For govulncheck, the behavior may differ. Define the ordering and enforcement mechanism.

**9. No timeout covering the pre-flight install phase**
The `timeout` field governs subprocess execution. A cold `go install` of three tools on a slow network can take 5–10 minutes. This happens before the per-tool timeout starts. An agent will see its request hang silently during install with no deadline. Either: extend the timeout to cover install, or add a separate install timeout, or document explicitly that the timeout does not cover pre-flight.

**10. Double-check pattern for concurrent installs unspecified**
The cache is protected by `sync.RWMutex`. Two concurrent requests both read-miss → both try to acquire write lock → first installs → second acquires lock and installs redundantly (or races on the module cache). The spec must describe the double-check pattern: re-validate the cached version after acquiring the write lock before installing.

**11. No panic recovery in check goroutines**
A goroutine that panics will have its deferred `wg.Done()` fire, but the `results <- runResult{...}` send will not execute (panic unwinds past it). `wg.Wait()` returns, `close(results)` fires, but the results channel has N-1 items instead of N. The draining loop returns fewer diagnostics than expected with no indication of the missing tool. Goroutines should use `recover()` and send an error Diagnostic on panic.

**12. Retry loop uses `time.Sleep`, ignores context**
The govulncheck retry backoff:
```go
time.Sleep(vulnRetryBackoff)
```
`time.Sleep` doesn't respect context cancellation. If `toolCtx` is cancelled during the 2-second sleep (client disconnected, timeout expired), the sleep runs to completion before cancellation is detected on the next `runGovulncheck` call. The 2-second sleep can exceed the remaining tool deadline. Use a context-aware select instead:
```go
select {
case <-time.After(vulnRetryBackoff):
case <-toolCtx.Done():
    return // propagate cancellation
}
```

**13. `go list -m -json all` vs. `use` directives for nilaway — not equivalent**
Step 2 of the multi-module workspace resolution says: "Parse the `use` directives (or run `go list -m -json all`) to collect all workspace module paths." These are **not** equivalent. `go list -m -json all` returns all transitive dependencies — potentially hundreds of modules. Passing all of them to `-include-pkgs` would instruct nilaway to analyze all dependencies (defeating the purpose). The correct approach is parsing only the `use` directives from `go.work`, then reading each referenced `go.mod` for the `module` declaration.

**14. govulncheck trace direction ambiguous**
"from the last `finding.trace` entry that has a `position` (the call site in user code)" — govulncheck's trace is ordered caller → callee. `trace[0]` is the user's call site; the last entry with a position is in the vulnerable package. The spec says "call site in user code" but then notes it "may point to a shared library module." These are opposite ends of the trace. The spec should specify `trace[0]` (first entry, user's calling code) or define a traversal that finds the first entry whose module matches a workspace module. The current heuristic navigates to the wrong end.

**15. Root discovery algorithm is ambiguous**
"Walks up looking for `go.work`, then `go.mod`." Two interpretations:
- At each level, check `go.work` first; if found, stop; else check `go.mod`; if found, stop. → First `go.mod` found wins if it's closer than any `go.work`. This is wrong for workspace setups.
- Walk up looking only for `go.work`; if not found anywhere, restart and walk up looking for `go.mod`. → Correct but two-pass.

Define the exact algorithm. The first interpretation breaks monorepos where `go.work` is in a grandparent directory and `go.mod` is in the immediate parent.

**16. No concurrency limit**
The MCP protocol allows multiple concurrent tool calls. Five simultaneous `run_code_checks` requests launch 15 subprocesses (5 × 3 tools). On a constrained machine (or container with ulimits), this can exhaust file descriptors, memory, or CPU. The spec should either document expected concurrency or add a request semaphore.

---

## AMBIGUITIES

**17. `native: null` for error diagnostics — not stated**
When `Error` is non-empty and `Native` is "zero-valued," a zero `json.RawMessage` marshals to JSON `null`. The spec never says so. Agents testing `if diagnostic.native` will see `null`, not an absent field. Add: "zero-valued `Native` marshals as `null` in the JSON output."

**18. `severity` field absent vs. empty string**
`Severity string json:"severity,omitempty"` — for govulncheck and nilaway diagnostics, `Severity` is `""`, so the field is **absent** from the JSON output (omitempty). The spec's Severity mapping says these tools return `""`. An agent checking `diagnostic.severity === ""` gets different results than checking `!("severity" in diagnostic)`. Document that `severity` is absent (not empty string) for these tools.

**19. "Merging" `finding` and `osv` for `Native`**
"`Native`: the raw `finding` object merged with its associated `osv` object." `json.RawMessage` values can't be merged without deserializing. Define the merge strategy: wrap both in a container `{"finding": ..., "osv": ...}`? Deep merge the two JSON objects? What happens on key collisions?

**20. nilaway "first sentence" rule fails for abbreviations**
The boundary rule: `. ` followed by `[A-Z]`. The string `"See e.g. Something wrong here"` splits at `"e.g. S"` — yielding `"See e.g."` as the first "sentence." This is a false split. The spec should either accept this as a known limitation, use a different heuristic, or define a list of common abbreviations to exclude.

**21. nilaway `-pretty-print=false` flag**
Nilaway is pre-1.0 software. The flag `-json -pretty-print=false` is stated as "verified working on installed version" — which version? If the version cache stores `v0.0.0-20260515...`, the flag API at that pseudo-version must match. Document which nilaway release introduced stable `-json` support, or add a comment that the parser must be re-validated on `install_tools` version bumps.

**22. What nilaway emits for zero findings**
If nilaway finds no issues, does it emit `{}` or nothing? The parser maps over the top-level JSON object — for an empty map it returns `[]Diagnostic{}`. Confirm this and document it. An empty-string stdout (no output at all) vs. `{}` are different failure modes.

---

## BEST PRACTICE GAPS

**23. Loop variable capture comment is stale for Go 1.22+**
The Data Flow and Timeout Model sections explicitly document the `go func(t Tool) {...}(tool)` pattern as avoiding "closure capture bug." In Go 1.22+, each loop iteration creates its own variable — this workaround is harmless but the explanatory comment will mislead readers into thinking it's still necessary in Go 1.25 (the target version). Remove the comment or note it as a guard for < 1.22 compatibility.

**24. `time.Sleep` in spec code vs. context-aware idiom**
Beyond the govulncheck retry, using `time.Sleep` in concurrent server code is an anti-pattern — any blocking sleep delays context propagation. Any other future retry or backoff in the implementation should default to context-aware waits. The spec setting this pattern in example code will be copy-pasted into the implementation.

**25. No MCP protocol version declared**
The spec doesn't state which MCP specification version this server implements (e.g., `2024-11-05`, `2025-03-26`). MCP has breaking changes between versions. Different MCP clients may negotiate different protocol versions. Omitting this creates compatibility ambiguity at integration time.

**26. `column: 0` ambiguity for 0-indexed tools**
`Column int json:"column,omitempty"` — `0` means "unknown or not applicable" and is omitted via `omitempty`. If any future tool uses 0-based column indexing, column 0 is valid data that will be silently omitted. The spec commits to 1-indexed columns without stating this assumption.

**27. Security: no sanitization note for `project_path`**
An agent (or compromised prompt) could pass `project_path: "../../../../etc"`. The server finds no `go.mod`/`go.work` and returns a fatal error — so no harm. But the spec never states this is safe by design. Add a sentence confirming path traversal is mitigated by the Go project validation requirement.

**28. `install_tools` on "already correct" version — wastes time unnecessarily**
Even if all tools are at the correct version, calling `install_tools` re-downloads and re-installs them (per "force-reinstalls"). On a slow network this is 2–5 minutes of unnecessary work. The recommended workflow says "Call `install_tools` once at session start" — if the agent follows this and all tools are fresh, it pays the full install cost for nothing. This should note: use `install_tools` selectively, or clarify that the binary-presence check is performed first.

---

## MINOR INCONSISTENCIES

**29. "Up to 3 goroutines" hardcoded in Architecture**
The Architecture section says "up to 3 goroutines." The actual max is `len(requestedTools)`, which is 1–3 depending on the `tools` parameter. Not wrong, but imprecise.

**30. `run_code_checks` description says "all 3" then "configurable subset"**
The MCP Tools table: "Run all 3 checkers in parallel (configurable subset), return unified results." "All 3" and "configurable subset" are contradictory in the same sentence.

**31. govulncheck `~40 MB` database size**
This is an approximation from when the spec was written. The Go vulnerability database grows over time. Flag this as approximate to avoid agents building hard-coded assumptions around it.

**32. References `spec-v2-review.md` and `spec-v2-review2.md` — actual files are `spec-v2-review1.md` through `spec-v2-review6.md`**
The References section cites filenames that don't match what's on disk.

---

## Summary by severity

| Category | Count |
|---|---|
| Contradictions | 4 |
| Holes (missing spec) | 12 |
| Ambiguities | 6 |
| Best practice gaps | 6 |
| Minor inconsistencies | 4 |

The highest-risk items are **#7** (GOPATH/bin PATH gap — silent infinite install loop), **#14** (govulncheck trace direction — agents navigate to the wrong file), **#13** (nilaway `go list all` vs `use` directives — nilaway scans all dependencies), **#12** (retry sleep ignores context — can miss deadlines), and **#10** (concurrent double-install race — no double-check pattern).
