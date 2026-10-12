# TODO

The live backlog. **This is the file to read to find out what is next.**

- `docs/mcp-spec-currency.md` is the *audit record* — the evidence and reasoning
  behind the MCP migration. It is history, not a work list.
- This file is the *work list*. Keep it short: open items only, with enough
  detail that someone can act without re-deriving the context.
- Nothing enforces this file. If you close an item, delete it from Open and add
  one line to Done. If you find new work, add it here rather than in a commit
  message or a chat log.

---

## Open

### T1 — Cut the first tagged release
The repo has **no tags and nothing published to the module proxy**, so
`go install ...@latest` currently resolves to a pseudo-version of `main` HEAD
rather than a stable release. Two consequences:

1. There is no stable version for consumers to pin, and the `goreleaser`
   release workflow (`on: push: tags: v*`) has never fired.
2. The breaking output change is already live for anyone installing `@latest`,
   because `@latest` tracks HEAD. There is nothing to "ship first" — but there
   is also no way for a consumer to stay on the old shape.

A first release should bump `internal/version/version.go` (`Value` is still
`v0.1.0`), turn the CHANGELOG's unreleased entry into a dated `0.2.0` (major
bump per semver: the output contract changed), and tag `v0.2.0`.

### T2 — Decide the fate of the historical docs
`docs/superpowers/**` and `docs/tools-research.md` are now banner-labelled as
historical records and cross-linked from `docs/superpowers/README.md` and
`CONTRIBUTING.md`. They still contain obsolete protocol (`2025-03-26`) and pin
(`v2.11.4`) values. Either leave them permanently (cheap, provenance) or delete
them. Deliberately not deleted — that is a call for the maintainer.

### T3 — Windows timeout reporting
`internal/runner/runner.go` reattaches the context error only when a subprocess
was signal-killed (negative exit code). That is Unix-shaped, so on Windows a
genuine timeout still surfaces as an exit error rather than
`timed out after <d>`. Marked inline with a `shortcut:` comment.
*Upgrade when: Windows timeout reporting matters.*

### T4 — Pinned tool versions have no automated bump path
Dependabot covers `go.mod` and GitHub Actions. It does **not** cover the three
tool pins in `internal/config/config.go` (`GolangciLintVersion`,
`GovulncheckVersion`, `NilawayVersion`) or their copies in the `Dockerfile`.
Those go stale silently, and a stale `govulncheck` means weaker vulnerability
detection. Either add a scheduled reminder or a CI check that reports how far
behind each pin is.

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
- **O6** — `govulncheck` and `nilaway` pinned via `config.DefaultVersion`; no
  `latest` left in config, Dockerfile, docs, or example configs.
- **O7** — Historical docs banner-labelled and indexing added (residual decision
  is T2).
- **O9** — Timeout classification fixed: `ExecRunner` reattaches the context
  error on signal-kill, so a real timeout reports `timed out after <d>`.
- **O10** — `docs/architecture.html` file map corrected; issue template now
  names `mcp-server-go-quality --version`.
- **Review** — 12 findings from an independent adversarial review closed
  (R1–R12); test gaps for the install-tools payload, the `native` schema
  widening, and assertion strength all addressed.
