# Adversarial Review: Implementation (feat/part1-setup-and-types)

**Reviewed:** 2026-05-25  
**Branch:** origin/feat/part1-setup-and-types (c4f2fcb)  
**Status:** All unit tests pass (32). All integration tests pass (3 of 4; nilaway missing binary). golangci-lint: 0 issues. govulncheck: stdlib CVEs only.  

---

## Summary

93 tests, clean lint, working MCP server. The implementation handles several spec gaps
correctly where the spec was wrong (govulncheck uses pretty-printed JSON not NDJSON;
golangci-lint v2 flag format; nilaway deterministic sorting). Three notable bugs, one
performance regression vs spec, four design gaps, two test coverage gaps.

---

## BUGS

### 1. `--config` nonexistent file returns defaults silently

```go
// main.go:62-64
cfg, err = config.Load(*configPath)
if err != nil {
    log.Fatalf("config error: %v", err)
}
```

`config.Load` returns `(defaults, nil)` when the file doesn't exist (`os.ErrNotExist` is
returned as `(cfg, nil)`). If the user passes `--config /typo/path.yaml`, the server
starts with compiled-in defaults and no error. The user never learns their config file
wasn't found.

**Fix:** `config.Load` should return an error when the path was explicitly provided (via
`--config`) and the file doesn't exist. When no `--config` is set, the missing-is-defaults
behavior is correct. Distinguish via a `bool` parameter or separate function.

### 2. Context error masking in error formatters

Both `formatHandlerError` (main.go) and `formatCheckerError` (orchestrator.go) check
`ctx.Err()` before examining the actual error:

```go
// main.go:362-368
func formatHandlerError(ctx context.Context, toolName string, timeout time.Duration, err error) string {
    if ctx.Err() == context.DeadlineExceeded {
        return fmt.Sprintf("timed out after %s", timeout)
    }
    if ctx.Err() == context.Canceled {
        return "cancelled"
    }
    return err.Error()
}
```

If a tool fails with a non-zero exit code AND the context deadline expires at ~roughly
the same time, the error is reported as "timed out" instead of the actual tool failure.
The agent loses the real error (stderr, exit code).

`ctx.Err()` returns the context's cancellation cause, not the error's cause. A tool can
exit non-zero at t=4:59.9 while the deadline is t=5:00. Both are true when the handler
returns, but `ctx.Err()` is checked first.

**Fix:** Check `errors.Is(err, context.DeadlineExceeded)` or `errors.Is(err, context.Canceled)`
instead of `ctx.Err()`. This tests whether the *error itself* is a context error, not
whether the context happened to be done when we looked.

Same pattern in `orchestrator.go:82-92` (`formatCheckerError`).

### 3. Error prefix duplication in golangci-lint handler

```go
// golangci_lint.go:60
return nil, fmt.Errorf("golangci-lint: %w", exitErr)
```

`exitErr` is an `*ExitError` whose `Error()` returns `"Tool command failed with exit code N. Stderr: ..."`.
Wrapping it with `"golangci-lint: %w"` produces `"golangci-lint: Tool command failed with exit code N. Stderr: ..."`.
This then appears in the `error` field as `"golangci-lint: Tool command failed..."` —
the tool name is doubled, and the format doesn't match the spec's standardized
`"Tool command failed with exit code <N>. Stderr: <content>"` because the prefix
"golangci-lint: " is prepended.

**Fix:** Return the unwrapped error or format it without the tool prefix, since the
`Diagnostic.Tool` field already carries the tool name. Use `exitErr.Error()` directly
or return the `ExitError` without wrapping.

---

## PERFORMANCE REGRESSION VS SPEC

### 4. Latest version resolved on every request (not once per process)

The spec (Latest Resolution Policy) states: "resolves `latest` to a concrete version
**once per process, unless `install_tools` is called**."

The implementation calls `ResolveLatest` on every request for "latest" tools:

```go
// discover.go, in EnsureInstalled
if requestedVersion == "latest" {
    v, err := ResolveLatest(ctx, modulePath)
    ...
}
```

The double-check at line 175 (`if v2, ok := cache.Load(toolName); ok && ... v2 == resolved`)
prevents *re-installs* when the version hasn't changed, but it does NOT prevent the
network call. Every `run_code_checks` that includes govulncheck or nilaway (default: "latest")
triggers `go list -m -json <pkg>@latest`.

On a slow or unavailable network, this adds latency to every check call. The spec design
intentionally cached the resolved semver in memory to avoid this.

**Fix:** Add a separate `resolvedCache` that maps `toolName → resolvedSemver` with
process-lifetime semantics. Check this cache before calling `ResolveLatest`. Only
`install_tools` should bypass it. The version cache (`toolName → installedVersion`)
already exists; the `ResolveLatest` result just needs to be stored and reused.

---

## DESIGN GAPS

### 5. `ensureToolsAvailable` silently skips tools not in config

```go
// main.go:192-195
tc, ok := cfg.Tools[name]
if !ok {
    continue
}
```

If a tool name is somehow in `toolNames` but absent from `cfg.Tools`, pre-flight install
skips it — but the tool is still included in `buildHandlers` and will be run. If the
binary is missing, the handler fails with a confusing fork/exec error instead of the
standardized install-failed Diagnostic.

Currently can't trigger because the default config always has all three tools and partial
YAML merges into defaults. But it's a landmine for future config changes.

**Fix:** Log a warning and still attempt install with defaults, or return an error.

### 6. Govulncheck parse error diagnostic missing `Native` field

The spec says ("NDJSON parse error handling"):
```go
Diagnostic{
    Error:  fmt.Sprintf("%d line(s) failed to parse: %s", n, firstErr),
    Native: json.RawMessage(mustMarshal(rawLinesAsString)),
}
```

The implementation sets `Error` but leaves `Native` zero-valued (null):
```go
// govulncheck.go:163-166
diags = append(diags, diagnostic.Diagnostic{
    Tool:  toolname.Govulncheck,
    Error: fmt.Sprintf("%d line(s) failed to parse: %s", len(parseErrors), parseErrors[0]),
})
```

The agent receives `native: null` and can't inspect what govulncheck emitted. While
`json.NewDecoder` makes line-level capture harder (it's not line-oriented), the error
diagnostic still loses debuggability.

**Mitigation:** The server log (stderr) would contain the raw output if `--verbose` is
enabled, but agents don't see stderr. Low severity — govulncheck v1.3.0 output is stable.

### 7. Hardcoded golangci-lint version in `makeInstallHandler`

```go
// main.go:279
versionStr := "latest"
if name == toolname.GolangciLint {
    versionStr = "v2.11.4"
}
```

The default golangci-lint version is hardcoded in two places: `config.Default()` and
`makeInstallHandler`. If the config default ever changes, the handler must also be
updated. The handler should read the version from a single source (config defaults or
constants).

### 8. `formatHandlerError` accepts `toolName` parameter but doesn't use it

```go
func formatHandlerError(ctx context.Context, toolName string, timeout time.Duration, err error) string {
```

The `toolName` parameter is unused. The function signature was likely copied from
`formatCheckerError` (orchestrator.go) where `toolName` is passed but also unused.
Dead parameter.

---

## TEST COVERAGE GAPS

### 9. No tests for `main.go` handler wiring

`cmd/mcp-server-go-quality/main.go` has zero test files. Functions with no coverage:
- `resolveProjectPath` — path defaulting logic
- `resolveRequestedTools` — empty vs nil `tools` array handling
- `buildHandlers` — handler construction with workspace modules and nilaway includePkgs
- `ensureToolsAvailable` — pre-flight install with config version lookup
- `makeRunAllHandler`, `makeSingleHandler`, `makeInstallHandler` — full handler composition
- `marshalDiagnostics`, `marshalInstallResult` — JSON marshalling for empty slices (which guards against `null` vs `[]`)
- `formatHandlerError` — context error detection

These are the public API of the MCP server and the most likely to break on refactoring.
The handler wiring in particular has subtle logic (empty `tools` → all three, config key
lookups, project root discovery call ordering).

### 10. No test for `RunAllChecks` with a nil handler that panics without recovery

The panic recovery test (`TestRunAllPanicRecovery`) covers the case where a checker
panics. But there's no test for what happens if `handlers` is non-nil but contains a nil
element (`[]Checker{nil, NewGolangciLintHandler(...)}`). This would cause a nil pointer
dereference in the goroutine since `checker.Name()` is called before `Run()`.

The `recover()` in the goroutine would catch this panic, but the test doesn't verify it.

---

## WHAT WAS WELL-IMPLEMENTED

- **govulncheck retry loop** uses `select` with `ctx.Done()`, not `time.Sleep`. Fixes
  spec review item #12.
- **govulncheck parsing** uses `json.NewDecoder` for multi-value pretty-printed JSON,
  handling govulncheck v1.3.0's actual output format (not the NDJSON the spec assumed).
- **golangci-lint v2 flags** correctly use `--output.json.path stdout --output.text.path stderr`
  instead of the spec's outdated `--out-format=json`.
- **nilaway deterministic output** sorts package keys before iteration.
- **Double-check install lock pattern** fully implemented with both cache re-read and
  binary re-verification after acquiring `InstallMu`.
- **Two-pass root discovery** correctly handles `go.work` precedence over closer `go.mod`.
- **Path traversal is safe by design** — root discovery rejects any path without
  `go.mod`/`go.work`, including `../../../../etc`.
- **Panic recovery in orchestrator** sends an error Diagnostic for the failed tool
  without losing results from sibling goroutines.
- **`pathutil.Rel`** catches escape paths (`../../../`) and falls back to absolute.
- **All unit tests pass, golangci-lint 0 issues**, govulncheck only finds stdlib CVEs.

---

## SUMMARY BY SEVERITY

| Severity | Count | Items |
|---|---|---|
| Bug | 3 | `--config` nonexistent file (1), context error masking (2), error prefix duplication (3) |
| Performance regression | 1 | Latest resolution every request (4) |
| Design gap | 4 | Config tool skip (5), govulncheck parse error native (6), hardcoded version (7), dead parameter (8) |
| Test coverage gap | 2 | main.go untested (9), nil handler panic (10) |
| Well-implemented | 7 | Retry, parsing, flags, sorting, lock, root discovery, path safety, panic recovery |
