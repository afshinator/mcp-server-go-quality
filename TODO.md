# TODO

The live backlog. **This is the file to read to find out what is next.**

- `docs/mcp-spec-currency.md` is the *audit record* — the evidence and reasoning
  behind the MCP migration. It is history, not a work list.
- This file is the *work list*. Keep it short: open items only, with enough
  detail that someone can act without re-deriving the context.
- Nothing enforces this file. If you close an item, move it to Done. If you find
  new work, add it here rather than in a commit message or a chat log.

---

## Open

### T1 — Cut the first tagged release
The repo has **no tags and nothing published to the module proxy**, so
`go install ...@latest` resolves to a pseudo-version of `main` HEAD rather than a
stable release. Two consequences:

1. There is no stable version for consumers to pin, and the `goreleaser` release
   workflow (`on: push: tags: v*`) has never fired.
2. The breaking output change is already live for anyone installing `@latest`,
   because `@latest` tracks HEAD. There is nothing to "ship first" — but there is
   also no way for a consumer to stay on the old shape.

Recommended: tag **v0.1.0**. Nothing was ever released, so there is no prior
contract to bump away from, and `internal/version/version.go` already says
`v0.1.0`. Fold the CHANGELOG's `Changed` section into an initial-release framing
(it currently reads as a change to something nobody had), date it, run
`goreleaser check`, then tag.

### T5 — Coverage gate and badge
CI enforces a **60%** coverage floor and prints the number, but the README has no
coverage badge, so the gate is invisible. Current measured coverage is **66.9%**
(the new output-schema and runner tests raised it from ~65%). Raising the floor
toward 80% would need new tests first.

Decide: keep 60%, raise it, and whether to publish a badge.

### T6 — Declared Go version vs nilaway's requirement
`go.mod` declares `go 1.25.9`, but the pinned nilaway requires **Go >= 1.26.0** to
build, so `install_tools` silently triggers `GOTOOLCHAIN=auto` and downloads a
second toolchain. That works, but it is invisible and costs a download on first
use. Either bump the declared toolchain to 1.26 or document the switch.

### T7 — Listing and example polish
Carried over from the deleted repo-polish plan, plus one found in this session:

- `glama.json` has no icon and there is no 512x512 `assets/logo.png`; the README
  uses `project-image-01.png` directly.
- No `examples/claude-code/`, though the README documents
  `claude mcp add go-quality ...`.
- ~~`examples/github-actions/quality.yml` reads `.result.content[0].text`.~~
  Resolved with the CLI: the example now calls `go-quality --json`, which is
  simpler than piping JSON-RPC through stdin and exercises the same envelope.

### T9 — The pin-location list exists in three places
A file that carries a pin is known to
`internal/config/config_test.go` (the consistency test's file list),
`cmd/bump-tool-pins/main.go` (`pinFiles`), and the docs themselves. `make
bump-tool-pins` and the consistency test each read their own copy, so a **new**
file that gains a pin would be neither bumped nor guarded — the exact drift class
T4 set out to remove, one level up.

Either derive one list from a single source both consumers read, or have the
consistency test assert that `pinFiles` and its own list are identical. Note the
CLI work already moved the *module paths* to `toolname.ModulePath`, so this is
about the file list only.

### T10 — `golangci-lint` is outside the read-only verification vocabulary
Not a code defect, a task-config constraint worth remembering. The harness
matches `shell_allowlist` entries by exact string, so `golangci-lint run ./...`
was not granted to the read-only peers and the lint criterion in that run was
attested from the implementer's own output rather than independently reproduced
(disclosed in `.mypi/task-hardening-02/run-details/final.md`). Add it to the
vocabulary if a future run needs lint independently attested.

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
- **T12** — `--fail-on=none` semantics pinned. It suppresses the **findings** exit
  (1) only; a tool execution failure (`error` field present) forces exit 2
  unconditionally, including under `none`. Verified by
  `TestExitCodeToolFailureOutranksEverything`, which asserts the failure outranks
  every tier of `--fail-on`.
- **CLI** — `go-quality` shipped as a second entrypoint over the same engine.
  `cmd/mcp-server-go-quality` went 454 → 328 lines; the shared glue moved to
  `internal/preflight` (`BuildHandlers`, `EnsureToolsAvailable`, install types),
  `toolname.ModulePath` joined `InstallPath`, `formatHandlerError` was deleted in
  favour of the byte-identical `checkers.FormatCheckerError`, and
  `DiagnosticResponse` became `diagnostic.Response` so both entrypoints marshal one
  type. 128 lines of duplicated tests went with it.

  Verified rather than asserted:
  - `--json` output is **byte-identical** to the MCP `structuredContent` — proven
    against real output, two MCP runs and one CLI run, all
    `sha256 aa46c7137efc2d4b`, 36,282 bytes.
  - The `--fail-on` threshold is a comparison, not equality: reverting it to
    `rank == limit` fails `TestTripsWarningTripsOnErrorSeverity`, which is the case
    that would otherwise let a CVE through `--fail-on=warning`.
  - A bad flag exits 2 with stdout empty, so `--json | jq` cannot be fed garbage.
- **T8** — nilaway publishes no tags, so its pin is a commit pseudo-version and
  `go list -m ...@latest` resolves to a **different value on every upstream
  commit**. The scheduled `pin-drift` job therefore goes red most weeks for a
  reason nobody can act on, which is exactly how a real drift signal gets ignored.

  This was raised and accepted as a known risk during the run that added the
  workflow. Decide: exclude nilaway from the latest-comparison (it is pinned to a
  commit by intent), compare it only against an explicitly chosen upstream
  timestamp, or demote it to a non-failing report. Any option must keep the
  failing direction for golangci-lint and govulncheck, which do have releases.
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
  command returns the tree byte-identical to clean. The residual noise problem is
  T8.
