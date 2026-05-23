Good. This is the strongest version of the spec by some margin. The major structural problems are gone. What remains is smaller but still real — here's everything I can find.

---

## Genuine bugs still present

**`go version -m` shell syntax survived into Auto-Install Rule #1.** The shell one-liner `go version -m $(which <tool>)` still appears in Auto-Install Rule 1 on line 312: `"reads the installed version via go version -m $(which <tool>)"`. The fix was correctly applied in the `install_tools` Output Format section (line 368), but the Auto-Install Rules section wasn't updated to match. There are now two contradictory descriptions of the same operation in the same spec.

**The code example doesn't collect results.** The `wg.Wait()` pattern shows goroutines being fired but `res` and `err` from `chk.Run(toolCtx, projectPath)` are never written anywhere — the results variable is assigned but immediately discarded. The comment says "timeout captured as Diagnostic.Error" but doesn't show how. An implementer using this as a reference will write a goroutine that drops all results. The pattern needs a results channel or a mutex-protected slice. This matters because the spec presents this block as the authoritative implementation pattern.

**NDJSON parse error: the inconsistency was never resolved.** The spec still says on line 181: "When `Error` is set, `File`/`Line`/`Column`/`Severity`/`Message`/`Native` are zero-valued. Exception: when the error is an NDJSON parse failure, `Native` carries a JSON-encoded string." But the NDJSON handling paragraph on line 276 says to set `error: "N line(s) failed to parse"` — meaning `Error` IS set. This was flagged in the last two reviews as an unresolved decision about whether to use `Error` or `Message`. The spec still says both simultaneously: `Error` is set (triggering the "zero other fields" rule) AND `Native` is populated (the documented exception). That's one exception. Fine. But the recommendation to use `Message` instead of `Error` was explicitly agreed to in discussion and was supposed to be implemented. It wasn't. The spec needs to commit: either `Error` + `Native` exception (current text, inconsistent), or `Message` + `Native` with `Error: ""` (recommended, not implemented).

---

## Logical problems

**The `.go-quality.yaml` example block appears twice, identically.** Lines 389–403 and lines 428–442 are byte-for-byte the same YAML block. The second one is inside the `latest` Resolution Policy section. This is a copy-paste error — the second block adds no information and will confuse implementers who think the duplication is intentional. Delete one.

**`install_tools` with a `tools` subset interacts unspecified with the `latest` resolution cache.** The spec says `install_tools` "bypasses the version cache and force-reinstalls all tools (or the requested tools subset), then updates the cache with newly resolved versions." If `install_tools tools:["golangci-lint"]` is called, does it update only the golangci-lint cache entry, or does it invalidate govulncheck and nilaway's cached entries too? An agent calling `install_tools tools:["govulncheck"]` to refresh only vuln check doesn't want to suddenly trigger a nilaway re-install on the next check call. The answer is clearly "only update the subset's cache entries" — but it's not stated.

**Root discovery walk-up has a filesystem boundary problem that's unspecified.** The spec says the server walks up looking for `go.work` then `go.mod`. It doesn't say where the walk stops. On Linux, walking up from `/project/monorepo/services/auth` will eventually reach `/`. If someone passes `project_path: "/tmp/some-random-dir"` and there's no Go project anywhere in the ancestry, the walk hits `/` and only then fails. This is technically handled by the fatal error ("not a Go project"), but the walk should stop at filesystem root or at the user's home directory — and if it hits root without finding anything, the error message "not a Go project" is correct. However, if the server is running as root and there happens to be a `go.mod` somewhere in `/`, it would resolve to a completely unexpected project. Specify the walk termination condition explicitly: stop at filesystem root, and also stop at any directory that isn't readable (permission error → treat as boundary).

**`go list -m -json <pkg>@latest` for `latest` resolution is a network call on first warm-up.** The spec presents this as acceptable — it happens once per process lifetime. But the spec also says the first incoming request triggers this. An agent calling `run_lint` for the first time will silently block on a network call to the Go module proxy before the tool even runs. The install step itself is sequential and logged to stderr, but the `latest` resolution call happens before the install decision and has no corresponding log message. Add: "the server logs to stderr: `Resolving <tool>@latest...` before the `go list` call, so the delay isn't silent."

---

## User/agent experience gaps

**No documented maximum response size.** golangci-lint on a large project with many linters enabled can return thousands of issues. Each issue has a `Native` field that duplicates the raw JSON. A 5000-issue run with native objects could produce a multi-megabyte JSON response. MCP has practical message size limits depending on the transport and client. The spec says nothing about this. Options: truncate after N diagnostics with a truncation marker, paginate, or document it as a known limitation. Not having a policy is itself a policy (unbounded), and agents or transports may silently fail on huge responses.

**`govulncheck` with no `go.sum` present.** govulncheck requires a `go.sum` file to run against a project. If someone runs it on a project that has a `go.mod` but not a `go.sum` (i.e. `go mod tidy` was never run), govulncheck exits non-zero with an error about missing checksums. The server returns a non-zero exit Diagnostic, which is technically correct. But the error message from govulncheck will be about checksum verification, which looks like a security tool failure rather than a missing setup step. Worth a sentence in the error table or the govulncheck section: "`go.sum` missing or incomplete → govulncheck exits non-zero; stderr will mention 'missing go.sum entry'; agent should prompt user to run `go mod tidy`."

**The govulncheck `osv.summary` fallback to `finding.osv` ID produces terse messages.** If `osv.summary` is absent, `Message` becomes something like `"GO-2026-4918"` — an opaque ID with no context. An agent surfacing this to a user would say "found vulnerability GO-2026-4918" which is nearly useless without clicking into `Native`. The ID-only case does happen: some older OSV records lack summaries. Consider falling back further: if summary is absent, try `osv.details` (the longer description field) truncated to 120 characters, before falling back to the bare ID.

---

## Minor / editorial

**`already_present` in the JSON example shows nilaway appearing in both `already_present` and `failed`.** Nilaway can't logically be in both arrays in the same response — it's either present or failed. The example should use different tool names across arrays.

**Testing strategy doesn't mention the version-cache or `latest`-resolution logic.** These are pure functions that are well-suited to unit tests (mock the `go list` call, verify the cache state transitions). They're non-trivial to implement correctly under concurrency and the spec doesn't call for testing them. Should be explicit.

**The spec has no file layout section.** There are references to `golangci_lint.go`, `govulncheck.go`, `nilaway.go`, `cmd/mcp-server-go-quality/main.go`, `testdata/sample_project/`, `AGENTS.md`, `.go-quality.yaml`, and `docs/tools-research.md` scattered throughout. An implementer has to mentally reconstruct the directory structure. A brief file tree — even 10 lines — would eliminate ambiguity about where things live and is the kind of thing that saves 20 minutes of "where does this go" questions during implementation.

---
my pushback to the above, since this is about spec, not plans...

The results channel / mutex in the goroutine example — that's implementation detail. The spec just needs to say results are collected; how is the plan's problem.
Maximum response size — operational concern, not spec.
go.sum missing — edge case operational note, belongs in AGENTS.md troubleshooting, not the spec.
File layout section — plan doc territory.
Walk termination at filesystem root — reasonable to mention the termination condition in one sentence, but the permission-error detail is implementation.
Testing strategy gaps around version cache — plan doc.


