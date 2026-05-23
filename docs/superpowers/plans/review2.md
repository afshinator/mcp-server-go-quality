Almost. This revision is substantially better and is now very close to spec-complete, but I still see a few remaining mismatches and one regression.

# Status

Current state:

* Architecture: aligned
* Install locking: now properly specified
* Binary resolution: fixed
* govulncheck sequencing: aligned
* Workspace logic: aligned
* MCP protocol: aligned
* Concurrency model: aligned

I would now classify this as:

* ~97–98% spec compliant
* implementation-ready after a very small cleanup pass

---

# Remaining Problems

## 1. `parseUseDirectives()` still has the empty-fields panic edge case

You still have:

```go
dirs = append(dirs, strings.Fields(line)[0])
```



This is still fragile.

A malformed block like:

```go
use (
    // comment
)
```

can become empty after stripping comments.

## Required Fix

Replace:

```go
dirs = append(dirs, strings.Fields(line)[0])
```

with:

```go
fields := strings.Fields(line)
if len(fields) == 0 {
    continue
}
dirs = append(dirs, fields[0])
```

This is minor but should absolutely be hardened before implementation.

---

# 2. Runner error formatting is STILL not spec-safe

You still have:

```go
func (e *ExitError) Error() string {
    return e.Err.Error()
}
```



and again here: 

This is the last major semantic risk.

The spec requires canonical agent-facing formatting:

```text
Tool command failed with exit code N. Stderr: ...
```



Returning wrapped Go errors from `.Error()` is dangerous because:

* future code WILL accidentally surface it
* formatting drift will reappear later

## Best Fix

Make `ExitError.Error()` canonical directly:

```go
func (e *ExitError) Error() string {
	stderr := strings.TrimSpace(e.Stderr)

	if stderr == "" {
		return fmt.Sprintf(
			"Tool command failed with exit code %d.",
			e.ExitCode,
		)
	}

	return fmt.Sprintf(
		"Tool command failed with exit code %d. Stderr: %s",
		e.ExitCode,
		stderr,
	)
}
```

Then the entire system becomes safer by construction.

This is the main remaining thing I would still insist on changing.

---

# 3. One regression: old `cmd.Output()` implementation still exists in the plan

I found BOTH versions of the runner implementation.

One correct version using buffers exists. 

But the old broken version using:

```go
output, err := cmd.Output()
```

still exists later in the document. 

That creates ambiguity for implementers.

## Required Fix

Delete the old implementation completely from the plan.

Only keep the buffered stdout/stderr version.

---

# 4. Install lock sequence looks correct now

This section is now solid. 

You now correctly have:

* fast-path cache check
* install mutex
* second cache check
* latest-resolution inside lock
* verification before cache update
* unknown-version semantics
* cancellation awareness

This now matches the spec intent.

I would consider this fixed.

---

# 5. PATH fallback appears fixed

Good news:
I no longer see PATH fallback logic.

You now consistently:

* resolve `binDir` at startup
* pass `binDir` everywhere
* construct handlers with explicit binDir




This now aligns with the spec requirement:

> never exec.LookPath



This issue is fixed.

---

# 6. One small unresolved spec precision item

I still do NOT see explicit implementation of the nilaway “first sentence” extraction rule:

Spec requires:

* split on `. ` followed by uppercase ASCII
* OR first newline



I still don't see:

* dedicated helper
* explicit regex/state-machine logic
* explicit tests

This is not architecture-breaking anymore, but the spec is unusually precise here and the implementation plan should encode it.

## Recommendation

Add:

```go
func firstSentence(msg string) string
```

with explicit tests:

* `pkg.Type.Method`
* newline split
* abbreviation false-positive case
* lowercase continuation

---

# Final Verdict

At this point:

## MUST FIX

1. remove old `cmd.Output()` implementation from plan
2. make `ExitError.Error()` canonical
3. harden `parseUseDirectives()` empty-fields handling

## SHOULD FIX

4. explicitly specify nilaway sentence extraction helper/tests

After those, I would consider the plan effectively spec-complete and ready for implementation.
