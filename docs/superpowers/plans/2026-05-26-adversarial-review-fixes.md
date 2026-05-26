# Adversarial Review Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Address all 10 findings from the adversarial implementation review (2026-05-25): 3 bugs, 1 performance regression, 4 design gaps, 2 test coverage gaps.

**Architecture:** Each fix is isolated to its package; main.go changes are batched into one task to minimise context switching. All fixes follow strict red-green-refactor TDD: write the failing test, confirm it fails, implement the fix, confirm it passes, run the full suite, commit.

**Tech Stack:** Go 1.25, `github.com/mark3labs/mcp-go v0.54.0`, `internal/` packages

**Branch:** `feat/part1-setup-and-types` (all work here; do not touch `main`)

---

## File Map

| File | Tasks |
|---|---|
| `internal/checkers/golangci_lint.go` | Task 1 |
| `internal/checkers/golangci_lint_test.go` | Task 1 |
| `internal/checkers/orchestrator.go` | Tasks 2, 7 |
| `internal/checkers/orchestrator_test.go` | Tasks 2, 7 |
| `internal/config/config.go` | Task 3 |
| `internal/config/config_test.go` | Task 3 |
| `internal/discover/discover.go` | Task 4 |
| `internal/discover/discover_test.go` | Task 4 |
| `internal/checkers/govulncheck.go` | Task 6 |
| `internal/checkers/govulncheck_test.go` | Task 6 |
| `cmd/mcp-server-go-quality/main.go` | Tasks 2, 3, 4, 5 |
| `cmd/mcp-server-go-quality/main_test.go` (new) | Task 8 |

---

## Task 1: Bug 3 — Remove error prefix duplication in golangci-lint handler

**Verified:** `internal/checkers/golangci_lint.go:57-59`

```go
if exitErr != nil && len(diags) == 0 {
    return nil, fmt.Errorf("golangci-lint: %w", exitErr)  // BUG
}
```

`exitErr.Error()` returns `"Tool command failed with exit code N. Stderr: ..."`.
Wrapping with `"golangci-lint: %w"` produces a doubled prefix in the `error` field:
`"golangci-lint: Tool command failed with exit code 1. Stderr: ..."`.
The `Diagnostic.Tool` field already carries the tool name — doubling is wrong.

**Fix:** Return `exitErr` directly without the prefix wrapper.

**Files:**
- Modify: `internal/checkers/golangci_lint.go:57-59`
- Modify: `internal/checkers/golangci_lint_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/checkers/golangci_lint_test.go` (after existing tests). Note: the test file already imports `context` — add `"errors"` and `"strings"` to the import block:

```go
func TestGolangciLintHandlerExitErrorNotPrefixed(t *testing.T) {
    exitErr := &runner.ExitError{ExitCode: 1, Stderr: "no linter config found"}
    r := &mockRunner{err: exitErr}
    handler := NewGolangciLintHandler("/fake/bin")

    _, err := handler.Run(context.Background(), r, "/project/myapp")
    if err == nil {
        t.Fatal("expected error, got nil")
    }
    if strings.HasPrefix(err.Error(), "golangci-lint: ") {
        t.Errorf("error must not have tool prefix, got: %q", err.Error())
    }
    var e *runner.ExitError
    if !errors.As(err, &e) {
        t.Errorf("error should unwrap to *runner.ExitError, got %T: %v", err, err)
    }
}
```

- [ ] **Step 2: Run the test and confirm it FAILS**

```bash
cd /project/mcp-server-go-quality
go test -run TestGolangciLintHandlerExitErrorNotPrefixed ./internal/checkers/ -v
```

Expected output: `FAIL` — error has prefix `"golangci-lint: Tool command failed..."`.

- [ ] **Step 3: Implement the fix**

In `internal/checkers/golangci_lint.go`, change lines 57-59 from:

```go
if exitErr != nil && len(diags) == 0 {
	return nil, fmt.Errorf("golangci-lint: %w", exitErr)
}
```

to:

```go
if exitErr != nil && len(diags) == 0 {
	return nil, exitErr
}
```

- [ ] **Step 4: Run the test and confirm it PASSES**

```bash
go test -run TestGolangciLintHandlerExitErrorNotPrefixed ./internal/checkers/ -v
```

Expected: `PASS`

- [ ] **Step 5: Run the full checkers test suite**

```bash
go test -short ./internal/checkers/ -v
```

Expected: all existing tests still pass.

- [ ] **Step 6: Commit**

```bash
git add internal/checkers/golangci_lint.go internal/checkers/golangci_lint_test.go
git commit -m "fix: remove redundant golangci-lint prefix in exit error wrapping"
```

---

## Task 2: Bug 2 + Design Gap 8 — Fix context error masking and remove dead toolName parameter

**Verified (Bug 2):** `internal/checkers/orchestrator.go:83-91` and `cmd/mcp-server-go-quality/main.go:349-357`

Both `formatCheckerError` and `formatHandlerError` check `ctx.Err()` before examining the actual error:

```go
func formatCheckerError(ctx context.Context, toolName string, timeout time.Duration, err error) string {
    if ctx.Err() == context.DeadlineExceeded {  // BUG: checks context state, not err
        return fmt.Sprintf("timed out after %s", timeout)
    }
    ...
}
```

If a tool exits non-zero at t=4:59.9 and the deadline fires at t=5:00, by the time the handler returns `ctx.Err()` may be `DeadlineExceeded` even though `err` is a real `*runner.ExitError`. The agent receives `"timed out after 5m"` instead of the actual tool failure.

**Fix:** Use `errors.Is(err, context.DeadlineExceeded)` — this tests whether the **error itself** is a context error, not the context's state.

**Verified (Design Gap 8):** Both functions accept `toolName string` but never use it. Confirmed: no reference to `toolName` in either function body.

**Fix:** Remove the `toolName` parameter from both functions and update call sites.

**Files:**
- Modify: `internal/checkers/orchestrator.go` (function signature + call site)
- Modify: `internal/checkers/orchestrator_test.go`
- Modify: `cmd/mcp-server-go-quality/main.go` (function signature + call site)

- [ ] **Step 1: Write the failing test for context masking in orchestrator**

Add to `internal/checkers/orchestrator_test.go`:

```go
func TestFormatCheckerErrorPrefersTrueErrorOverContextState(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    cancel() // context is cancelled, but err is a real tool failure

    realErr := errors.New("Tool command failed with exit code 1. Stderr: syntax error")
    result := formatCheckerError(ctx, 5*time.Minute, realErr)
    if result != realErr.Error() {
        t.Errorf("got %q, want real error %q (context state should not shadow tool failure)", result, realErr.Error())
    }
}

func TestFormatCheckerErrorDeadlineExceededError(t *testing.T) {
    result := formatCheckerError(context.Background(), 5*time.Minute, context.DeadlineExceeded)
    if result != "timed out after 5m0s" {
        t.Errorf("got %q, want timeout message", result)
    }
}

func TestFormatCheckerErrorCancelledError(t *testing.T) {
    result := formatCheckerError(context.Background(), 5*time.Minute, context.Canceled)
    if result != "cancelled" {
        t.Errorf("got %q, want 'cancelled'", result)
    }
}
```

Note: the import block in `orchestrator_test.go` already has `"context"` and `"time"` — add `"errors"` to the import.

- [ ] **Step 2: Run the tests and confirm they FAIL**

```bash
go test -run "TestFormatCheckerError" ./internal/checkers/ -v
```

Expected: `TestFormatCheckerErrorPrefersTrueErrorOverContextState` FAILS — currently returns `"cancelled"` instead of the real error.

- [ ] **Step 3: Fix `formatCheckerError` in orchestrator.go and remove dead `toolName` parameter**

Replace the entire `formatCheckerError` function in `internal/checkers/orchestrator.go` (lines 83-91):

```go
func formatCheckerError(ctx context.Context, timeout time.Duration, err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("timed out after %s", timeout)
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return err.Error()
}
```

Add `"errors"` to the import block of `orchestrator.go` (it currently does not import it).

Update the call site in `orchestrator.go` (the line that calls `formatCheckerError` inside the goroutine, currently passing `checker.Name()` as the second argument):

```go
Error: formatCheckerError(toolCtx, timeout, err),
```

- [ ] **Step 4: Fix `formatHandlerError` in main.go and remove dead `toolName` parameter**

Replace the entire `formatHandlerError` function in `cmd/mcp-server-go-quality/main.go` (lines 349-357):

```go
func formatHandlerError(ctx context.Context, timeout time.Duration, err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("timed out after %s", timeout)
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return err.Error()
}
```

Update the call site in `main.go` (inside `makeSingleHandler`):

```go
Error: formatHandlerError(toolCtx, timeout, runErr),
```

(Remove the `toolName` argument — it is the second positional arg currently.)

Add `"errors"` to the import block of `main.go` if not already present.

- [ ] **Step 5: Run the orchestrator tests and confirm they PASS**

```bash
go test -run "TestFormatCheckerError" ./internal/checkers/ -v
```

Expected: all three new tests PASS.

- [ ] **Step 6: Run the full test suite and lint**

```bash
go test -short ./... && golangci-lint run ./...
```

Expected: all tests pass, lint clean.

- [ ] **Step 7: Commit**

```bash
git add internal/checkers/orchestrator.go internal/checkers/orchestrator_test.go \
        cmd/mcp-server-go-quality/main.go
git commit -m "fix: use errors.Is for context detection; remove unused toolName param"
```

---

## Task 3: Bug 1 — Config explicit path must error on ENOENT

**Verified:** `internal/config/config.go:70-74`

```go
func Load(path string) (Config, error) {
    cfg := Default()
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return cfg, nil  // BUG: silently returns defaults for missing file
        }
        ...
    }
```

And in `cmd/mcp-server-go-quality/main.go:53-57`:

```go
if *configPath != "" {
    cfg, err = config.Load(*configPath)  // BUG: missing file returns defaults silently
    ...
}
```

If the user passes `--config /typo/path.yaml` and the file doesn't exist, the server starts with compiled-in defaults with no error — the user never learns their config wasn't loaded.

The `os.IsNotExist` behaviour is correct for the **default** config path (no `--config` flag): absent `.go-quality.yaml` is expected. It is wrong for an **explicit** path provided by the user.

**Fix:** Add `config.LoadRequired(path string) (Config, error)` which errors when the file does not exist. Call it from `main.go` when `--config` is set.

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `cmd/mcp-server-go-quality/main.go:53-57`

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestLoadRequiredMissingFile(t *testing.T) {
    _, err := LoadRequired(filepath.Join(t.TempDir(), "nonexistent.yaml"))
    if err == nil {
        t.Error("LoadRequired must return an error when file does not exist, got nil")
    }
}

func TestLoadRequiredExistingFile(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "config.yaml")
    if err := os.WriteFile(path, []byte("timeout: 3m\n"), 0o644); err != nil {
        t.Fatal(err)
    }
    cfg, err := LoadRequired(path)
    if err != nil {
        t.Fatalf("LoadRequired returned unexpected error: %v", err)
    }
    if cfg.Timeout != 3*time.Minute {
        t.Errorf("timeout = %v, want 3m", cfg.Timeout)
    }
}
```

- [ ] **Step 2: Run the tests and confirm they FAIL**

```bash
go test -run "TestLoadRequired" ./internal/config/ -v
```

Expected: compilation error — `LoadRequired` undefined.

- [ ] **Step 3: Add `LoadRequired` to config.go**

Append to `internal/config/config.go` (after the `Load` function):

```go
// LoadRequired is like Load but returns an error if the file does not exist.
// Use when the path was explicitly specified by the caller (e.g. --config flag).
func LoadRequired(path string) (Config, error) {
	if _, err := os.Stat(path); err != nil {
		return Config{}, fmt.Errorf("config file not found: %w", err)
	}
	return Load(path)
}
```

- [ ] **Step 4: Update main.go to call LoadRequired for explicit --config**

In `cmd/mcp-server-go-quality/main.go`, change the `if *configPath != ""` block (lines 53-57) from:

```go
if *configPath != "" {
    cfg, err = config.Load(*configPath)
    if err != nil {
        log.Fatalf("config error: %v", err)
    }
} else {
```

to:

```go
if *configPath != "" {
    cfg, err = config.LoadRequired(*configPath)
    if err != nil {
        log.Fatalf("config error: %v", err)
    }
} else {
```

- [ ] **Step 5: Run the tests and confirm they PASS**

```bash
go test -run "TestLoadRequired" ./internal/config/ -v
```

Expected: both tests PASS.

- [ ] **Step 6: Run full suite and lint**

```bash
go test -short ./... && golangci-lint run ./...
```

Expected: all pass, lint clean.

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go \
        cmd/mcp-server-go-quality/main.go
git commit -m "fix: LoadRequired errors on ENOENT for explicit --config path"
```

---

## Task 4: Performance Regression — Cache resolved "latest" semver per process

**Verified:** `internal/discover/discover.go:159-164`

```go
resolved := requestedVersion
if requestedVersion == "latest" {
    v, err := ResolveLatest(ctx, modulePath)  // network call every request
    ...
    resolved = v
    // check at 167-169 prevents re-install, but NOT the network call
}
```

Every `run_code_checks` call that includes govulncheck or nilaway (default: "latest") triggers `go list -m -json <pkg>@latest`. On a slow network, this adds 200–2000ms of latency per check. The spec requires "latest" to be resolved **once per process** (unless `install_tools` is called to force a refresh).

**Fix:**
1. Add a `resolved sync.Map` field to `Cache` storing `toolName → concrete semver`.
2. Add `resolveLatestFn` injectable override for unit testing.
3. Add `LoadResolved`, `StoreResolved`, `InvalidateResolved` methods.
4. In `EnsureInstalled`, add an early fast-path: if resolved cache is warm and installed version matches, return immediately (no lock, no network).
5. In the locked slow path, check resolved cache before calling `ResolveLatest`; store to resolved cache after resolution.
6. In `makeInstallHandler`, call `versionCache.InvalidateResolved(name)` before `EnsureInstalled` so `install_tools` always forces a fresh network lookup.

**Files:**
- Modify: `internal/discover/discover.go`
- Modify: `internal/discover/discover_test.go`
- Modify: `cmd/mcp-server-go-quality/main.go` (makeInstallHandler)

- [ ] **Step 1: Write failing tests for the resolved cache methods and the caching behaviour**

Add to `internal/discover/discover_test.go`:

```go
func TestCacheLoadStoreInvalidateResolved(t *testing.T) {
    c := NewCache()

    if _, ok := c.LoadResolved("govulncheck"); ok {
        t.Error("fresh cache should have no resolved entry")
    }

    c.StoreResolved("govulncheck", "v1.3.0")
    v, ok := c.LoadResolved("govulncheck")
    if !ok || v != "v1.3.0" {
        t.Errorf("LoadResolved = (%q, %v), want (v1.3.0, true)", v, ok)
    }

    c.InvalidateResolved("govulncheck")
    if _, ok := c.LoadResolved("govulncheck"); ok {
        t.Error("after InvalidateResolved, LoadResolved should miss")
    }
}

func TestEnsureInstalledCachesResolvedVersion(t *testing.T) {
    callCount := 0
    c := NewCache()
    c.resolveLatestFn = func(_ context.Context, _ string) (string, error) {
        callCount++
        return "v1.5.0", nil
    }
    // Pre-populate installed version cache so EnsureInstalled thinks binary is at v1.5.0.
    c.Store("testtool", "v1.5.0")

    // First call: resolved cache is empty — resolveLatestFn is invoked.
    r1, err := EnsureInstalled(context.Background(), c, "/fake/bin", "testtool",
        "some/module", "some/module/cmd/testtool", "latest")
    if err != nil {
        t.Fatalf("first call error: %v", err)
    }
    if r1.Version != "v1.5.0" {
        t.Errorf("first call version = %q, want v1.5.0", r1.Version)
    }
    if callCount != 1 {
        t.Errorf("resolveLatestFn called %d times after first call, want 1", callCount)
    }

    // Second call: resolved cache is warm — resolveLatestFn must NOT be called again.
    r2, err := EnsureInstalled(context.Background(), c, "/fake/bin", "testtool",
        "some/module", "some/module/cmd/testtool", "latest")
    if err != nil {
        t.Fatalf("second call error: %v", err)
    }
    if r2.Version != "v1.5.0" {
        t.Errorf("second call version = %q, want v1.5.0", r2.Version)
    }
    if callCount != 1 {
        t.Errorf("resolveLatestFn called %d times after second call, want 1 (should be cached)", callCount)
    }
}

func TestEnsureInstalledInvalidateResolved(t *testing.T) {
    callCount := 0
    c := NewCache()
    c.resolveLatestFn = func(_ context.Context, _ string) (string, error) {
        callCount++
        return "v1.5.0", nil
    }
    c.Store("testtool", "v1.5.0")

    _, _ = EnsureInstalled(context.Background(), c, "/fake/bin", "testtool",
        "some/module", "some/module/cmd/testtool", "latest")
    if callCount != 1 {
        t.Fatalf("expected 1 resolve call after first install, got %d", callCount)
    }

    // Simulate install_tools: invalidate before calling EnsureInstalled.
    c.InvalidateResolved("testtool")
    _, _ = EnsureInstalled(context.Background(), c, "/fake/bin", "testtool",
        "some/module", "some/module/cmd/testtool", "latest")
    if callCount != 2 {
        t.Errorf("resolveLatestFn called %d times after InvalidateResolved, want 2", callCount)
    }
}
```

- [ ] **Step 2: Run the tests and confirm they FAIL**

```bash
go test -run "TestCacheLoadStoreInvalidateResolved|TestEnsureInstalledCache|TestEnsureInstalledInvalidate" \
    ./internal/discover/ -v
```

Expected: compilation errors — `LoadResolved`, `StoreResolved`, `InvalidateResolved`, `resolveLatestFn` undefined.

- [ ] **Step 3: Add resolved cache fields and methods to Cache in discover.go**

Replace the `Cache` struct and add new methods. The full updated struct and methods:

```go
type Cache struct {
	mu   sync.RWMutex
	data map[string]string // toolName → installed concrete semver

	resolved        sync.Map // toolName → resolved semver for "latest" (process lifetime)
	resolveLatestFn func(ctx context.Context, modulePath string) (string, error) // nil = use ResolveLatest
}

func NewCache() *Cache {
	return &Cache{data: make(map[string]string)}
}

func (c *Cache) Load(toolName string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[toolName]
	return v, ok
}

func (c *Cache) Store(toolName, version string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[toolName] = version
}

func (c *Cache) LoadResolved(toolName string) (string, bool) {
	if v, ok := c.resolved.Load(toolName); ok {
		return v.(string), true
	}
	return "", false
}

func (c *Cache) StoreResolved(toolName, version string) {
	c.resolved.Store(toolName, version)
}

func (c *Cache) InvalidateResolved(toolName string) {
	c.resolved.Delete(toolName)
}
```

- [ ] **Step 4: Update EnsureInstalled to use the resolved cache**

The full updated `EnsureInstalled` function in `discover.go`:

```go
func EnsureInstalled(
	ctx context.Context,
	cache *Cache,
	binDir, toolName, modulePath, installPath, requestedVersion string,
) (InstallResult, error) {
	// Fast path 1: installed version cache matches (pinned version).
	if v, ok := cache.Load(toolName); ok && (v == "unknown" || v == requestedVersion) {
		return InstallResult{Version: v, NewlyInstalled: false}, nil
	}

	// Fast path 2: "latest" already resolved this process AND installed at that version.
	if requestedVersion == "latest" {
		if resolved, ok := cache.LoadResolved(toolName); ok {
			if v, ok := cache.Load(toolName); ok && v == resolved {
				return InstallResult{Version: v, NewlyInstalled: false}, nil
			}
		}
	}

	// Fast path 3: pinned version — check binary on disk before taking the lock.
	if requestedVersion != "latest" {
		installed, err := ReadInstalledVersion(ctx, binDir, toolName, modulePath)
		if err == nil && (installed == "unknown" || installed == requestedVersion) {
			cache.Store(toolName, installed)
			return InstallResult{Version: installed, NewlyInstalled: false}, nil
		}
	}

	select {
	case <-ctx.Done():
		return InstallResult{}, ctx.Err()
	default:
	}

	InstallMu.Lock()
	defer InstallMu.Unlock()

	// Re-check after acquiring lock.
	if v, ok := cache.Load(toolName); ok && (v == "unknown" || v == requestedVersion) {
		return InstallResult{Version: v, NewlyInstalled: false}, nil
	}

	if requestedVersion == "latest" {
		if resolved, ok := cache.LoadResolved(toolName); ok {
			if v, ok := cache.Load(toolName); ok && v == resolved {
				return InstallResult{Version: v, NewlyInstalled: false}, nil
			}
		}
	}

	if requestedVersion != "latest" {
		installed, err := ReadInstalledVersion(ctx, binDir, toolName, modulePath)
		if err == nil && (installed == "unknown" || installed == requestedVersion) {
			cache.Store(toolName, installed)
			return InstallResult{Version: installed, NewlyInstalled: false}, nil
		}
	}

	select {
	case <-ctx.Done():
		return InstallResult{}, ctx.Err()
	default:
	}

	resolved := requestedVersion
	if requestedVersion == "latest" {
		if cached, ok := cache.LoadResolved(toolName); ok {
			resolved = cached
		} else {
			resolveFn := cache.resolveLatestFn
			if resolveFn == nil {
				resolveFn = ResolveLatest
			}
			v, err := resolveFn(ctx, modulePath)
			if err != nil {
				return InstallResult{}, fmt.Errorf("resolving latest for %s: %w", toolName, err)
			}
			cache.StoreResolved(toolName, v)
			resolved = v
		}

		if v2, ok := cache.Load(toolName); ok && (v2 == "unknown" || v2 == resolved) {
			return InstallResult{Version: v2, NewlyInstalled: false}, nil
		}
	}

	pkgWithVersion := fmt.Sprintf("%s@%s", installPath, resolved)
	cmd := exec.CommandContext(ctx, "go", "install", pkgWithVersion) // #nosec G204
	output, err := cmd.CombinedOutput()
	if err != nil {
		return InstallResult{}, fmt.Errorf(
			"install failed: go install %s. exit code %d. stderr: %s",
			pkgWithVersion, cmd.ProcessState.ExitCode(), string(output),
		)
	}

	binaryPath := filepath.Join(binDir, toolName)
	if _, err := os.Stat(binaryPath); err != nil {
		return InstallResult{}, fmt.Errorf("installed %s but binary not found at %s: %w", toolName, binaryPath, err)
	}

	cache.Store(toolName, resolved)
	return InstallResult{Version: resolved, NewlyInstalled: true}, nil
}
```

- [ ] **Step 5: Update makeInstallHandler in main.go to invalidate resolved cache**

In `cmd/mcp-server-go-quality/main.go`, inside `makeInstallHandler`, add `versionCache.InvalidateResolved(name)` before calling `EnsureInstalled`. Change the loop body from:

```go
for _, name := range toolNames {
    versionStr := "latest"
    if name == toolname.GolangciLint {
        versionStr = "v2.11.4"
    }
    if tc, ok := cfg.Tools[name]; ok && tc.Version != "" {
        versionStr = tc.Version
    }

    instResult, err := discover.EnsureInstalled(ctx, versionCache, binDir, name, ...
```

to:

```go
for _, name := range toolNames {
    versionStr := "latest"
    if name == toolname.GolangciLint {
        versionStr = "v2.11.4"
    }
    if tc, ok := cfg.Tools[name]; ok && tc.Version != "" {
        versionStr = tc.Version
    }

    versionCache.InvalidateResolved(name) // install_tools always forces fresh resolution
    instResult, err := discover.EnsureInstalled(ctx, versionCache, binDir, name, ...
```

(Note: Task 5 will remove the hardcoded `"v2.11.4"` block — for now, just add the `InvalidateResolved` call.)

- [ ] **Step 6: Run the new tests and confirm they PASS**

```bash
go test -run "TestCacheLoadStoreInvalidateResolved|TestEnsureInstalledCache|TestEnsureInstalledInvalidate" \
    ./internal/discover/ -v
```

Expected: all three PASS.

- [ ] **Step 7: Run full suite and lint**

```bash
go test -short ./... && golangci-lint run ./...
```

Expected: all pass, lint clean.

- [ ] **Step 8: Commit**

```bash
git add internal/discover/discover.go internal/discover/discover_test.go \
        cmd/mcp-server-go-quality/main.go
git commit -m "perf: cache resolved 'latest' semver per process to avoid repeated network calls"
```

---

## Task 5: Design Gaps 5 + 7 — Fix ensureToolsAvailable silent skip + deduplicate golangci-lint version

**Verified (Design Gap 7):** `cmd/mcp-server-go-quality/main.go:284-290`

```go
versionStr := "latest"
if name == toolname.GolangciLint {
    versionStr = "v2.11.4"  // hardcoded — duplicates config.Default()
}
if tc, ok := cfg.Tools[name]; ok && tc.Version != "" {
    versionStr = tc.Version
}
```

`config.Default()` already sets golangci-lint version to `"v2.11.4"`. Since `config.Load` always merges from `Default()`, `cfg.Tools["golangci-lint"].Version` is always `"v2.11.4"` (or user-overridden). The hardcoded block in `makeInstallHandler` is a second source of truth that will silently diverge if `Default()` is updated.

**Fix:** Remove the `if name == toolname.GolangciLint` block. Always read version from `cfg.Tools[name].Version`.

**Verified (Design Gap 5):** `cmd/mcp-server-go-quality/main.go:186-195`

```go
tc, ok := cfg.Tools[name]
if !ok {
    continue  // silent skip — tool is still run but install is skipped
}
```

If a tool name appears in `toolNames` but not in `cfg.Tools`, pre-flight install is silently skipped. The tool still runs via `buildHandlers`. If the binary is missing, the handler fails with a confusing fork/exec error instead of a clean install-failed Diagnostic.

Currently unreachable because `Default()` always populates all three tools and `Load` fills from defaults. However, it is a landmine for future changes.

**Fix:** Return an error instead of silently continuing. This makes the invariant explicit and provides a clear failure message if it ever triggers.

**Files:**
- Modify: `cmd/mcp-server-go-quality/main.go`

*(The tests for these two changes land in Task 8's main_test.go — these are pure logic changes.)*

- [ ] **Step 1: Fix Design Gap 7 — remove duplicated version in makeInstallHandler**

In `cmd/mcp-server-go-quality/main.go`, inside `makeInstallHandler`, replace:

```go
versionStr := "latest"
if name == toolname.GolangciLint {
    versionStr = "v2.11.4"
}
if tc, ok := cfg.Tools[name]; ok && tc.Version != "" {
    versionStr = tc.Version
}
```

with:

```go
versionStr := "latest"
if tc, ok := cfg.Tools[name]; ok && tc.Version != "" {
    versionStr = tc.Version
}
```

- [ ] **Step 2: Fix Design Gap 5 — ensureToolsAvailable returns error instead of continuing**

In `cmd/mcp-server-go-quality/main.go`, inside `ensureToolsAvailable`, replace:

```go
tc, ok := cfg.Tools[name]
if !ok {
    continue
}
```

with:

```go
tc, ok := cfg.Tools[name]
if !ok {
    return fmt.Errorf("tool %q not found in config (this is an internal error — report it)", name)
}
```

- [ ] **Step 3: Build to verify no compilation errors**

```bash
go build ./cmd/mcp-server-go-quality/
```

Expected: clean build.

- [ ] **Step 4: Run full suite and lint**

```bash
go test -short ./... && golangci-lint run ./...
```

Expected: all pass, lint clean.

- [ ] **Step 5: Commit**

```bash
git add cmd/mcp-server-go-quality/main.go
git commit -m "refactor: dedup golangci-lint version constant; error on missing tool config"
```

---

## Task 6: Design Gap 6 — Govulncheck parse error diagnostic missing Native field

**Verified:** `internal/checkers/govulncheck.go:194-199`

```go
if len(parseErrors) > 0 {
    diags = append(diags, diagnostic.Diagnostic{
        Tool:  toolname.Govulncheck,
        Error: fmt.Sprintf("%d line(s) failed to parse: %s", len(parseErrors), parseErrors[0]),
        // Native: nil — agents cannot inspect raw content
    })
}
```

The spec requires the `Native` field to carry the raw content so agents can inspect what govulncheck emitted. Currently it is null. Since `json.NewDecoder` is used (not line-by-line reading), the raw lines are not available, but the error strings themselves are useful debug content.

**Fix:** Marshal the `parseErrors` slice as JSON and set it as the `Native` field.

**Files:**
- Modify: `internal/checkers/govulncheck.go`
- Modify: `internal/checkers/govulncheck_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/checkers/govulncheck_test.go` (find a suitable location after existing tests):

```go
func TestParseGovulncheckOutputNativeSetOnParseError(t *testing.T) {
    input := `not valid json at all`
    diags, err := parseGovulncheckOutput([]byte(input), "/project", nil)
    if err != nil {
        t.Fatal(err)
    }
    if len(diags) == 0 {
        t.Fatal("expected at least one diagnostic for unparseable input")
    }

    // The parse error diagnostic is always appended last.
    errDiag := diags[len(diags)-1]
    if errDiag.Error == "" {
        t.Error("expected non-empty Error field in parse error diagnostic")
    }
    if errDiag.Native == nil {
        t.Error("Native field must not be nil for parse error diagnostic")
    }

    var parseErrors []string
    if err := json.Unmarshal(errDiag.Native, &parseErrors); err != nil {
        t.Errorf("Native field should be JSON array of error strings, unmarshal failed: %v", err)
    }
    if len(parseErrors) == 0 {
        t.Error("Native array of parse errors must not be empty")
    }
}
```

The test file already imports `"encoding/json"`.

- [ ] **Step 2: Run the test and confirm it FAILS**

```bash
go test -run TestParseGovulncheckOutputNativeSetOnParseError ./internal/checkers/ -v
```

Expected: FAIL — `Native` is nil.

- [ ] **Step 3: Implement the fix in govulncheck.go**

In `internal/checkers/govulncheck.go`, replace the parse error append block (lines 194-199):

```go
if len(parseErrors) > 0 {
    diags = append(diags, diagnostic.Diagnostic{
        Tool:  toolname.Govulncheck,
        Error: fmt.Sprintf("%d line(s) failed to parse: %s", len(parseErrors), parseErrors[0]),
    })
}
```

with:

```go
if len(parseErrors) > 0 {
    nativeRaw, _ := json.Marshal(parseErrors)
    diags = append(diags, diagnostic.Diagnostic{
        Tool:   toolname.Govulncheck,
        Error:  fmt.Sprintf("%d line(s) failed to parse: %s", len(parseErrors), parseErrors[0]),
        Native: nativeRaw,
    })
}
```

`json.Marshal` on a `[]string` never fails in practice; the blank identifier is safe here.

- [ ] **Step 4: Run the test and confirm it PASSES**

```bash
go test -run TestParseGovulncheckOutputNativeSetOnParseError ./internal/checkers/ -v
```

Expected: PASS.

- [ ] **Step 5: Run full suite and lint**

```bash
go test -short ./... && golangci-lint run ./...
```

Expected: all pass, lint clean.

- [ ] **Step 6: Commit**

```bash
git add internal/checkers/govulncheck.go internal/checkers/govulncheck_test.go
git commit -m "fix: populate Native field in govulncheck parse error diagnostic"
```

---

## Task 7: Test Gap 10 — Fix nil handler crash in RunAllChecks

**Verified:** `internal/checkers/orchestrator.go:33-44`

```go
go func(checker Checker) {
    defer wg.Done()
    defer func() {
        if rec := recover(); rec != nil {
            results <- runResult{
                diagnostics: []diagnostic.Diagnostic{{
                    Tool:  checker.Name(),  // BUG: panics again if checker is nil
                    ...
                }},
            }
        }
    }()
    ...
    diags, err := checker.Run(toolCtx, r, projectPath)  // panics if checker is nil
```

If a nil `Checker` interface is passed in the `handlers` slice:
1. `checker.Run(...)` panics with a nil pointer dereference.
2. The `defer recover()` catches the first panic.
3. The recovery block calls `checker.Name()` on the nil interface — **a second panic**.
4. This second panic is **not caught** by the same recover. It propagates as an unrecovered goroutine panic, crashing the entire MCP server process.

**Fix:** Capture the tool name into a local variable before any call that can panic. Use the captured name in the recovery block.

**Files:**
- Modify: `internal/checkers/orchestrator.go`
- Modify: `internal/checkers/orchestrator_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/checkers/orchestrator_test.go`:

```go
func TestRunAllNilHandlerDoesNotCrash(t *testing.T) {
    r := &mockRunner{outputs: map[string][]byte{}}

    // Passing a nil Checker interface currently crashes the whole process.
    // After the fix it must produce an error Diagnostic without panicking.
    diags := RunAllChecks(context.Background(), r, []Checker{nil}, "/project/myapp", 5*time.Second)

    if len(diags) == 0 {
        t.Error("expected at least one error diagnostic for nil handler, got none")
    }
    if len(diags) > 0 && diags[0].Error == "" {
        t.Error("expected non-empty Error field in diagnostic for nil handler")
    }
}
```

- [ ] **Step 2: Run the test and confirm it FAILS (crashes)**

```bash
go test -run TestRunAllNilHandlerDoesNotCrash ./internal/checkers/ -v
```

Expected: the test binary **panics** — `"runtime error: invalid memory address or nil pointer dereference"` — confirming the bug is real.

- [ ] **Step 3: Fix orchestrator.go — capture name before potential panic**

Replace the goroutine body inside `RunAllChecks` in `internal/checkers/orchestrator.go`.

The current goroutine:
```go
go func(checker Checker) {
    defer wg.Done()
    defer func() {
        if rec := recover(); rec != nil {
            results <- runResult{
                diagnostics: []diagnostic.Diagnostic{{
                    Tool:  checker.Name(),
                    Error: fmt.Sprintf("internal panic: %v", rec),
                }},
            }
        }
    }()

    toolCtx, toolCancel := context.WithTimeout(ctx, timeout)
    defer toolCancel()

    diags, err := checker.Run(toolCtx, r, projectPath)
    if err != nil {
        results <- runResult{
            diagnostics: []diagnostic.Diagnostic{{
                Tool:  checker.Name(),
                Error: formatCheckerError(toolCtx, timeout, err),
            }},
        }
        return
    }
    results <- runResult{diagnostics: diags}
}(h)
```

Replace with:

```go
go func(checker Checker) {
    defer wg.Done()

    // Capture name before any panicking call; recovery block uses this variable.
    toolName := ""
    defer func() {
        if rec := recover(); rec != nil {
            results <- runResult{
                diagnostics: []diagnostic.Diagnostic{{
                    Tool:  toolName,
                    Error: fmt.Sprintf("internal panic: %v", rec),
                }},
            }
        }
    }()

    toolName = checker.Name() // panics here if checker is nil — recovery uses ""
    toolCtx, toolCancel := context.WithTimeout(ctx, timeout)
    defer toolCancel()

    diags, err := checker.Run(toolCtx, r, projectPath)
    if err != nil {
        results <- runResult{
            diagnostics: []diagnostic.Diagnostic{{
                Tool:  toolName,
                Error: formatCheckerError(toolCtx, timeout, err),
            }},
        }
        return
    }
    results <- runResult{diagnostics: diags}
}(h)
```

The key change: `toolName` is a local variable initialised to `""`. It is set by `checker.Name()` before `checker.Run()`. If either call panics, the recovery block uses the captured `toolName` (which may be `""` if `Name()` itself panicked). Because the recovery block no longer calls any method on `checker`, there is no second panic.

- [ ] **Step 4: Run the test and confirm it PASSES**

```bash
go test -run TestRunAllNilHandlerDoesNotCrash ./internal/checkers/ -v
```

Expected: PASS — no crash, one error Diagnostic returned.

- [ ] **Step 5: Run the full orchestrator test suite**

```bash
go test -short ./internal/checkers/ -v
```

Expected: all tests pass including the existing `TestRunAllPanicRecovery`.

- [ ] **Step 6: Run full suite and lint**

```bash
go test -short ./... && golangci-lint run ./...
```

Expected: all pass, lint clean.

- [ ] **Step 7: Commit**

```bash
git add internal/checkers/orchestrator.go internal/checkers/orchestrator_test.go
git commit -m "fix: prevent second panic in RunAllChecks recovery block for nil handler"
```

---

## Task 8: Test Gap 9 — Add comprehensive tests for main.go functions

**Verified:** No `*_test.go` file exists in `cmd/mcp-server-go-quality/`. The following functions have zero test coverage:
- `resolveProjectPath` — path defaulting logic
- `resolveRequestedTools` — nil vs empty `tools` array, invalid tool names
- `buildHandlers` — handler construction per tool name
- `ensureToolsAvailable` — error on tool not in config (after Task 5 fix)
- `marshalDiagnostics` — nil slice must marshal as `[]`, not `null`
- `marshalInstallResult` — correctness of marshalled JSON structure
- `formatHandlerError` — correct classification of real errors vs context errors (after Task 2 fix)

**Files:**
- Create: `cmd/mcp-server-go-quality/main_test.go`

- [ ] **Step 1: Create the test file**

Create `cmd/mcp-server-go-quality/main_test.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/afshinator/mcp-server-go-quality/internal/config"
	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/discover"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
	"github.com/mark3labs/mcp-go/mcp"
)

// makeRequest constructs a CallToolRequest with the given arguments.
func makeRequest(args map[string]any) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

// extractText pulls the first TextContent.Text from a CallToolResult.
func extractText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatal("no TextContent found in CallToolResult")
	return ""
}

// --- resolveProjectPath ---

func TestResolveProjectPathDefault(t *testing.T) {
	req := makeRequest(nil)
	path, err := resolveProjectPath(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path when no project_path arg")
	}
}

func TestResolveProjectPathExplicit(t *testing.T) {
	req := makeRequest(map[string]any{"project_path": "/some/path"})
	path, err := resolveProjectPath(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/some/path" {
		t.Errorf("path = %q, want /some/path", path)
	}
}

// --- resolveRequestedTools ---

func TestResolveRequestedToolsNilParam(t *testing.T) {
	req := makeRequest(nil) // no "tools" key
	tools, err := resolveRequestedTools(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 3 {
		t.Errorf("got %d tools, want 3 (all)", len(tools))
	}
}

func TestResolveRequestedToolsEmptySlice(t *testing.T) {
	req := makeRequest(map[string]any{"tools": []any{}})
	tools, err := resolveRequestedTools(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 3 {
		t.Errorf("got %d tools, want 3 (all)", len(tools))
	}
}

func TestResolveRequestedToolsSubset(t *testing.T) {
	req := makeRequest(map[string]any{"tools": []any{"golangci-lint", "govulncheck"}})
	tools, err := resolveRequestedTools(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 2 {
		t.Errorf("got %d tools, want 2", len(tools))
	}
}

func TestResolveRequestedToolsInvalidName(t *testing.T) {
	req := makeRequest(map[string]any{"tools": []any{"unknown-tool"}})
	_, err := resolveRequestedTools(req)
	if err == nil {
		t.Error("expected error for unknown tool name, got nil")
	}
}

// --- buildHandlers ---

func TestBuildHandlersAllThree(t *testing.T) {
	cfg := config.Default()
	handlers := buildHandlers(toolname.All(), cfg, "/fake/project", "/fake/bin")
	if len(handlers) != 3 {
		t.Errorf("got %d handlers, want 3", len(handlers))
	}
	names := make(map[string]bool)
	for _, h := range handlers {
		names[h.Name()] = true
	}
	for _, name := range toolname.All() {
		if !names[name] {
			t.Errorf("handler for %q not built", name)
		}
	}
}

func TestBuildHandlersSingle(t *testing.T) {
	cfg := config.Default()
	handlers := buildHandlers([]string{toolname.GolangciLint}, cfg, "/fake/project", "/fake/bin")
	if len(handlers) != 1 {
		t.Errorf("got %d handlers, want 1", len(handlers))
	}
	if handlers[0].Name() != toolname.GolangciLint {
		t.Errorf("handler name = %q, want %s", handlers[0].Name(), toolname.GolangciLint)
	}
}

// --- ensureToolsAvailable ---

func TestEnsureToolsAvailableUnknownToolReturnsError(t *testing.T) {
	cfg := config.Default()
	// Remove golangci-lint from config to trigger the error path.
	delete(cfg.Tools, toolname.GolangciLint)

	ctx := context.Background()
	err := ensureToolsAvailable(ctx, []string{toolname.GolangciLint}, cfg, "/fake/bin", discover.NewCache())
	if err == nil {
		t.Error("expected error when tool not in config, got nil")
	}
}

// --- marshalDiagnostics ---

func TestMarshalDiagnosticsNilIsEmptyArray(t *testing.T) {
	result, err := marshalDiagnostics(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	// The JSON text content must be "[]", not "null".
	text := extractText(t, result)
	var out []diagnostic.Diagnostic
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if out == nil {
		t.Error("unmarshalled nil — expected empty array []")
	}
	if len(out) != 0 {
		t.Errorf("got %d diagnostics, want 0", len(out))
	}
}

func TestMarshalDiagnosticsWithFindings(t *testing.T) {
	diags := []diagnostic.Diagnostic{
		{Tool: "golangci-lint", File: "main.go", Line: 10, Message: "unused var"},
	}
	result, err := marshalDiagnostics(diags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := extractText(t, result)
	var out []diagnostic.Diagnostic
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(out) != 1 {
		t.Errorf("got %d diagnostics, want 1", len(out))
	}
}

// --- marshalInstallResult ---

func TestMarshalInstallResultEmpty(t *testing.T) {
	result, err := marshalInstallResult(InstallResult{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	text := extractText(t, result)
	var out InstallResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
}

// --- formatHandlerError (after Task 2 fix: uses errors.Is) ---

func TestFormatHandlerErrorRealErrorWhenCtxDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // context is cancelled, but err is a real tool failure

	realErr := errors.New("Tool command failed with exit code 1. Stderr: syntax error")
	got := formatHandlerError(ctx, 5*time.Minute, realErr)
	if got != realErr.Error() {
		t.Errorf("got %q, want real error string %q (context state must not shadow tool failure)", got, realErr.Error())
	}
}

func TestFormatHandlerErrorDeadlineExceeded(t *testing.T) {
	got := formatHandlerError(context.Background(), 5*time.Minute, context.DeadlineExceeded)
	if got != "timed out after 5m0s" {
		t.Errorf("got %q, want timeout message", got)
	}
}

func TestFormatHandlerErrorCancelled(t *testing.T) {
	got := formatHandlerError(context.Background(), 5*time.Minute, context.Canceled)
	if got != "cancelled" {
		t.Errorf("got %q, want 'cancelled'", got)
	}
}
```

- [ ] **Step 2: Run the tests and confirm they FAIL**

```bash
go test -run "." ./cmd/mcp-server-go-quality/ -v
```

Expected: multiple failures — `marshalDiagnostics` returns nil-vs-empty differences before Task 3 guard is in place, and `ensureToolsAvailable` previously returned `nil` for unknown tools (now errors after Task 5 fix). Some tests will compile-fail if signatures don't match yet — fix imports and signatures iteratively.

- [ ] **Step 3: Iteratively fix compilation and run again**

After any compilation errors: adjust import paths, type assertions, or method calls to match the actual signatures of the functions under test. Do not change the functions themselves — fix the test to call them correctly.

- [ ] **Step 4: Run tests and confirm all pass**

```bash
go test -run "." ./cmd/mcp-server-go-quality/ -v
```

Expected: all tests in `main_test.go` PASS.

- [ ] **Step 5: Run full suite and lint**

```bash
go test -short ./... && golangci-lint run ./...
```

Expected: all pass, lint clean. If golangci-lint flags any issues in the new test file (e.g., function length, error checking), address them.

- [ ] **Step 6: Commit**

```bash
git add cmd/mcp-server-go-quality/main_test.go
git commit -m "test: add comprehensive test coverage for main.go handler wiring functions"
```

---

## Final Verification

After all 8 tasks are complete:

- [ ] **Run the full test suite (including integration)**

```bash
go test -timeout 10m ./...
```

Expected: all unit tests pass; integration tests pass (or skip gracefully if nilaway binary not installed).

- [ ] **Run golangci-lint**

```bash
golangci-lint run ./...
```

Expected: 0 issues.

- [ ] **Run gofumpt and goimports**

```bash
gofumpt -w ./... && goimports -w ./...
```

- [ ] **Run govulncheck**

```bash
govulncheck ./...
```

Expected: only stdlib CVEs (Go 1.25.9 known issues — not actionable).

- [ ] **Confirm all 10 adversarial findings are addressed**

| # | Finding | Task | Status |
|---|---|---|---|
| 1 | `--config` ENOENT silent | Task 3 | ✓ |
| 2 | Context error masking | Task 2 | ✓ |
| 3 | Error prefix duplication | Task 1 | ✓ |
| 4 | ResolveLatest every request | Task 4 | ✓ |
| 5 | ensureToolsAvailable silent skip | Task 5 | ✓ |
| 6 | Govulncheck parse error Native null | Task 6 | ✓ |
| 7 | Hardcoded golangci-lint version | Task 5 | ✓ |
| 8 | Dead toolName parameter | Task 2 | ✓ |
| 9 | main.go zero test coverage | Task 8 | ✓ |
| 10 | Nil handler crash in RunAllChecks | Task 7 | ✓ |
