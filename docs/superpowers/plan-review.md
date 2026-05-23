# Plan Review: Adversarial Analysis (Rated)

**Date:** 2026-05-23  
**Spec:** `docs/superpowers/specs/2026-05-23-go-quality-mcp-design.md`  
**Plan:** `docs/superpowers/plans/2026-05-23-go-quality-mcp-plan.md`

Each finding is rated: **VALID** (confirmed defect), **INVALID** (re-examination disproves), **PARTIALLY VALID** (true in part, overblown, or needs verification).

---

## VALID — Confirmed Defects

### #1 — ExtraArgs from config never used ⚠️ Critical
- **Spec requirement:** `extra_args` should be "appended to required flags" for each tool
- **Plan defect:** Config struct loads `ExtraArgs []string`, but handlers in Tasks 7-9 hardcode all args (e.g., `args := []string{"run", "--out-format=json", "./..."}`) with no reference to `ExtraArgs`
- **Fix:** Append `cfg.Tools["golangci-lint"].ExtraArgs...` in each handler's args

### #3 — Timeout error formatting missing ⚠️ Critical
- **Spec requirement:** "timed out after `<duration>`"
- **Plan defect:** `context.WithTimeout` is used but the resulting `context.DeadlineExceeded` error is returned raw — no format to include duration
- **Fix:** In orchestrate or each handler, check `ctx.Err() == context.DeadlineExceeded` and wrap with duration

### #6 — nilaway module name reads from wrong directory ⚠️ High
- **Plan defect:** `readModuleName` calls `r.Run(ctx, "go", "list", "-m")` accepting `projectPath` but never using it — `ExecRunner.Run` never sets `cmd.Dir`
- **Impact:** Returns module name of server's CWD, not target project
- **Fix:** Set `cmd.Dir = projectPath` inside `readModuleName` or pass it through runner

### #7 — Working directory never set for tool execution ⚠️ High
- **Plan defect:** `ExecRunner.Run` doesn't set `cmd.Dir`. When `project_path` differs from server CWD, all tools run against the wrong directory
- **Impact:** `./...` patterns resolve against server CWD, not target project
- **Fix:** Runner needs `Dir` support, or each handler must set it before exec

### #8 — Tool install missing from single handlers ⚠️ High
- **Spec requirement:** Pre-flight install before any tool runs
- **Plan defect:** `ensureToolsInstalled` is only called in `RunAllChecks`. `makeSingleHandler` for `run_lint`, `run_vuln_check`, `run_nil_check` jumps straight to execution
- **Impact:** Single-tool handlers fail if tools aren't pre-installed via prior `install_tools` or `run_code_checks`
- **Fix:** Add install check to `makeSingleHandler` before calling checker

### #9 — Silent error swallowing in govulncheck parser
- **Plan defect:** `if err := json.Unmarshal(line, &entry); err != nil { continue }` — skips malformed NDJSON lines with no logging
- **Fix:** Accumulate parse errors and report alongside diagnostics

### #10 — Race condition in tool discovery (TOCTOU)
- **Plan defect:** `IsInstalled()` then `Install()` is not atomic
- **Mitigation:** Low severity for single-server use; document limitation

### #11 — Install handler message format broken
- **Plan defect:** `fmt.Sprintf("Installed: %v. Failed: %v. Already present: %v", installed, failed, installed)` — `installed` slice printed twice, "Already present" never tracked separately
- **Fix:** Track `alreadyPresent` in a separate slice

### #15 — Error ignored in single handler marshal
- **Plan defect:** `b, _ := json.Marshal(diags)` — marshal error discarded silently
- **Fix:** Handle the error

### #18 — Context cancellation not distinguished
- **Plan defect:** Orchestrate treats all errors identically — doesn't distinguish timeout vs tool crash vs config error
- **Fix:** Check error types in the result loop and set `Diagnostic.Error` accordingly

---

## PARTIALLY VALID — True in Part or Needs Verification

### #4 — Non-JSON output handling (valid but minor)
- **Spec says:** "unexpected output format from `<tool>`"
- **Plan returns:** raw unmarshal errors like `parsing golangci-lint output: invalid character...`
- **Partial:** The info is all there, just not in the spec's exact phrasing. Low priority.

### #12 — nilaway `-include-pkgs` flag (needs verification)
- **Plan uses:** module name directly (e.g., `github.com/myorg/myapp`)
- **Uncertain:** Whether nilaway expects module path vs package wildcard pattern vs `./...` semantics
- **Needs:** Verify against installed nilaway behavior

### #14 — Integration test path fragility (minor, idiomatic)
- **Plan uses:** `../../testdata/sample_project`
- **Partial:** Standard Go practice (many projects use relative paths to testdata), but adds an implicit requirement that tests run from the package directory. Not a real problem for `go test`.

### #16 — Missing edge case tests (partially covered)
- **Spec lists:** missing tools, broken config, empty project, non-Go directory, timeout
- **Plan coverage:** "non-Go directory" tested via `TestValidateProjectPathNoMod`; others not explicit
- **Partial:** Some gaps are real but some are implicitly covered or tested at integration level

### #17 — golangci-lint config validation gap (process concern, not code)
- **Spec note:** "must be validated against installed v2.11.4 on first run"
- **Plan defect:** No explicit validation step
- **Partial:** More of a runtime verification procedure than a code defect

---

## INVALID — Re-Examination Disproves

### #2 — Error field semantics **already enforced**
- **Claim:** Orchestrate doesn't clear File/Line/Message/Native when setting Error
- **Reality:** The plan code creates `Diagnostic{Tool: "server", Error: r.err.Error()}` with no other fields — Go zero-values them to `File=""`, `Line=0`, `Message=""`, `Native=nil`. Semantics are correct by construction.
- Additionally, `r.diags` is appended separately — successful results from other goroutines (with empty `r.err`) correctly carry their own fields. No conflict.

### #5 — Single-tool exports **already in plan**
- **Claim:** Tasks 7-9 don't create public exports, Task 11 references nonexistent symbols
- **Reality:** Task 11 Step 2 **explicitly** says: "Each checker file must export a public function wrapping its private handler. Add to each:" followed by the function bodies. The exports are created in Task 11, not Tasks 7-9. The task order is intentional — public exports aren't needed until the server glue is written.

### #13 — Extraction rules **already validated by tests**
- **Claim:** Tests don't verify govulncheck picks the "last trace entry with position"
- **Reality:** The `TestParseGovulncheckOutputFindsVulnerabilities` test provides a `finding` with two trace entries — stdlib at line 586 then user code at line 78 — and asserts `d0.Line != 78` (expecting 78). The implementation loops from `len-1` to 0, picking the last position-bearing entry. The test input + assertion covers this rule.

---

## Summary

| Rating | Count | Issues |
|--------|-------|--------|
| Valid | 10 | #1, #3, #6, #7, #8, #9, #10, #11, #15, #18 |
| Partially Valid | 5 | #4, #12, #14, #16, #17 |
| Invalid | 3 | #2, #5, #13 |

**Verdict:** 10 confirmed defects, 3 of them critical/high severity requiring fix before implementation. 3 of the original 18 claims don't hold up on re-examination.

---

## Second-Pass Verification: Confirming My Own Ratings

I re-read all three documents (spec, plan, review) and verified each rating against the actual plan code. My original ratings held — the findings are accurate. The 10 valid defects, 5 partially valid, and 3 invalid are correctly classified.

Key verifications:
- **#1** — Confirmed: `runGolangciLint`, `runGovulncheck`, `runNilaway` all hardcode args with no ExtraArgs access
- **#2** — Confirmed: `Diagnostic{Tool: "server", Error: r.err.Error()}` uses Go struct literal with only those two fields; File/Line/Message/Native are zero-valued by language semantics
- **#3** — Confirmed: `defer cancel()` on shared timeout context; errors returned as raw context errors with no duration formatting
- **#5** — Confirmed: Task 11 Step 2 explicitly adds `RunGolangciLintOnly`, `RunGovulncheckOnly`, `RunNilawayOnly` exports to each checker file
- **#6/#7** — Confirmed: `readModuleName` accepts `projectPath` but the underlying `ExecRunner.Run` never sets `cmd.Dir`; `go list -m` runs from server CWD
- **#8** — Confirmed: `makeSingleHandler` has no install check; jumps directly to checker call
- **#9** — Confirmed: NDJSON loop uses `continue` on unmarshal errors with no accumulation
- **#11** — Confirmed: format string `("Installed: %v. Failed: %v. Already present: %v", installed, failed, installed)` — `installed` printed in position 1 and 3
- **#13** — Confirmed: test `if d0.Line != 78` expects line 78 (last trace entry), implementation loops `len-1` to 0, test correctly verifies extraction rule
- **#15** — Confirmed: `b, _ := json.Marshal(diags)` in `makeSingleHandler` discards marshal error

---

