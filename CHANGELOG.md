# Changelog

All notable changes to mcp-server-go-quality are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] — Unreleased

### Added

- Five MCP tools: `run_code_checks`, `run_lint`, `run_vuln_check`, `run_nil_check`, `install_tools`
- Parallel checker execution with independent per-tool timeouts and panic recovery
- Unified `Diagnostic[]` output schema with file:line:column navigation
- Auto-install with version pinning (golangci-lint v2.11.4, govulncheck latest, nilaway latest)
- `.go-quality.yaml` configuration with reserved flag validation
- Two-pass project root discovery (`go.work` → `go.mod`)
- Multi-module workspace support with automatic nilaway `-include-pkgs` resolution
- Pre-flight tool availability check before every run
- Graceful error handling: per-tool error diagnostics, context cancellation detection, govulncheck vuln DB lock retry
- Comprehensive test suite with unit and integration tests
- Agent documentation: `docs/agents/AGENTS.md` and `docs/agents/reference.md`
