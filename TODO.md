# TODO

The live backlog. **This is the file to read to find out what is next.**

- `docs/mcp-spec-currency.md` is the *audit record* — the evidence and reasoning
  behind the MCP migration. It is history, not a work list.
- This file is the *work list*. Keep it short: open items only, with enough
  detail that someone can act without re-deriving the context.
- Nothing enforces this file. If you close an item, move it to Done. If you find
  new work, add it here rather than in a commit message or a chat log.
- Items **F1–F13** come from an adversarial first pass over the repo as a
  stranger would use it: install from the release, then the CLI and MCP server in
  fresh modules outside the repo. Findings, evidence and resolution are in Done.
  The separate review document they came from was folded in here, so this is the
  only work list.

---

## Open

Nothing open.

---

## Done

- **O1** — `mark3labs/mcp-go` v0.54.1 → v1.2.1; server now speaks MCP
  `2026-07-28` and negotiates down.
- **O2** — Every tool declares an `outputSchema`; check tools return
  `{"diagnostics": [...]}`; server-side output validation enabled.
- **O3** — golangci-lint pin collapsed into `config.GolangciLintVersion` with a
  test that fails on divergence.
- **O4** — CI actions bumped (`checkout@v7`, `setup-go@v7`,
  `golangci-lint-action@v9`, `goreleaser-action@v7`); Dependabot tracks
  `github-actions`.
- **O5** — README badge, Go prerequisite, and all live output-contract
  descriptions updated to the envelope (R1–R6).
- **O6** — All three tools pinned via `config.DefaultVersion`; no `latest` left
  in config, Dockerfile, docs, or example configs.
- **O7** — Historical docs banner-labelled and indexed.
- **O9** — Timeout classification fixed for the Unix path: `ExecRunner`
  reattaches the context error on signal-kill, so a real timeout reports
  `timed out after <d>`. The remaining Windows gap was closed by T3, which
  replaced that platform-shaped inference with reporting both facts.
- **O10** — `docs/architecture.html` file map corrected; issue template now
  names `mcp-server-go-quality --version`.
- **T11** — govulncheck and nilaway have no severity concept, and golangci-lint v2
  reports one only when the repository config defines severity rules. Because
  `severity` is `omitempty`, an unlabelled finding vanished from the JSON entirely
  and dropped out of any severity-filtered view. Measured on the sample project: a
  74-diagnostic sweep had **74/74 with no severity**, and the recipe documented in
  `docs/agents/AGENTS.md` bucketed **42 distinct CVEs plus a nil-panic into `info`** —
  a clean bill of health from a repo full of reachable vulnerabilities.

  Fixed by normalising at parse time: `diagnostic.DefaultSeverity` (`"error"`) plus
  `SeverityOrDefault`, applied in all three checkers. Error diagnostics are
  explicitly unaffected — they carry `error` and no severity, so a consumer can key
  "tool failed" off one field without confusing it with a finding.

  Guards: `TestSeverityOrDefault` pins the default at `"error"` so lowering it is a
  deliberate act; `TestParseGolangciLintEmptySeverityDefaults` covers the real-world
  empty case; the govulncheck and nilaway assertions were flipped from "should be
  empty" (which pinned the bug) to "must be `error`". Verified by reverting the fix:
  all three fail.

  Residual, not done: severity remains flat, so `--fail-on=warning` behaves like
  `--fail-on=error`. Gradation would mean a level above `error`, which is a contract
  change and was deliberately deferred — `--tools=govulncheck` already covers the
  "gate on security only" case without touching the schema.
- **T6** — Resolved by bumping the declared toolchain to `go 1.26.0`, which is what
  the pinned nilaway needs to build. That removes the silent `GOTOOLCHAIN=auto`
  download mid-`install_tools`: the toolchain is declared, so CI and builds fetch it
  once, up front. All four workflows, the example workflow and the README
  prerequisite now agree on 1.26. Verified by building and running the full suite
  under 1.26.0.
- **T9** — A file that carries a pin is now known to exactly one place:
  `config.PinLocations`, which maps each file to the tools pinned in it.
  `cmd/bump-tool-pins` reads it instead of its own copy, and both consistency
  tests derive their file lists from it. Previously the bump tool covered 10 files
  while the tests covered 7 and 9, so `Makefile` and `lint.yml` were bumped but
  never all-tool-checked, and `config.go` — the source of truth — was in neither
  test list. Verified by round trip: staling every location and running
  `make bump-tool-pins` returns the tree byte-identical to clean.
- **Review** — 12 findings from an independent adversarial review closed
  (R1–R12); test gaps for the install-tools payload, the `native` schema
  widening, and assertion strength all addressed.
- **T2** — Historical docs resolved. The six `docs/superpowers/plans/*.md`
  implementation plans (~4,500 lines) were deleted: they narrated how the code
  was built, are fully superseded by the code, and are preserved in git history.
  `specs/` (design rationale), `tools-research.md` (tool selection), and
  `architecture.html` were kept and remain banner-labelled. Anything still
  unfinished in the deleted plans was extracted into T5 and T7.
- **T3** — Timeout reporting is now platform-independent. The report states the
  context state **and** the process exit code and stderr, discarding neither, and
  no branch depends on the exit code's sign, `runtime.GOOS`, or a `shortcut:`
  marker. Delivered in `2c23f27`.

  The first attempt failed and is worth reading: `.mypi/task-t3-t4-hardening/`
  produced no code and has the post-mortem in its `run-details/final.md`. Its
  real contribution was settling that **no timing or signal heuristic can prove
  the context caused a termination** — `Process.Kill() == nil` means the signal
  was accepted, not that it caused the exit (`os/exec/exec.go:827-833`), and a
  grace period supplies no causal marker at its boundary. So the requirement was
  rewritten from *infer the cause* to *report both facts*, which is satisfiable
  and fixes the user-visible defect. The successful run is
  `.mypi/task-hardening-02/` (`exit_code: 0`, 13/13 criteria, $0.30).

  The adversary in that run earned its budget: it found that a `runner.go`-only
  fix could not satisfy the DoD because both formatters short-circuit and discard
  the process detail, which is why the fix spans `internal/runner/runner.go`,
  `cmd/mcp-server-go-quality/main.go`, and `internal/checkers/orchestrator.go`.
  It also caught that a blind `errors.Join` makes `errors.Is(err,
  DeadlineExceeded)` true for a genuine exit at the deadline, so a real tool
  failure could be relabelled a timeout. That trade is recorded and accepted, not
  overlooked.
- **T4** — Pins now have a drift check and a single-command bump. Delivered in
  `2c23f27`.
  `.github/workflows/pin-drift.yml` runs weekly and fails when any pin is behind
  the version resolved independently through the module proxy; it opens no PR and
  needs no secret beyond the default token. The detector is a Go test
  (`TestPinnedVersionsAreCurrent`) that skips under `-short`, so the ordinary
  suite stays hermetic. `make bump-tool-pins` rewrites every pin location at
  once. Verified by round trip: staling all ten locations and running the one
  command returns the tree byte-identical to clean. The nilaway noise problem that remained is
  fixed as T8.
- **T1** — First tagged release. The repo had no tags and nothing on the module
  proxy, so `@latest` resolved to a pseudo-version of `main` HEAD and the
  goreleaser workflow had never fired. `v0.1.0` was tagged and published: 7
  assets, one archive per platform, each containing both binaries, both verified
  working. Closed in `1a70f77`, which deleted this entry rather than moving it
  (the same failure mode as T5); restored here.
- **T5** — Coverage gate. Decision: the floor stays at **60**, with the reasoning
  pinned as a comment in `.github/workflows/test.yml` so it is not raised blindly
  later. The CLI's happy path only executes when the three real checkers run,
  which `go test -short` skips, so that code cannot be counted in the coverage
  job; raising the floor without counting integration runs turns CI red for a
  reason that is not a bug. No badge: a live one needs a hosted service and a
  static one would be a hardcoded number that lies. Closed in `7fa6d3a`.

  This entry was **lost** by that commit. The move to Done anchored the insertion
  on text that did not exist, so the replace was a silent no-op and the section
  was deleted instead of moved. Restored here.
- **T7** — Listing and example polish. The listing icon is wired
  (`assets/logo.jpeg` is the `glama.json` icon and the README header uses
  `assets/project-image-02.jpeg`), `examples/claude-code/` was added covering
  both entrypoints, and `examples/github-actions/quality.yml` now calls
  `go-quality --json` instead of reading `.result.content[0].text` — simpler than
  piping JSON-RPC through stdin, and it exercises the same envelope.
- **T8** — The weekly pin-drift job would cry wolf on nilaway. nilaway publishes
  no tags, so its pin is a commit pseudo-version and `@latest` resolves to a
  different value on every upstream commit, turning the job red most weeks for a
  reason nobody can act on. Decided: pseudo-version modules are reported as
  informational and do not fail the job; tagged modules (golangci-lint,
  govulncheck) keep the failing direction. `isPseudoVersion` matches the 14-digit
  timestamp run; verified in both directions. Closed in `7f1dfa3`.

### Fresh-user review (F1–F13)

From a first pass over the repo as a stranger would use it. Impact order. All
closed except F5 (above).

- **F1 (critical)** — Unreachable CVEs were reported as `error` findings.
  `parseGovulncheckOutput` emitted all three of govulncheck's trace levels
  (symbol = your code calls it, package = imported but never called, module =
  required but never called) and only symbol means the code is affected. A
  hello-world module reported **56 findings and exit 1** while govulncheck itself
  reported "affected by 0 vulnerabilities" — 55 finding objects: 51 module-level,
  4 package-level, 0 symbol-level. So any project whose dependency graph held a
  vulnerable version failed `--fail-on=error` and `--fail-on=any` whether or not
  the code could reach it, and the `error` label put an uncalled advisory above a
  genuine lint warning. Fixed in `c26e1ee`: `isReachableTrace` keeps a finding
  only if its trace carries a function frame, keyed on that frame rather than on
  a missing position (which merely coincides with the bug). The test that pinned
  the old behaviour (`want 2 diagnostics`, "file should be empty for module-level
  finding") was inverted, and the fix is verified on the **released** `v0.1.1`
  binary: 1 finding where there were 56, and a clean project exits 0 where it
  exited 1. The dependency-graph advisories are deliberately out of the result
  set; run `govulncheck` directly for the full list.
- **F2** — Closed by F1 with no doc edit. `docs/agents/reference.md:139`
  ("govulncheck only reports vulnerabilities that are reachable via your call
  graph") and `README.md:63` described behaviour the code did not have; fixing F1
  made both sentences true. `docs/agents/AGENTS.md:17` ("in your dependency
  graph") was left as written — vague, not false.
- **F3** — `README.md:135`'s agent install command used `@latest`, which
  `RELEASING.md` forbids for install examples and which both `examples/` READMEs
  pin. Set to `@v0.1.0` in `34f8e9f`, then moved to `@v0.1.1` by the release
  commit `b3128a0`.
- **F4** — The README's prerequisite claimed Go 1.26+ on `PATH`. It is 1.21+:
  `GOTOOLCHAIN` reads the `go` directive (`1.26.0`) and fetches that toolchain,
  which is how the documented install succeeded on go1.25.9. Fixed in `34f8e9f`.
  The 0.1.0 CHANGELOG note repeated the same error — see F13.
- **F6** — The MCP server silently swallowed a positional path. The flag package
  stops parsing at the first non-flag argument, so `mcp-server-go-quality
  ~/myproject` discarded the path *and* every flag after it (including
  `--version`) and then served the working directory: results for the wrong
  project, no warning, exit 0. Fixed in `34f8e9f` by rejecting positional
  arguments with a message naming `--config`, deliberately not by adding path
  support — the server takes no path and the README documents none. Regression
  test `TestCheckArgsRejectsPositionalArguments`.
- **F7** — "Tools bundled" credited golangci-lint with `gosec` unconditionally.
  The linter set follows the *target* project's `.golangci.yml`, and gosec is not
  on by default: a no-config hello-world run produced only `unused`. Qualified in
  `34f8e9f`.
- **F8** — "auto-install into `$GOBIN`" omitted the fallback. `go env GOBIN` is
  usually empty and `ResolveGoBinDir` then uses `$GOPATH/bin`. Corrected in
  `34f8e9f`.
- **F9** — Module-level govulncheck findings rendered as `:0` with an empty file,
  which reads like a rendering bug. Moot after F1: none survive the filter.
- **F10** — The `MCP` badge had an empty link target (`[![MCP](…)]()`). It now
  points at `docs/mcp-spec-currency.md`. Fixed in `34f8e9f`.
- **F11** — The README config example carried per-tool `version` lines,
  inconsistent with `examples/config/*.yaml`, which inherit the pin. Removed, and
  the prose now says omitting `version` inherits the pinned default.
  `PinLocations` needed no change: the tools table still carries all three pins.
  Fixed in `34f8e9f`.
- **F12** — `docs/mcp-spec-currency.md:304` said `v0.1.0` was "unreleased per
  CHANGELOG"; it shipped 2026-10-13. Fixed in `b3128a0`. Found while grepping
  every `v0.1.0` reference before the release bump, not in the original pass.
- **F13** — The 0.1.0 CHANGELOG note repeated F4's error ("Requires Go 1.26 on
  `PATH`"). Fixed in `b3128a0`. Editing a historical entry is unusual, but this
  was a wrong *requirement statement*, not a record of a change, and it
  contradicted the README correction.
- **F5** — The README's demo GIF was stale in four ways, not one. It predated the
  CLI by four and a half months (`demo.gif` 2026-06-03, `cmd/go-quality/`
  2026-10-13); it never showed the CLI at all; it demonstrated the exact friction
  the CLI was built to remove (a viewer hand-writing `{"jsonrpc":…}` and piping
  it through `jq`); and its output carried **no `severity` key** at all, the
  pre-T11 schema — so the demo advertised the bug T11 fixed. It also sat high in
  the README, right after the badges.

  Fixed by making the demo reproducible instead of re-recording it by hand.
  `demo.tape` records both entrypoints: `go-quality` text output, `go-quality
  --json | jq` showing the severity field, then the MCP server answering
  `tools/list`. `make demo` builds the binaries onto PATH and runs `vhs`. The
  tape is now the source of truth, so the next schema change is a tape edit
  rather than a re-record.

  That absence was the root cause: the old GIF had no source anywhere, in the
  repo or its history (`git log --all -- "*.tape"` is empty), which is why it
  went stale twice over without anyone noticing.

Checked and found correct, recorded so nobody re-checks: path normalisation is
right — the `../../../../tmp/…` seen during the review is a macOS `/tmp` →
`/private/tmp` symlink artifact, and the same project outside `/tmp` reports
`main.go`. The documented install command works end to end; the toolchain
auto-switch works (F4 was the doc, not the mechanism); a non-Go directory and a
nonexistent path both exit 2 with a clear message; stdout carries only the payload
while progress goes to stderr; `project_path` is the correct parameter name and
the MCP server does accept `--config`.

---

## Standing notes

Not work. Recorded because each changes how a future task run should be set up.

- **T10 — `golangci-lint` is outside the read-only verification vocabulary.** The
  harness matches `shell_allowlist` entries by exact string, so
  `golangci-lint run ./...` was not granted to the read-only peers and the lint
  criterion in that run was attested from the implementer's own output rather
  than independently reproduced (disclosed in
  `.mypi/task-hardening-02/run-details/final.md`). If a future run needs lint
  independently attested, a task-run config (`.mypi/*/session.yaml`) must put
  `golangci-lint run ./...` in the read-only peer's `shell_allowlist`, and the
  reviewer brief must tell the peer it is there.
