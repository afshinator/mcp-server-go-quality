This is genuinely close to done. The list of real issues is short now.

---

## Remaining bugs

**The goroutine example still drops results.** `res, err := chk.Run(...)` assigns to variables that are never used. `wg.Wait()` returns but there's no result collection. This has been flagged twice. It's still there. The comment "timeout captured as Diagnostic.Error" implies results go somewhere, but the code shows they don't. The spec doesn't need to show the full channel plumbing, but it should at minimum not show dead assignments. Either remove the `res, err :=` line and replace the comment with "results sent to a shared channel before wg.Done()", or drop the code example entirely and describe the pattern in prose. Showing wrong code as a reference pattern is worse than no code.

---

## Spec-level gaps that remain

**`version: unknown` and version-mismatch re-install: the interaction is unspecified.** Line 381 says if `go version -m` fails, report `version: unknown`. But what does the cache do with `unknown`? If the cache stores `govulncheck → unknown` and `.go-quality.yaml` requests `version: latest`, the server compares `unknown` to the `latest`-resolved version — they differ, so it triggers a re-install on every single request. This is the worst of both worlds: silent repeated network calls. The policy needs one sentence: "if the cached version is `unknown`, treat it as always-matching to avoid re-install loops; the tool was found on PATH and is presumed usable."

**`go list -m -json <pkg>@latest` is called during `latest` resolution, but `<pkg>` is never defined in the spec.** The module paths for each tool are implied by the install commands shown elsewhere but never explicitly mapped. An implementer has to reverse-engineer them from the example `go install` commands. Trivial to fix: add a mapping table somewhere — `govulncheck → golang.org/x/vuln/cmd/govulncheck`, `nilaway → go.uber.org/nilaway/cmd/nilaway`, `golangci-lint → github.com/golangci/golangci-lint/v2/cmd/golangci-lint`.

**The `latest` resolution policy has a bootstrapping gap.** The policy says: on first request, run `go list -m -json <pkg>@latest`, then install if the resolved version isn't cached. But if the tool is already installed at an older version and `latest` resolves to a newer one, a re-install is triggered. Fine. But `go list -m -json` itself requires network access to the Go module proxy. If the server is running in an air-gapped environment with a private proxy (`GOPROXY=off` or `GOPROXY=direct` pointing nowhere), this call fails. The spec says `go install` will fail in that case too — but `go list` failing is a different error path. Does a `go list` failure for `latest` resolution cause a fatal error, fall back to whatever is installed, or report the tool as unavailable? Not specified.

---

## Small issues

**The `latest` Resolution Policy section says "on the first request that warms the cache."** That phrase is ambiguous — the cache could already be warm from a previous request to a different tool. The first `run_lint` call might have already resolved and cached govulncheck's `latest` as a side effect. Rephrase: "on the first request that needs this tool's version, if it isn't already cached."

**The data flow pseudocode shows `toolCtx` being created inside the goroutine loop but it's referenced before that** — `go runHandler(tool, toolCtx, r, path)` on line 135 uses `toolCtx` as if it already exists, but the Timeout Model section shows `toolCtx` is created inside each goroutine. The pseudocode and the timeout model are inconsistent about where `toolCtx` is born. Small, but an implementer will notice.

**The govulncheck example NDJSON still shows a multi-line `finding` object.** NDJSON by definition has one JSON object per line — each line must be a complete, self-contained object. The example spans multiple lines for readability, which is fine as documentation, but there's no note clarifying this is a formatted representation. A reader unfamiliar with NDJSON might think govulncheck actually emits pretty-printed multi-line objects and that `bufio.Scanner` line-by-line reading would break on them. Worth one sentence: "the multi-line formatting above is for readability; each object is emitted as a single line in actual output."