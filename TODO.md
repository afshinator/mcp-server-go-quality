# TODO

## Remove before public release

- [ ] `CLAUDE.md` — Claude Code agent instructions with private vault paths.
- [ ] `.commandcode/taste/taste.md` — `.commandcode/*` is gitignored but this file was committed before the pattern was added. Needs `git rm --cached`.
- [ ] `docs/superpowers/specs/spec-v3-review1.md` — outdated spec review commentary.
- [ ] `docs/superpowers/plans/install-lock-double-check-sequence.md` — implementation scratch notes.

## Already handled — no action needed

- [x] `.mcp.json` — personal MCP config. Never tracked, `.mcp*` pattern in `.gitignore`.
- [x] `.superpowers/` — Already gitignored, no tracked files.
- [x] `.commandcode/` (except taste.md) — Gitignored, only the single tracked file above remains.
- [x] `.pi-lens/` — Added to `.gitignore` and removed from git tracking.
- [x] `bin/mcp-server-go-quality` — Already in `.gitignore`.
- [x] `coverage.out` and `*.test` — Already in `.gitignore`.
- [x] `/mcp-server-go-quality` (root binary) — Already in `.gitignore`.

## KEEP — these are public-facing

These stay in the repo:

- [x] `docs/agents/AGENTS.md` — agent contract for using this MCP server.
- [x] `docs/agents/reference.md` — error tables, remediation, troubleshooting.
- [x] `docs/superpowers/specs/spec-v3.md` — design spec. Useful context for contributors.
- [x] `docs/superpowers/plans/01-setup-and-types.md` through `05-orchestration-server.md` — implementation plans.
- [x] `docs/superpowers/plans/2026-05-26-adversarial-review-fixes.md` — review fixes log.
- [x] `docs/superpowers/plans/2026-05-26-repo-polish-and-discoverability.md` — polish plan.
- [x] `Makefile` — targets: `setup-dev`, `check`, `fmt-check`, plus standard build/test/lint.

## Done

- [x] **Branch protection on `main`** — now enabled (requires PR + passing status checks + blocks force pushes).
- [x] **PR #2** (dependabot mcp-go v0.54.0→v0.54.1) — merged.
- [x] **PR #3** (fix/ci-guardrails: pre-push hook, make targets, docs) — merged.
- [x] **`make setup-dev`** — works. Installs gofumpt, goimports, golangci-lint v2 (pinned), govulncheck, nilaway, lefthook; wires pre-commit and pre-push hooks.
- [x] **`make check`** — fmt → vet → lint → test, all passing.
- [x] **`make fmt-check`** — non-destructive formatting verification (gofumpt + goimports).
- [x] **Lefthook hooks** — pre-commit (gofumpt, go vet, incremental lint), pre-push (full lint + short tests).
- [x] **golangci-lint** — bumped from v1 to v2.12.2 to match `.golangci.yml` config.
- [x] **nilaway install path** — fixed from `github.com/uber-go/nilaway` to `go.uber.org/nilaway`.

## P2 — remaining

- [ ] **Terminal demo GIF**

  See instructions below.

- [ ] **Pre-flight CI sanity script (`scripts/check-ci-config.sh`)**

  Validates CI workflow configs against the project's declared constraints
  so version mismatches get caught before push, not after.

  **What it checks:**

  - **Go version alignment** — extracts `go-version` entries from
    `.github/workflows/test.yml` matrix (and any `setup-go` steps across
    all workflows), parses the `go` directive from `go.mod`, and verifies
    every CI Go version is >= the go.mod requirement. Uses `sort -V` for
    semantic version comparison.

  - **golangci-lint action vs config version** — parses the action ref
    (e.g. `golangci/golangci-lint-action@v6`) from `lint.yml` and compares
    with the `version:` field in `.golangci.yml`. If the config uses
    `version: "2"`, the action must be `@v7` or later. Can also check the
    `version` parameter of the action — if config says v2, the version
    param should be `v2.x`, not `v1.x`.

  - **Setup Go version vs go.mod** — verifies that `setup-go` `go-version`
    values across all workflow files are compatible with `go.mod`.

  - **(Future)** Checks that required secrets/tokens are referenced
    consistently, that `actions/checkout` is `@v4`, that `setup-go` is
    `@v5`, etc.

  **Exit code:** 0 = all checks pass; 1 = at least one failure with
  `::error::` messages explaining which file and field is wrong.

  **When to run:**
    - Lefthook pre-commit hook (runs when any `.github/workflows/*.yml`
      or `.golangci.yml` or `go.mod` changes)
    - CI itself as a first step (fast-fail before spending minutes on
      builds)
    - Manually: `scripts/check-ci-config.sh`

  **How to add to lefthook:**
    ```yaml
    check-ci-config:
      glob: ".github/workflows/*.{yml,yaml}"
      run: scripts/check-ci-config.sh
    ```

  **Design notes:**
    - Pure bash, no external dependencies beyond `yq` if YAML parsing is
      needed; otherwise `grep`/`awk`/`sort` are sufficient for the basic
      checks listed above.
    - If YAML depth parsing is needed (e.g. reliably extracting the
      `matrix.go-version` array), pull in `yq` (already available in CI
      runners via `go install github.com/mikefarah/yq/v4@latest`).

### How to generate the terminal demo GIF

1. Install asciinema and agg:
   ```bash
   npm install -g asciinema
   # agg: cargo install --git https://github.com/asciinema/agg
   ```

2. Record:
   ```bash
   asciinema rec demo.cast --cols 100 --rows 24
   ```

3. In the session, type slowly:
   ```bash
   mcp-server-go-quality --version
   echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | mcp-server-go-quality 2>/dev/null | jq .
   cd testdata/sample_project
   echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"run_lint","arguments":{"project_path":"."}}}' | mcp-server-go-quality 2>/dev/null | jq .
   exit
   ```

4. Ctrl-D to stop. Convert:
   ```bash
   agg demo.cast demo.gif --font-size 14 --theme monokai
   ```

5. Place `demo.gif` in repo root and reference in README:
   ```markdown
   ![demo](demo.gif)
   ```
