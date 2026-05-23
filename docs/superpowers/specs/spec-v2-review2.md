
## Still broken or contradictory

**The `already_present` version field is misleading by the spec's own admission.** The spec says `already_present` reports "the version the server *believes* is installed (what was recorded at install time, not what's on disk)". On the first ever call to `install_tools`, nothing has been installed by this process — the version-aware cache is cold. The server has no recorded install time. It can only run `exec.LookPath` and get a path. So the first `install_tools` call either can't populate `already_present` reliably, or the server needs to run `<tool> --version` to discover what's actually there. The spec doesn't say which. This is a concrete implementer question that will surface immediately. This is a concrete implementer question that will surface immediately.

**Version-aware cache and `install_tools` interact inconsistently.** Rule 1 says the cache tracks `toolName@version` pairs so re-install happens when a different version is requested. But `install_tools` "bypasses the cache and force-reinstalls all tools." After `install_tools` runs, what's in the cache? If the cache was just bypassed (not updated), the next check call will see a cache miss and try to install again. If the cache is updated by `install_tools`, specify that. The update path must be explicit.

**Root discovery + `--config` path interact unspecified.** The server walks up from `project_path` to find the workspace root, and reads `.go-quality.yaml` from "the target project". But if `--config /custom/path.yaml` is passed, where does the timeout and tool config come from — the custom file? The discovered workspace root? What if `project_path` is `/monorepo/services/auth` but `--config` points to `/monorepo/.go-quality.yaml`? These are probably the same file in practice, but the spec doesn't say so explicitly. An implementer has two reasonable readings: "read config from discovered root always" vs "read config from `--config` path always". Needs one sentence of clarification.

---

## Significant gaps

**`tools` subset parameter has no validation error specified.** If an agent passes `tools: ["golangci-lint", "typo"]`, the spec doesn't say what happens. Fatal top-level error? Silently ignored? Return a diagnostic? Given the decision rule established in Error Handling, a bad `tools` value is a validation failure and should probably be fatal — but it's not in the error table.

**`osv.severity` field is probably not what you think.** The govulncheck NDJSON format's `osv` object follows the OSV schema. The severity field there is an array of objects (`[{"type": "CVSS_V3", "score": "CVSS:3.1/AV:N/..."}]`), not a simple string. Mapping that to `Severity: "warning" | "error"` requires a decision rule (e.g. CVSS score ≥ 7 → "error", else "warning") that the spec doesn't define. If the spec means to just pass through the raw severity string from somewhere else in the govulncheck output, identify exactly which field. This is the one extraction rule most likely to cause a runtime panic or silent empty string when the implementer tries to assign an array to a string.


**The `tools` parameter on `run_code_checks` doesn't appear in the data flow.** The data flow shows `Filter to requested tools (from tools param, default all)` — good. But it doesn't show what happens if the filtered set is empty after the subset is applied (e.g. agent passes `tools: []`). Empty slice = all three, or return an error?

**`govulncheck latest` install version reporting.** `already_present` would show `"govulncheck@latest"` for a tool installed at `latest`. But `latest` resolves to a real semver at install time. An agent checking "is the right version installed" can't compare `latest` to `v0.2.1`. The version cache should record the resolved version, not the requested specifier.

---

## User/agent experience issues

**The govulncheck vuln DB download is not mentioned in the timeout guidance.** This is a known operational problem with govulncheck — the first invocation downloads the Go vulnerability database (~40MB), which can take 30–60 seconds on a slow connection and happens *inside* the tool execution, not during install. The 5-minute default timeout covers it, but agents (and users configuring timeout) have no idea why govulncheck is slow the first time. Worth a note in the timeout section or the Agent Workflow.

**`run_code_checks` with `tools: ["nilaway"]` is equivalent to `run_nil_check` — why?** The spec now has both. That's fine, but the Recommended Agent Workflow says "single-tool shortcuts are available for incremental use" without acknowledging that `tools: ["nilaway"]` on `run_code_checks` does the same thing (but also triggers the pre-flight for all three tools unnecessarily). The two paths should be differentiated: single-tool MCP calls are lighter because they only install and discover the one needed tool, not all three.

**`already_present` version string format is inconsistent with `installed`.** `installed` uses `"golangci-lint@v2.11.4"` (bare string with `@version`). `already_present` also uses `"govulncheck@latest"` strings. But `failed` is an array of objects. For programmatic consumption, consider making all three arrays use the same shape — either all strings or all objects. Mixed shapes mean agents need two code paths to read the same response.

---

## Minor / editorial

**"per-tool deadline" vs earlier language.** The `.go-quality.yaml` example comment now correctly says `# per-tool deadline (default: 5m)` — good. But the Timeout Model section says "The total wall clock is bounded by the slowest tool (max cfg.Timeout)." This is correct but could mislead: if all three tools hit the deadline, total wall clock is exactly `cfg.Timeout`, not "max" in any dynamic sense. Rephrase to "total wall clock is at most `cfg.Timeout`."

**testdata govulncheck caveat is good but incomplete.** The note that "whether a CVE is found depends on the installed Go version's vulnerability database" is correct. But it implies this test is non-deterministic, which makes it a bad integration test. Consider pinning the sample project's vulnerable dependency to a specific old version with a *known* CVE (e.g. an old `golang.org/x/net` with a documented GO-XXXX ID) so the test is deterministic. The spec could just recommend this approach without mandating the exact package.

**`NDJSON parse error` Diagnostic has `Tool: "govulncheck"` but no `File`, `Line`, or `Message`.** The spec says `Message` is zero-valued when `Error` is set — fine. But a parse-error Diagnostic mixed into a results array is hard for agents to distinguish from a tool-execution error. Consider whether the NDJSON parse error Diagnostic should set `Message: "N line(s) failed to parse"` rather than `Error`, to avoid triggering the agent's error-handling path for what is essentially a data-quality warning. This is a design question, not a bug — but it should be answered.


--- Proposed fixes to some of the above

# Spec v2 — Targeted Fixes

These are drop-in replacements for specific sections of spec-v2.md.
Each fix is labelled with the issue it addresses.

---

## Fix 1 — `already_present` version field + `install_tools` cache update
*(addresses: "already_present misleading on cold cache", "install_tools doesn't update cache")*

Replace the `install_tools` output section (currently under **### install_tools Output
Format**) with:

---

### install_tools Output Format

`install_tools` returns a structured JSON object so agents can act on it
programmatically:

```json
{
  "installed": [
    { "tool": "golangci-lint", "version": "v2.11.4" }
  ],
  "already_present": [
    { "tool": "govulncheck", "version": "v0.2.1" },
    { "tool": "nilaway",     "version": "v0.19.0" }
  ],
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

All three arrays use the same per-entry object shape: `{ tool, version }` for success
entries; `{ tool, version, command, stderr }` for failures. Agents can use a single
iteration pattern across all three.

**Version discovery for `already_present`:** to report a real version (not a cache
assumption), the server runs `<tool> version` before deciding the tool is current:

- `golangci-lint version` → parse semver from first line
- `govulncheck -version` → parse semver from output
- `nilaway -version` → parse semver from output

If a `--version`-style call fails or returns unparseable output, the entry is reported
as `{ "tool": "<name>", "version": "unknown" }` rather than omitting it or fabricating
a version.

**Cache update after install_tools:** after `install_tools` completes (whether
force-install or discovery-only), the version-aware cache is updated with the resolved
`toolName@resolvedVersion` pairs. The next check call will find the cache warm and skip
all discovery. This ensures `install_tools` at session start eliminates the pre-flight
overhead from the first `run_code_checks` call.

---

## Fix 2 — govulncheck severity mapping
*(addresses: "`osv.severity` is an array, not a string")*

Replace the govulncheck severity line in **Severity mapping by tool** and in the
govulncheck extraction rules:

---

**Severity mapping — govulncheck:**

The OSV schema's `severity` field is an array of objects
(`[{"type": "CVSS_V3", "score": "CVSS:3.1/AV:N/..."}]`), not a plain string.
Map it to the `Diagnostic.Severity` string using this rule:

1. Find the first entry in `osv.severity` where `type` is `"CVSS_V3"` or `"CVSS_V4"`.
2. Extract the base score from the `score` vector string:
   - CVSS v3: the `B` component in `CVSS:3.x/.../B:X` — parse the numeric base score
     from the vector, or use the pre-computed `baseScore` if the OSV record includes it.
   - In practice, the Go vuln DB includes a pre-computed `database_specific.severity`
     field (`"LOW"`, `"MEDIUM"`, `"HIGH"`, `"CRITICAL"`) — prefer that if present.
3. Map to `Diagnostic.Severity`:
   - `"CRITICAL"` or `"HIGH"` → `"error"`
   - `"MEDIUM"` or `"LOW"` → `"warning"`
   - No severity data at all → `""`

If the `osv` object has no `severity` array and no `database_specific.severity`, set
`Severity: ""`. Never panic on a missing or unexpected severity shape — default to `""`.

**Updated extraction rules for govulncheck:**

- `File`/`Line`/`Column`: from the last `finding.trace` entry that has a `position`
- `Severity`: mapped from `osv.database_specific.severity` (preferred) or CVSS vector
  per the rule above; `""` if absent
- `Message`: from `osv.summary`; fall back to `finding.osv` ID if summary is absent
- `Native`: the raw `finding` object + associated `osv` object

---

## Fix 3 — `tools` parameter validation
*(addresses: "no validation error specified for bad `tools` values")*

Add to the **Error Table** (after the `.go-quality.yaml` unparseable row):

| `tools` contains unrecognised value | Fatal | Top-level error: `"unknown tool: \"<value>\". valid values: golangci-lint, govulncheck, nilaway"` |
| `tools` is empty array `[]` | — | Treated as omitted — runs all three checkers |

And add to the **Decision Rule** fatal list:

- `tools` parameter contains an unrecognised checker name

---

## Fix 4 — `already_present` and `installed` consistent shape
*(addresses: "mixed shapes in install_tools response")*

This is already resolved by Fix 1 (all entries use the same `{tool, version}` object
shape). The previous format used bare strings like `"golangci-lint@v2.11.4"` for
`installed` but objects for `failed`. Fix 1 makes all three arrays use objects.

No additional change needed beyond Fix 1.

---

## Fix 5 — resolved version stored in cache, not requested specifier
*(addresses: "`govulncheck@latest` in cache can't be compared to a real semver")*

Replace the version-pinning cache rule in **Design Rules** ("Lazy install with
version-aware caching"):

---

**Lazy install with version-aware caching** — tool discovery runs on the first
incoming request from any check tool. The cache tracks `toolName → resolvedVersion`
(e.g. `govulncheck → v0.2.1`), not the requested specifier (`latest`). When
`.go-quality.yaml` requests `latest`, the server resolves the version by running
`go list -m -json <pkg>@latest` before installing, and stores the resolved semver.
On subsequent calls, the server compares the requested specifier's resolved version
against the cached resolved version — if they match, no reinstall. `install_tools`
bypasses the cache and force-reinstalls all tools, then updates the cache with the
newly resolved versions.

---

## Fix 6 — testdata govulncheck determinism
*(addresses: "govulncheck test is non-deterministic")*

Replace the govulncheck bullet in **testdata/sample_project Minimum Content**:

---

- **govulncheck:** A `go.mod` with a pinned dependency known to have a recorded
  vulnerability, e.g. `golang.org/x/net v0.0.0-20210226172049-4d89b558e7d3` (has
  multiple GO-YYYY-NNNN entries). Pin the version explicitly in `go.mod` and commit
  a `go.sum`. This makes the test deterministic regardless of the local vuln DB
  version — the vulnerability is historical and will always be present. Do not use
  a `latest` or floating dep for the integration test fixture.

---

## Fix 7 — NDJSON parse error as warning, not error
*(addresses: "parse-error Diagnostic is hard to distinguish from tool-execution error")*

Replace the NDJSON parse error handling paragraph in the govulncheck section:

---

**NDJSON parse error handling:** The parser must accumulate `json.Unmarshal` errors
per line rather than silently skipping them. If any lines failed to parse, append a
single Diagnostic with:

```go
Diagnostic{
    Tool:    "govulncheck",
    Message: fmt.Sprintf("%d line(s) failed to parse: %s", n, firstErr),
    Error:   "",   // intentionally empty — this is a data-quality warning, not a tool failure
    Native:  json.RawMessage(mustMarshalString(rawLines)), // JSON-encoded string of raw content
}
```

Setting `Error: ""` and populating `Message` instead means this entry sorts alongside
normal findings rather than triggering the agent's error-handling path. An agent
filtering `d.error != ""` for failures will not trip on it; an agent reading all
messages will see the warning naturally. The raw unparseable content in `Native`
gives a developer enough to file a bug or check for a govulncheck format change.

---

## Leftover questions

**`run_code_checks tools:["nilaway"]` vs `run_nil_check`**

The clear answer is: single-tool MCP calls (`run_lint`, `run_vuln_check`, `run_nil_check`) should only discover and install the one tool they need. `run_code_checks` with a `tools` subset should discover and install all tools in the subset, not all three. The rule is "pre-flight installs exactly the tools that are about to run, nothing more."

The reason this is clear: the whole point of the subset parameter is to skip slow or problematic tools. If `run_code_checks tools:["golangci-lint"]` still triggers nilaway's install (which can be slow on first run), you've defeated the purpose. An agent skipping nilaway because it's not yet annotated shouldn't pay nilaway's install cost. The two call paths become genuinely equivalent in cost and behaviour for the same subset — which is the right property. Document `run_nil_check` as syntactic sugar for `run_code_checks tools:["nilaway"]` and note they are identical in behaviour including pre-flight scope.

---

**`--config` flag + root discovery interaction**

The clear answer is: `--config` always wins for the file path, and root discovery is only used when `--config` is absent. The server never combines them.

Concretely: if `--config /custom/path.yaml` is passed, that file is the config regardless of where `project_path` resolves to. If `--config` is absent, the server uses root discovery to find the workspace root and looks for `.go-quality.yaml` there. The two mechanisms are mutually exclusive — `--config` short-circuits root discovery entirely for config loading purposes.

The reason this is clear: `--config` exists precisely for cases where the config isn't co-located with the project — CI systems, shared team configs, testing with a non-default file. If root discovery could silently override or combine with it, `--config` becomes unpredictable. The MCP client that passes `--config` is being explicit; explicit always beats implicit. Root discovery is the fallback for when no explicit path is given.

The one wrinkle worth noting in the spec: root discovery still runs regardless, because the discovered root determines `cmd.Dir` for tool execution. `--config` only affects which YAML file is read — it has no bearing on where the tools are invoked from. These are two separate uses of the root and should be documented as such to avoid future confusion.

---

## Resolution Log (2026-05-23)

All issues researched against live tool output (`golangci-lint version`, `govulncheck -version`, `nilaway -V=full`, `go version -m`, `govulncheck -json ./...` against real vulnerable project). Key research findings:

- **govulncheck severity does not exist**: Zero `severity` fields across all OSV entries in the Go vulnerability database. `database_specific` contains only `url` and `review_status`. Fix 2 (severity mapping) rejected.
- **`go version -m $(which <tool>)` works universally**: Stable parseable output for all three tools. No per-tool flag guessing needed. Replaces Fix 1's `<tool> version` approach.
- **nilaway has no tagged releases**: `go list -m -versions go.uber.org/nilaway` returns zero versions. `go version -m` gives the pseudo-version (`v0.0.0-...`), which is stored in the cache.
- **`go list -m -json <pkg>@latest` resolution works**: Resolves `latest` to real semver for govulncheck and golangci-lint.

| Fix | Status | Notes |
|---|---|---|
| Fix 1 (already_present version discovery) | **Accepted** — uses `go version -m` not `<tool> version` | Spec updated with universal version discovery |
| Fix 2 (govulncheck severity mapping) | **Rejected** — field doesn't exist | govulncheck `Severity` always `""` |
| Fix 3 (tools validation errors) | **Accepted** | Added to error table and fatal conditions |
| Fix 4 (consistent object shapes) | **Accepted** | Folded into Fix 1; all three arrays use `{tool, version}` |
| Fix 5 (resolved version cache) | **Accepted** — simplified | Cache stores `go version -m` output post-install |
| Fix 6 (deterministic testdata) | **Accepted** | Pinned `golang.org/x/net v0.0.0-20210226...` + call-graph reachable code |
| Fix 7 (NDJSON parse as Message vs Error) | **Rejected** — kept in `Error` | Per agent recommendation: parse failure is a tool execution problem |

Additional changes integrated:
- `--config` + root discovery: mutually exclusive for config loading; root discovery always runs for `cmd.Dir`
- Pre-flight scope: install only tools about to run (subset or single-tool)
- Timeout wording: "at most `cfg.Timeout`"
- govulncheck vuln DB download note in timeout section
- AGENTS.md: consistent object shapes, tools validation error, resolved versions in examples