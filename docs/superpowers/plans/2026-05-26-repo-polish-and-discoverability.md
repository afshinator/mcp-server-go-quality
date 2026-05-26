# Repo Polish & Discoverability Plan

> **Goal:** Bring the repo up to top-tier Go MCP server standards for community adoption, listing site eligibility, and overall polish. Target: glama.ai, smithery.ai, mcp.so listings.

**Status:** Code is production-quality. Repo infrastructure is minimal — no CI, no LICENSE file, no listing configs.

**Pexels comparison:** pexels has `glama.json`, `CHANGELOG.md`, `LICENSE`, `examples/`, logo image, `.env.example`, `implementation-summary.md`. It also lacks CI, CONTRIBUTING.md, SECURITY.md, Docker, CODEOWNERS.

---

## Critical Gaps (P0 — required for any listing site)

### 1. Add `LICENSE` file
- [ ] Create `LICENSE` at repo root with MIT text
- README already says "MIT License" — just needs the file

### 2. Add GitHub repo topics
Set on GitHub repo settings page (not a file change):
- `mcp`, `mcp-server`, `go`, `golang`, `code-quality`, `linting`, `security`, `vulnerability-scanner`, `devtools`

### 3. Add logo
- [ ] Create `assets/logo.png` (512x512 recommended by glama)
- [ ] Reference in README: replace `<!-- repo image -->` with actual `<img>`
- Pexels equivalent: `mcp-pexels-image.png` at repo root

### 4. Add `glama.json` (pexels has this — critical for glama.ai listing)
```json
{
  "name": "mcp-server-go-quality",
  "description": "One MCP server for Go code quality — golangci-lint, govulncheck, and nilaway with unified Diagnostic[] output, parallel execution, and auto-install.",
  "keywords": ["go", "golang", "linting", "security", "vulnerability", "code-quality"],
  "license": "MIT",
  "author": "afshinator",
  "homepage": "https://github.com/afshinator/mcp-server-go-quality",
  "repository": "https://github.com/afshinator/mcp-server-go-quality"
}
```

### 5. GitHub Actions CI/CD
Create `.github/workflows/` with three workflows:

**`test.yml`** — Run on push/PR to `main` and `feat/*`:
- [ ] Matrix: `go-version: ['1.24', '1.25']`
- [ ] `go test -short -race -coverprofile=coverage.out ./...`
- [ ] Upload coverage to Codecov (or `go tool cover -func` summary)
- [ ] `go vet ./...`

**`lint.yml`** — Run on push/PR:
- [ ] `golangci-lint run ./...`
- [ ] `gofumpt -d .` (check mode, fail if diffs)
- [ ] Reuse `.golangci.yml` already in repo

**`release.yml`** — Trigger on tag push `v*`:
- [ ] `goreleaser release --clean` (see `.goreleaser.yml` below)

### 6. `.goreleaser.yml`
- [ ] Standard Go binary release config:
  - `builds`: linux/darwin/windows × amd64/arm64, `CGO_ENABLED=0`
  - `archives`: tar.gz format
  - `checksum`: enabled
  - `changelog`: auto from commits
  - `main`: `./cmd/mcp-server-go-quality/`

### 7. Add `CODEOWNERS`
- [ ] `/.github/CODEOWNERS` — single entry: `* @afshinator`

---

## Important Polish (P1 — expected by community, pexels has some of these)

### 8. Add `SECURITY.md`
- [ ] Standard vulnerability reporting policy
- Link to GitHub Security Advisories or email

### 9. Add `CHANGELOG.md` (pexels has this)
- [ ] Create with existing release history:
  - v0.1.0 — Initial release: 5 MCP tools, parallel checker execution, auto-install, yaml config
- Follow Keep a Changelog format (pexels does)
- Will be auto-maintained by goreleaser changelog generation afterward

### 10. Add `.github/` templates
- [ ] `ISSUE_TEMPLATE/bug_report.md` — structured bug report
- [ ] `ISSUE_TEMPLATE/feature_request.md` — feature request template
- [ ] `PULL_REQUEST_TEMPLATE.md` — checklist: tests pass, lint clean, docs updated

### 11. Add `CONTRIBUTING.md`
- [ ] Development setup: `git clone`, `go mod download`
- [ ] Test commands: `make test`, `make test-all`
- [ ] TDD expectation: every PR needs tests
- [ ] PR checklist: run lint, vet, fmt before submitting
- [ ] Link to `docs/superpowers/` for architecture overview

### 12. Real README badges
Replace placeholder `#` links with real services:
- [ ] GitHub Actions status badge (`test.yml`)
- [ ] [Go Report Card](https://goreportcard.com/) badge
- [ ] [pkg.go.dev](https://pkg.go.dev/) badge
- [ ] Code coverage badge (Codecov or `go tool cover` shield)
- [ ] Keep MCP version and license badges

### 13. `smithery.yaml`
```yaml
name: mcp-server-go-quality
description: Go code quality MCP server — golangci-lint, govulncheck, nilaway
command: go run github.com/afshinator/mcp-server-go-quality/cmd/mcp-server-go-quality@latest
homepage: https://github.com/afshinator/mcp-server-go-quality
author: afshinator
icon: assets/logo.png
categories:
  - code-quality
  - security
  - developer-tools
tags:
  - go
  - golang
  - linting
  - vulnerability
```

### 14. `package.json` (for npm-based aggregators — pexels has a real one)
Minimal metadata-only package (not an npm package — just for discovery):
```json
{
  "name": "mcp-server-go-quality",
  "version": "0.1.0",
  "description": "Go code quality MCP server — golangci-lint, govulncheck, nilaway",
  "keywords": ["mcp", "mcp-server", "go", "code-quality", "linting", "security"],
  "repository": "github:afshinator/mcp-server-go-quality",
  "license": "MIT"
}
```

### 15. Glama.ai submission checklist
- [ ] Logo (512×512 PNG) — same as #3
- [ ] `glama.json` — same as #4
- [ ] Clear one-liner description
- [ ] Install commands for: Claude Desktop, Claude Code, generic MCP client
- [ ] All 5 tools documented with input/output
- [ ] `go.mod` present (already)
- [ ] LICENSE file present
- [ ] Active development signals (recent commits, CI passing)

---

## Nice-to-Have (P2 — top-tier polish)

### 16. Examples directory (pexels has: `examples/client.ts`)
- [ ] `examples/claude-code/` — how to register and use in Claude Code
- [ ] `examples/github-actions/` — CI pipeline using this server
- [ ] `examples/config/` — `.go-quality.yaml` recipes for different scenarios
- [ ] `examples/client.go` — similar to pexels' `client.ts`

### 17. `.env.example` (pexels has this)
- [ ] Not critical for this server (no API keys), but signals completeness

### 18. Pre-commit hooks
- [ ] `.pre-commit-config.yaml` or `lefthook.yml`
- Runs: `gofumpt`, `golangci-lint`, `go vet`

### 19. Dependabot
- [ ] `.github/dependabot.yml` — Go modules updates, monthly

### 20. Code coverage threshold
- [ ] Enforce min coverage in CI (80%+ is realistic given current test suite)

### 21. Graphical demo
- [ ] Terminal recording: `install_tools` → `run_code_checks` → diagnostic output
- [ ] Use `asciinema` or a simple GIF in README

---

## What pexels has that go-quality doesn't

| Element | pexels | go-quality | Priority |
|---|---|---|---|
| `glama.json` | ✅ | ❌ | P0 |
| `CHANGELOG.md` | ✅ Keep a Changelog | ❌ | P1 |
| `LICENSE` file | ✅ MIT | ❌ | P0 |
| `examples/` | ✅ client.ts | ❌ | P2 |
| Logo image | ✅ mcp-pexels-image.png | ❌ | P0 |
| `.env.example` | ✅ | ❌ | P2 |
| `implementation-summary.md` | ✅ | ❌ | P2 |
| `.mcp.json` (project-level) | ✅ firecrawl+eslint+stylelint | ✅ soon after our adds | ✅ |
| CI/CD (.github/workflows) | ❌ | ❌ | P0 |
| CONTRIBUTING.md | ❌ | ❌ | P1 |
| SECURITY.md | ❌ | ❌ | P1 |
| CODEOWNERS | ❌ | ❌ | P0 |

---

## Verification

After all changes:
- [ ] All CI workflows pass on push
- [ ] `goreleaser check` succeeds locally
- [ ] Go Report Card shows A+ grade
- [ ] pkg.go.dev renders docs correctly
- [ ] glama.ai submission form is fully populated
- [ ] smithery.ai accepts the `smithery.yaml`
- [ ] GitHub repo shows all topics and badges
- [ ] README renders with logo and real CI badges
- [ ] `go test -short -race ./...` still passes (no regressions)
