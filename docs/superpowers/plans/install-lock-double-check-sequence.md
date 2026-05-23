# Tool Install Concurrency Model (Authoritative Sequence)

## Goals

The install system must guarantee:

1. No concurrent `go install` executions for the same tool/version
2. No duplicate latest-version resolution races
3. No corrupted cache state
4. Minimal lock contention for hot-path reads
5. Deterministic behavior under concurrent MCP requests
6. Correctness even when many requests start simultaneously

---

# Shared State

```go
type ToolManager struct {
	cacheMu   sync.RWMutex
	installMu sync.Mutex

	cache map[toolKey]InstalledTool
}
```

## Important

`installMu` is process-global.

It serializes ALL:

* version resolution
* install checks
* installs
* cache mutation

This is intentional.

The spec prioritizes correctness and reproducibility over parallel installs.

---

# Required Double-Check Sequence

## Canonical Algorithm

```text
FAST PATH
---------
1. Acquire cacheMu.RLock
2. Check cache for matching tool/version
3. If found:
       release RLock
       return cached result
4. Release RLock

SLOW PATH
---------
5. Acquire installMu

6. Re-acquire cacheMu.RLock
7. Re-check cache again
8. If now found:
       release RLock
       release installMu
       return cached result
9. Release RLock

10. Resolve version if needed
11. Verify binary existence
12. Install tool if missing
13. Verify installation succeeded

14. Acquire cacheMu.Lock
15. Update cache
16. Release cacheMu.Lock

17. Release installMu
18. Return installed tool
```

---

# Why The Second Cache Check Is Mandatory

Without the second check:

```text
Request A -> cache miss
Request B -> cache miss

A installs tool
B waits on installMu

B acquires installMu
B installs AGAIN unnecessarily
```

The second check prevents redundant installs after waiting.

---

# Required Lock Ordering

## Always:

```text
installMu
    ->
cacheMu.Lock / cacheMu.RLock
```

Never the reverse.

This avoids deadlocks.

---

# Required Cache Semantics

## Cache Key

```go
type toolKey struct {
	Name    string
	Version string
}
```

Version MUST already be normalized:

* exact semver
* `"latest"`
* `"unknown"`

---

# Required Unknown-Version Behavior

If cache contains:

```go
InstalledTool{
	Version: "unknown",
}
```

then treat it as compatible with ALL requested versions.

Reason:

* externally provided binaries
* Docker prebaked tools
* avoids reinstall loops

Required logic:

```go
if cached.Version == "unknown" {
	return cached, nil
}
```

This check applies:

* before installMu
* and after installMu

---

# Required Failure Semantics

## Install Failure

If install fails:

* DO NOT cache failure
* DO NOT cache partial state
* DO NOT leave corrupted entry

Return error immediately.

---

# Required Verification Step

After installation:

```go
os.Stat(binaryPath)
```

must succeed before cache update.

Never assume `go install` success means binary exists.

---

# Required Latest-Resolution Behavior

Version resolution for `"latest"` occurs INSIDE `installMu`.

Never outside.

Otherwise:

```text
Request A resolves v1.2.3
Request B resolves v1.2.4
```

during upstream release race windows.

The spec requires deterministic behavior.

---

# Required Context Cancellation Behavior

While waiting for install:

```go
select {
case <-ctx.Done():
    return ctx.Err()
default:
}
```

must be checked:

* before acquiring installMu
* after acquiring installMu
* before running go install

Long installs must remain cancellable.

---

# Recommended Reference Pseudocode

```go
func (m *ToolManager) EnsureInstalled(
	ctx context.Context,
	name string,
	version string,
) (InstalledTool, error) {

	key := toolKey{
		Name:    name,
		Version: version,
	}

	// FAST PATH
	m.cacheMu.RLock()

	cached, ok := m.cache[key]
	if ok || cached.Version == "unknown" {
		m.cacheMu.RUnlock()
		return cached, nil
	}

	m.cacheMu.RUnlock()

	// SLOW PATH
	select {
	case <-ctx.Done():
		return InstalledTool{}, ctx.Err()
	default:
	}

	m.installMu.Lock()
	defer m.installMu.Unlock()

	// SECOND CHECK
	m.cacheMu.RLock()

	cached, ok = m.cache[key]
	if ok || cached.Version == "unknown" {
		m.cacheMu.RUnlock()
		return cached, nil
	}

	m.cacheMu.RUnlock()

	select {
	case <-ctx.Done():
		return InstalledTool{}, ctx.Err()
	default:
	}

	resolvedVersion, err := m.resolveVersionLocked(...)
	if err != nil {
		return InstalledTool{}, err
	}

	installed, err := m.installLocked(...)
	if err != nil {
		return InstalledTool{}, err
	}

	m.cacheMu.Lock()
	m.cache[key] = installed
	m.cacheMu.Unlock()

	return installed, nil
}
```

---

# Explicit Non-Goals

The system intentionally does NOT support:

* parallel installs
* per-tool install locks
* speculative installs
* optimistic version resolution

Reason:

* reproducibility
* avoiding module-cache races
* simplifying correctness guarantees

---

# Required Tests

Add explicit concurrency tests:

## Required

### `TestEnsureInstalledDoubleCheckPreventsDuplicateInstall`

Verify:

* 20 concurrent requests
* exactly 1 install occurs

---

### `TestEnsureInstalledConcurrentLatestResolution`

Verify:

* only 1 latest-resolution call

---

### `TestEnsureInstalledFailureNotCached`

Verify:

* failed install does not poison cache

---

### `TestEnsureInstalledUnknownVersionAlwaysMatches`

Verify:

* unknown version satisfies all requests

---

### `TestEnsureInstalledContextCancellation`

Verify:

* cancelled request exits while waiting on install lock
