# Changelog

All notable changes to mcp-server-go-quality are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] — Unreleased

### Changed

- **MCP protocol upgraded to `2026-07-28`** via `mark3labs/mcp-go` v0.54.1 → v1.2.1. Older
  clients negotiate down and keep working.
- **Breaking: check tools now return `{"diagnostics": [...]}`** instead of a
  top-level array. `structuredContent` and the text fallback carry the same JSON.
- Every tool declares an `outputSchema`; server-side output validation is enabled, so
  a result that drifts from the declared contract fails loudly.
- golangci-lint pin raised to v2.14.0 and consolidated into `config.GolangciLintVersion`,
  with a test that enforces agreement across the Makefile, CI, Dockerfile, examples, and docs.
- CI actions updated: `checkout` v7, `setup-go` v7, `golangci-lint-action` v9,
  `goreleaser-action` v7. Dependabot now tracks `github-actions`.
- **A finding always carries a `severity`.** govulncheck and nilaway have no severity
  concept, and golangci-lint reports one only when the repository config defines severity
  rules. Because `severity` is `omitempty`, an unlabelled finding vanished from the JSON
  entirely and dropped out of any severity-filtered view — which sorted a reachable CVE
  below a style warning. Findings whose tool reports no severity are now labelled `error`.
  Error diagnostics are unaffected: they carry `error` and no severity.
- **Diagnostic ordering is now total.** `RunAllChecks` sorted by `(file, line)` only,
  and `sort.Slice` is not stable — so the 64 of 72 govulncheck findings that share
  `file: "" / line: 0` came out in whatever order the tool happened to emit them.
  Output was not reproducible run to run. The comparator now breaks ties on column,
  tool, severity, message and raw payload, making the order deterministic.

### Added

- **A `go-quality` CLI**, sharing the whole engine with the MCP server: same checkers,
  same pinned tool versions, same root discovery, same envelope. `--json` is
  byte-identical to the MCP tools' `structuredContent`. Text output by default,
  `--tools` to narrow the sweep, `--fail-on=any|error|warning|none` to set the gate,
  `--install-only` to pre-warm tools in CI. stdout carries only the payload, so
  `go-quality --json | jq` is never corrupted by a first-run install. Exit codes:
  `0` clean, `1` findings, `2` the run did not complete — a checker that failed to run
  is unconditionally `2`, including under `--fail-on=none`.
- Five MCP tools: `run_code_checks`, `run_lint`, `run_vuln_check`, `run_nil_check`, `install_tools`
- Parallel checker execution with independent per-tool timeouts and panic recovery
- Unified `Diagnostic[]` output schema with file:line:column navigation
- Auto-install with version pinning (golangci-lint v2.14.0, govulncheck v1.9.0, nilaway v0.0.0-20260918162853-acb8859b9031)
- `.go-quality.yaml` configuration with reserved flag validation
- Two-pass project root discovery (`go.work` → `go.mod`)
- Multi-module workspace support with automatic nilaway `-include-pkgs` resolution
- Pre-flight tool availability check before every run
- Graceful error handling: per-tool error diagnostics, context cancellation detection, govulncheck vuln DB lock retry
- Comprehensive test suite with unit and integration tests
- Agent documentation: `docs/agents/AGENTS.md` and `docs/agents/reference.md`
