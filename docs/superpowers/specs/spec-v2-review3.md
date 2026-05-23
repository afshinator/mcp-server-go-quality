This is a genuinely strong spec now — the major structural problems are resolved and it reads coherently end to end. What remains is a mix of real implementer traps, a few logical inconsistencies that survived the rewrites, and some user-facing gaps. I'll be direct.

---

## Still broken

**`go version -m $(which <tool>)` doesn't work the way the spec assumes.** `go version -m` takes a file path, not a shell pipeline. The `$(which <tool>)` expansion works in bash but this is a Go program constructing an `exec.Command` — you can't pass a subshell expression as an argument. The correct approach is two steps: `exec.LookPath(tool)` to get the binary path, then `exec.Command("go", "version", "-m", binaryPath)` with the result. The spec example shows `go version -m $(which <tool>)` in the parsing examples as if it's a literal command the server runs, which will produce `exit status 1` at runtime. Fix the example to show the two-step Go code, not the shell one-liner.

**NDJSON parse error Diagnostic contradicts itself.** The spec says "when `Error` is set, `File`/`Line`/`Column`/`Severity`/`Message`/`Native` are zero-valued — Exception: when the error is an NDJSON parse failure, `Native` carries a JSON-encoded string." But two paragraphs later the NDJSON handling says to set `error: "N line(s) failed to parse: <first error>"` — meaning `Error` IS set, yet `Native` is also populated. That's the documented exception, fine. But then it also says `Message` should be zero-valued when `Error` is set. The last review recommended flipping this: set `Message` and leave `Error` empty so it doesn't trigger the agent's error path. That recommendation was discussed and agreed to, but the spec wasn't updated — the NDJSON Diagnostic still sets `Error` rather than `Message`. The spec is internally inconsistent on this specific case and needs to commit to one approach.

**The `--config` default is wrong.** The CLI section says `--config PATH  path to .go-quality.yaml (default: ./.go-quality.yaml)`. But the Config Sources section says "if `--config` is absent, the server uses root discovery to find the workspace root and looks for `.go-quality.yaml` there." These are different things. The CLI help text implies the default is always `./.go-quality.yaml` in CWD. The config section correctly says it's discovered at the workspace root (which may be several directories up from CWD). When a user launches the server from `/project/monorepo/services/auth` and the config is at `/project/monorepo/.go-quality.yaml`, the CLI default text is simply wrong — it won't be found at `./`. The default should read `(default: discovered at workspace root)` or similar.

---

## Real implementer traps

**Version comparison against `latest` specifier is still unsolved for the cache warm-up check.** The cache stores resolved versions (e.g. `govulncheck → v1.3.0`). On the next request, `.go-quality.yaml` says `version: latest`. The server needs to decide: does `latest` match the cached `v1.3.0`? To know, it would have to run `go list -m ... @latest` again — a network call — every time. If it skips this check for `latest`, a newer version is never picked up until `install_tools` is explicitly called. If it always re-resolves `latest`, the "version-aware cache" provides no benefit for the most common configuration. The spec says re-install triggers "when `.go-quality.yaml` requests a different version than the cached one" but never specifies how `latest` is compared. This is a real decision that will bite the implementer on day one.

**The `go version -m` approach returns a pseudo-version for nilaway, not a clean semver.** The spec correctly shows `v0.0.0-20260515015210-fd187751154f` as an example nilaway version. When `.go-quality.yaml` specifies `version: latest` and the cache has `v0.0.0-20260515015210-abc123`, how does the server decide if a re-install is needed? `latest` resolved today might produce a different timestamp-hash. Without comparing the resolved current `latest` to the cached pseudo-version (requiring a network call), the server can't know. This is the same `latest` comparison problem but worse because pseudo-versions don't sort meaningfully. The spec needs an explicit policy: for tools that resolve to pseudo-versions, always treat `latest` as "use whatever is installed, re-install only when `install_tools` is called."

**Race condition in the version-aware cache under concurrent requests.** The spec removed `sync.Once` in favour of a version-aware cache, but doesn't specify the concurrency primitive protecting the cache. If two requests arrive simultaneously before the cache is warm, both will miss, both will try to install, and the "sequential install to avoid module cache races" protection only covers within a single pre-flight pass. The cache needs a `sync.Mutex` or `sync.RWMutex` and the spec should say so, or the old `sync.Once` approach for the initial discovery should be reinstated explicitly.

**`go version -m` on a hand-built binary returns module info only if the binary was built with `go build`.** The spec handles this ("reports `version: unknown`") — good. But it doesn't handle the case where `nilaway` is installed via a package manager (e.g. `brew install nilaway` on macOS, if that ever exists) and the binary has no module info at all. `unknown` is the right answer, but the spec should clarify: if `version: unknown`, is re-install triggered when a specific version is requested in `.go-quality.yaml`? Probably yes — but specify it.

---

## User/agent experience gaps

**`install_tools` always reinstalls all three tools regardless of the `tools` subset.** The spec says `install_tools` force-reinstalls all tools and updates the cache. But if an agent is only ever going to call `run_lint` (skipping nilaway permanently because it's too slow), calling `install_tools` at session start wastes time installing nilaway. There's no way to say `install_tools tools:["golangci-lint", "govulncheck"]`. This is a minor friction but worth an explicit note: either accept it as a known limitation, or add an optional `tools` parameter to `install_tools` consistent with `run_code_checks`.

**What happens when the project has a `.golangci.yml` that disables all linters?** golangci-lint will exit 0 with an empty `Issues` array. The server returns an empty Diagnostic slice for golangci-lint. From the agent's perspective this is indistinguishable from "no issues found" vs "linters are all disabled." An agent trying to validate that golangci-lint actually ran meaningfully has no signal. This may be acceptable by design ("not our problem — the user controls `.golangci.yml`") but should be stated explicitly so the implementer doesn't second-guess it.

**The govulncheck `finding.trace` extraction rule picks the last entry with a position.** This is documented as "the call site in user code." But in a multi-module workspace, the last trace entry might be in a workspace module that's a framework or shared lib, not the specific service the agent is working on. The "last entry" heuristic works for single-module projects and is reasonable, but in a workspace with modules `[user/myapp, user/shared-lib]`, a vulnerability reachable through `shared-lib` will have its `File` pointing to `shared-lib` not `myapp`. An agent navigating by `file:line` will land in a dependency rather than in the code it owns. Not a spec bug per se, but worth a note so agents know to check `Native` for the full trace when `File` points somewhere unexpected.

**No mention of what happens when `project_path` is valid but the Go project has build errors.** golangci-lint and govulncheck can partially handle projects that don't build cleanly, but nilaway typically cannot — it requires a fully type-checked program. If the project has syntax errors, nilaway will exit non-zero with a compiler error in stderr, and the server returns a Diagnostic with `error: "Tool command failed with exit code 1. Stderr: <compiler errors>"`. This is technically correct behaviour but an agent seeing this will be confused — it looks like nilaway crashed, not that the project has build errors. A note here, or a specific error message pattern the agent can recognise, would help.

---

## Minor / editorial

**The data flow pseudocode still shows all three goroutines unconditionally** — `go runLint`, `go runVuln`, `go runNil` — even though the spec now filters by the `tools` parameter. The `Filter to requested tools` step is there, but the goroutine lines below it don't reflect the conditional. A reader implementing from the pseudocode alone would fire all three regardless.

**"Already present" in the `install_tools` response includes tools that were installed in a previous process run** — things found on PATH that this process didn't install. The spec calls them "already_present" but they're really "found on PATH." If a user manually installed govulncheck at a wrong version and the server finds it, it shows up in `already_present` even though the version doesn't match the requested one. But wait — the spec says already_present means "tools already on PATH with a matching version." That implies version-mismatched tools are re-installed and land in `installed`. But the spec doesn't say what happens if they land in neither (install fails for a version-mismatch case) — they'd be in `failed`. This is probably fine but the three-way classification needs a sentence confirming: tools found on PATH at the wrong version are attempted re-installs, not reported as `already_present`.

**The References section still doesn't include `AGENTS.md`** despite the Recommended Agent Workflow section promising it exists.

**`wg.Add(1)` appears in the code example but `wg` is never declared.** Small, but the spec is presenting this as a reference implementation pattern. A reader copy-pasting it gets a compile error. Either show the full pattern with `var wg sync.WaitGroup` or drop the `wg` calls and use channels instead, which is what a real Go implementation would likely use anyway.

----

Most yes, some require a decision. Let me go through each:

---

**Clear fixes — just write the code/text:**

**`go version -m $(which <tool>)`** — replace the shell example with the correct two-step Go pattern:
```go
binaryPath, err := exec.LookPath(toolName)
// ...
cmd := exec.Command("go", "version", "-m", binaryPath)
```
No decision needed, just fix the example.

**`--config` default text** — change `(default: ./.go-quality.yaml)` to `(default: .go-quality.yaml at discovered workspace root)`. One line change.

**`wg` undeclared in code example** — add `var wg sync.WaitGroup` before the loop, add `wg.Wait()` after. Straightforward.

**Data flow pseudocode still shows all three goroutines unconditionally** — add `if requested` conditionals or rewrite as `for _, tool := range requestedTools { go run(tool) }`. One line fix.

**AGENTS.md missing from References** — add `- AGENTS.md — agent usage guide (repo root)`. Trivial.

**`already_present` three-way classification gap** — add one sentence: "Tools found on PATH at the wrong version are treated as missing and attempted re-install; they appear in `installed` on success or `failed` on failure, never in `already_present`." Clear, no decision required.

**Race condition on the version cache** — add one sentence to Design Rules: "The version cache is protected by a `sync.RWMutex` — read lock for cache lookups, write lock for installs and cache updates." The implementation pattern is standard Go, no design decision involved.

**Build errors making nilaway look crashed** — add a row to the error table: "Tool exits non-zero with compiler errors in stderr → same Diagnostic as any non-zero exit; agent should check stderr content for `syntax error` or `undefined:` patterns to distinguish build failure from tool failure." Also worth a note in AGENTS.md troubleshooting. No design decision, just documentation.

---

**Require a decision from you:**


**`latest` version comparison in the cache** — three options, each with a different tradeoff:
- Never re-resolve `latest` between requests; only update when `install_tools` is called explicitly. Simple, predictable, but means the server can run stale tools indefinitely
- Re-resolve `latest` on every request via a network call. Always current, but defeats the cache's purpose and adds latency to every check
- Re-resolve `latest` once per process lifetime (on first warm-up), then treat the cache as authoritative until `install_tools` is called. Best balance — pick this one if you want a recommendation

The pseudo-version problem for nilaway is a corollary of this decision: if you adopt option 3, pseudo-version tools follow the same rule and the problem disappears.

**`install_tools` subset parameter** — do you want `install_tools` to accept an optional `tools` array like `run_code_checks` does? The current spec force-installs all three always. Adding the parameter makes it consistent and more useful for agents that permanently skip one tool. Costs one extra parameter definition and some implementation lines. Yes lets do it.