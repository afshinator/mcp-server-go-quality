# TODO

## Remove before public release

- [ ] `.mcp.json` — personal MCP config. Not for public repo.
- [ ] `CLAUDE.md` — Claude Code agent instructions with private paths.
- [ ] `.commandcode/` — local Command Code state.
- [ ] `.superpowers/` — brainstorm session state, pids, logs.
- [ ] `docs/superpowers/specs/spec-v3-review1.md` — outdated spec review commentary.
- [ ] `docs/superpowers/plans/install-lock-double-check-sequence.md` — implementation scratch notes.
- [ ] `bin/mcp-server-go-quality` — build artifact.
- [ ] `coverage.out` — build artifact.

## KEEP — these are public-facing

These stay in the repo:

- [x] `docs/agents/AGENTS.md` — agent contract for using this MCP server.
- [x] `docs/agents/reference.md` — error tables, remediation, troubleshooting.
- [x] `docs/superpowers/specs/spec-v3.md` — design spec. Useful context for contributors.
- [x] `docs/superpowers/plans/01-setup-and-types.md` through `05-orchestration-server.md` — implementation plans.
- [x] `docs/superpowers/plans/2026-05-26-adversarial-review-fixes.md` — review fixes log.
- [x] `docs/superpowers/plans/2026-05-26-repo-polish-and-discoverability.md` — polish plan.

## `.gitignore` already covers

- [x] `.commandcode/`
- [x] `.superpowers/`
- [x] `/mcp-server-go-quality` (root binary)
- [x] `coverage.out` and `*.test`

## P2 — remaining

- [ ] Terminal demo GIF

- [ ] Pre-flight CI sanity script (`scripts/check-ci-config.sh`)

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
