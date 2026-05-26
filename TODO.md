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
