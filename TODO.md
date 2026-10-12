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

### T3 — Windows timeout reporting
`internal/runner/runner.go` reattaches the context error only when a subprocess
was signal-killed (negative exit code). That is Unix-shaped, so on Windows a
genuine timeout still surfaces as an exit error rather than
`timed out after <d>`. Marked inline with a `shortcut:` comment.

The fix must not depend on the platform at all (a `runtime.GOOS` branch or an
exit-code sign test just moves the problem). Go 1.20+ exposes `cmd.Cancel`, a
hook invoked when the context is done — one candidate worth evaluating, not a
conclusion.

Config staged at `.mypi/task-t3-windows-timeout/session.yaml`.

### T4 — Pinned tool versions have no automated bump path
Dependabot covers `go.mod` and GitHub Actions. It does **not** cover the three
tool pins in `internal/config/config.go` (`GolangciLintVersion`,
`GovulncheckVersion`, `NilawayVersion`) or their copies in the `Dockerfile`,
README, agent docs, and `examples/config/*.yaml`. Those go stale silently, and a
stale `govulncheck` means weaker vulnerability detection.

A bump currently means hand-editing seven files, so any automation has to move
all of them together or the consistency tests fail.

Config staged at `.mypi/task-t4-pin-drift-automation/session.yaml`.

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
- `examples/github-actions/quality.yml` reads `.result.content[0].text`. That
  still works — the text block carries the same JSON as `structuredContent` — but
  `.result.structuredContent.diagnostics` is the modern path and reads better as
  an example.

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
  `timed out after <d>`. The Windows gap is T3.
- **O10** — `docs/architecture.html` file map corrected; issue template now
  names `mcp-server-go-quality --version`.
- **Review** — 12 findings from an independent adversarial review closed
  (R1–R12); test gaps for the install-tools payload, the `native` schema
  widening, and assertion strength all addressed.
- **T2** — Historical docs resolved. The six `docs/superpowers/plans/*.md`
  implementation plans (~4,500 lines) were deleted: they narrated how the code
  was built, are fully superseded by the code, and are preserved in git history.
  `specs/` (design rationale), `tools-research.md` (tool selection), and
  `architecture.html` were kept and remain banner-labelled. Anything still
  unfinished in the deleted plans was extracted into T5 and T7.
