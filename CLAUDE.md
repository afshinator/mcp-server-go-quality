# CLAUDE.md — Project: mcp-server-go-quality

Extends the global `/vault/AgentConfig/CLAUDE.md` — all global rules apply.
This file IS committed to the project git repo.

---

## Project Overview

**Name:** mcp-server-go-quality
**Purpose:** MCP server wrapping Go code quality tools (golangci-lint, govulncheck, nilaway) for AI agent consumption
**Stack:** Go 1.25 (not yet initialized — greenfield)
**Status:** Design complete, implementation not started

## Key Research

See `docs/tools-research.md` for the full tool overlap analysis.
Key conclusion: `gocyclo` and `gocognit` are built into golangci-lint — no separate binaries needed.

---

## Key Vault Paths

| Path | Purpose |
|---|---|
| `/vault/Knowledge/<project-name>.md` | Durable facts about this project |
| `/vault/Workflows/` | Verified runbooks for this project |
| `/vault/Context/<project-name>.md` | Launcher-written startup context (read-only, auto-generated) |

---

## Design Docs

| File | Purpose |
|---|---|
| `docs/superpowers/specs/2026-05-23-go-quality-mcp-design.md` | Full design spec |
| `docs/superpowers/plans/2026-05-23-go-quality-mcp-plan.md` | 15-task TDD implementation plan |
| `docs/architecture.html` | Architecture diagram (preview: port 3011) |

---

## Running the Project

```bash
# Build
go build ./cmd/mcp-server-go-quality/

# Run tests (unit only)
go test -short ./...

# Run all tests (including integration)
go test -timeout 10m ./...

# Run MCP server (stdio)
go run ./cmd/mcp-server-go-quality/
```

---

## Implementation Plan

15 TDD tasks in `docs/superpowers/plans/2026-05-23-go-quality-mcp-plan.md`:

1. Go module + dir structure
2. Diagnostic type
3. Version package
4. Config loading (.go-quality.yaml)
5. Tool discovery & install
6. Command runner interface
7. golangci-lint handler + parser
8. govulncheck handler + NDJSON parser
9. nilaway handler + parser
10. runAll parallel orchestrator
11. MCP server entry point (5 tools)
12. testdata sample project
13. Integration tests
14. Makefile
15. Quality suite (lint, vet, format)

---

## Verification Commands

```bash
# Unit tests
go test -short ./...

# Full test suite
go test -timeout 10m ./...

# Lint (after golangci-lint is installed)
golangci-lint run ./...

# Vet
go vet ./...

# Format
gofumpt -w .
goimports -w .
```

---

## Code Style
- Always check returned errors immediately; do not use blank identifiers (`_`) for errors.
- Keep cognitive complexity low. Break nested blocks into independent, testable helpers.
- One file per tool parser in `internal/checkers/`.
- Prefer pure functions — no hidden state, predictable per-call output.
