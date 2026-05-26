# TODO

## Files to review for .gitignore

- [ ] `.mcp.json` — per-project MCP config. Already tracked. Different devs may want different servers. Decide: keep tracked or gitignore.
- [ ] `CLAUDE.md` — already tracked. Project header says "This file IS committed." Leave as-is unless you change your mind.

## .gitignore additions needed

- [ ] `.commandcode/` — local Command Code state (taste, plans). Add to .gitignore.
- [ ] `*.test` already covered but there was a stale `checkers.test` at root. Verify gone.
- [ ] `mcp-server-go-quality` — binary at repo root. Already cleaned up. Ensure it doesn't reappear from local builds.

## Files that should be committed (currently untracked)

- [ ] `project-image-01.png` — the logo. Stage and commit.
- [ ] All new P0/P1 files from the repo polish plan:
  - [ ] `.github/` (workflows, templates, CODEOWNERS)
  - [ ] `.goreleaser.yml`
  - [ ] `LICENSE`
  - [ ] `CHANGELOG.md`
  - [ ] `CONTRIBUTING.md`
  - [ ] `SECURITY.md`
  - [ ] `glama.json`
  - [ ] `smithery.yaml`
  - [ ] `package.json`
  - [ ] `docs/superpowers/plans/2026-05-26-repo-polish-and-discoverability.md`

## P2 polish (deferred)

- [ ] `examples/` — client.go, config recipes, CI recipe
- [ ] `.env.example`
- [ ] Pre-commit hooks
- [ ] `.github/dependabot.yml`
- [ ] Coverage threshold in CI
- [ ] Terminal demo GIF
