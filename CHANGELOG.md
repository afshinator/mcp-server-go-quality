# Changelog

All notable changes to mcp-server-go-quality are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] — 2026-10-13

Initial release.

Two entrypoints over one engine, so the normalised result is reachable from a
terminal, a script, or an MCP client:

- **`go-quality`** — a plain CLI. Text on stdout by default, `--json` for the
  envelope, `--tools` to narrow the sweep, `--fail-on=any|error|warning|none` to
  set the gate, `--install-only` to pre-warm tools in CI. Exit codes: `0` clean,
  `1` findings, `2` the run did not complete.
- **`mcp-server-go-quality`** — an MCP server for Claude Code, Codex, OpenCode
  and other agents. Five tools: `run_code_checks`, `run_lint`, `run_vuln_check`,
  `run_nil_check`, `install_tools`. Speaks protocol `2026-07-28`.

Both run the same three checkers in parallel under independent per-tool
timeouts, resolve the same `.go-quality.yaml`, and auto-install the same pinned
tool versions. `--json` is byte-identical to the MCP tools' `structuredContent`,
so one `jq '.diagnostics'` query works against either.

### Added

- Unified `Diagnostic[]` output schema with file:line:column navigation, a
  canonical `error` field per diagnostic, and panic recovery. The envelope is
  `{"diagnostics": [...]}`, an object, so `structuredContent` is spec-compliant;
  every tool also declares an `outputSchema` and the MCP server validates
  results against it, so a result that drifts from the contract fails loudly
  rather than reaching the client.
- **A finding always carries a `severity`.** govulncheck and nilaway have no
  severity concept, and golangci-lint reports one only when the repository
  config defines severity rules. Because `severity` is `omitempty`, an
  unlabelled finding vanished from the JSON entirely and dropped out of any
  severity-filtered view — which sorted a reachable CVE below a style warning.
  Findings whose tool reports no severity are labelled `error`. Error
  diagnostics are unaffected: they carry `error` and no severity, so a consumer
  keys "tool failed" off one field without confusing it with a finding.
- **Deterministic diagnostic ordering.** Sorting by `(file, line)` alone left
  64 of 72 govulncheck findings tied at `file: "" / line: 0` in tool-emission
  order, because `sort.Slice` is not stable. The comparator now breaks ties on
  column, tool, severity, message and raw payload, making output reproducible
  run to run.
- Tool auto-install with version pinning: golangci-lint v2.14.0, govulncheck
  v1.9.0, nilaway v0.0.0-20260918162853-acb8859b9031. `install_tools` and
  `--install-only` pre-warm them; a scheduled workflow fails when a pin falls
  behind a published release and `make bump-tool-pins` updates every location
  at once.
- `.go-quality.yaml` configuration with reserved flag validation.
- Two-pass project root discovery (`go.work` → `go.mod`) and multi-module
  workspace support with automatic nilaway `-include-pkgs` resolution.
- Graceful error handling: per-tool error diagnostics, context cancellation
  detection, govulncheck vuln DB lock retry.
- Agent documentation: `docs/agents/AGENTS.md` and `docs/agents/reference.md`.
- Comprehensive test suite with unit and integration tests.

### Notes

- Built on `mark3labs/mcp-go` v1.2.1, which speaks protocol `2026-07-28`; older
  clients negotiate down and keep working.
- Requires Go 1.26 on `PATH`; the toolchain directive in `go.mod` fetches the
  exact version automatically.
