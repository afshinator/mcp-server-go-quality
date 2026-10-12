# `docs/superpowers/` — historical records

Everything in this directory is a **historical record**: dated design specs,
implementation plans, and review notes written while the server was being built.
They are kept for provenance, not as current documentation.

**Do not treat anything here as describing current behaviour.** These documents
predate the `2026-07-28` MCP protocol migration and the pinned-tool-version
consolidation, so they reference obsolete values — most visibly protocol
version `2025-03-26` and golangci-lint `v2.11.4`.

For current state, use:

| Question | Read |
|---|---|
| What does the server do, and how do agents call it? | [`../agents/AGENTS.md`](../agents/AGENTS.md) |
| Error tables, remediation, configuration reference | [`../agents/reference.md`](../agents/reference.md) |
| Outstanding work and why things are the way they are | [`../../TODO.md`](../../TODO.md) |
| The MCP-currency audit that produced the current state | [`../mcp-spec-currency.md`](../mcp-spec-currency.md) |

---

## Contents

- `plans/` — dated implementation plans (`01`–`05`, plus the repo-polish plan).
  Each was a TDD task list for a slice of the build.
- `specs/spec-v3.md` — the original design spec. Its two headline claims
  (targets protocol `2025-03-26`, pin `v2.11.4`) are both obsolete.
- `specs/adversarial-review-v3.md` — the review notes taken against that spec.
