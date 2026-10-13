# Agent instructions

Copy this file into the root of a Go project (as `AGENTS.md`, or `CLAUDE.md`
if your harness only reads that) so the agent you are using knows to run the
go-quality checks and how to act on them.

## Code quality checks

This project is checked by the `go-quality` MCP server, which runs
golangci-lint, govulncheck and nilaway in parallel and returns one
`Diagnostic[]` result.

- Call `install_tools` once per session; it installs the pinned checkers, so
  the other tools never fail for a missing binary.
- Before proposing changes, run `run_code_checks` on the repository root.
- For fast feedback while iterating, run `run_lint` (golangci-lint only).
- Treat any finding with severity `error` as blocking: fix it before finishing.
- A finding with an `Error` field is a tool that failed to run, not a code
  problem. Report it to the user rather than guessing at a fix.
- `run_vuln_check` (govulncheck) reports reachable CVEs; `run_nil_check`
  (nilaway) reports nil-panic paths.

If the harness has shell access instead of MCP, the same checks are available
as a CLI: `go-quality` (text) or `go-quality --json` (the same envelope).
