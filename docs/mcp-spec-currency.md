# MCP spec currency audit

Audit date: 2026-10. Scope: is this server up to date with the newest MCP
specification revision, and what else in the repo needs updating.

Method: read the full repo, compared the pinned `mark3labs/mcp-go` version
against upstream releases, diffed the MCP spec revisions `2025-03-26` →
`2026-07-28`, and checked CI, pinned tool versions, and docs for drift.

A follow-up adversarial review (three independent reviewers: MCP/code, CI
actions, repo-wide doc drift) was run against the resulting change set. Its
findings and the fixes applied are in **Review pass** below.

---

## Status summary

| ID | Item | Status |
|---|---|---|
| O1 | Upgrade `mark3labs/mcp-go` to the 2026-07-28-capable release | [x] done |
| O2 | Declare `outputSchema` and return validated structured output | [x] done |
| O3 | Collapse the golangci-lint pin to one source and bump it | [x] done |
| O4 | Track GitHub Actions in Dependabot and bump the four actions | [x] done |
| O5 | Fix stale MCP-version and Go-version claims in docs | [x] done |
| O5a | Update all live output-contract descriptions to the envelope | [x] done (Review R1) |
| O6 | Pin `govulncheck` and `nilaway` (no more `latest`) | [x] done |
| O7 | Annotate `docs/superpowers/*` and `docs/tools-research.md` as historical | [x] done |
| O8 | Release ordering: ship before external consumers read `@latest` | [ ] **open — now T1 in `TODO.md`** |
| O9 | Timeout classification unreachable on the real subprocess path | [x] done |
| O10 | Pre-existing cosmetic drift (architecture.html, issue template) | [x] done |

Open work now lives in [`TODO.md`](../TODO.md). This file is the audit record:
the evidence and reasoning behind the changes, not a work list.

---

## Findings

**F1 — Protocol floor was 2+ revisions behind.**
Latest MCP revision is **2026-07-28**. The pinned dep
`mark3labs/mcp-go v0.54.1` tops out at `LATEST_PROTOCOL_VERSION = "2025-11-25"`.
Support for `2026-07-28` landed in mcp-go **v1.0.0**; latest is **v1.2.1**.

**F2 — Docs were wrong independent of the upgrade.**
README badge read `MCP-2025--03--26` and `docs/superpowers/specs/spec-v3.md`
said "targets 2025-03-26". The library had spoken `2025-11-25` since the pin,
so the stated contract was already 2 revisions stale.

**F3 — `2026-07-28` is a large breaking revision, but mostly not this server's surface.**
It removes the `initialize` handshake and protocol-level sessions, makes
`server/discover` mandatory, replaces the GET endpoint and
`resources/subscribe` with `subscriptions/listen`, removes `ping`,
`logging/setLevel`, and roots-list-changed, moves tasks into an extension,
introduces Multi Round-Trip Requests, requires `resultType` on every result,
requires `ttlMs`/`cacheScope` on list/read results, and removes SSE
resumability. This server is stateless, tool-only, over stdio — no sessions,
resources, prompts, sampling, or roots. The entire delta lives inside mcp-go,
not in this repo's code. No action beyond the dependency bump.

**F4 — The upgrade was recompile-level.**
Every API this repo touches was intact and signature-stable in v1.2.1:
`NewMCPServer`, `WithToolCapabilities`, `AddTool`, `NewTool`,
`WithString`/`WithArray`/`Description`, the four annotation hints,
`NewToolResultJSON`, `NewToolResultError`, `CallToolRequest.Get*`,
`ServeStdio`. Import path unchanged. Confirmed: the bump required zero
non-test source changes and the suite passed unchanged.

**F5 — No tool declared `outputSchema`.**
`NewToolResultJSON` already emitted `structuredContent`, but without a declared
`outputSchema` clients cannot validate it — so the `Diagnostic[]` contract was
specified in prose (`docs/agents/AGENTS.md`) but not machine-readable. Also note
that `structuredContent` must be a JSON *object*; the previous top-level array
was not a valid structured payload under `2025-06-18`–`2025-11-25`. Fixed by
wrapping diagnostics in a `diagnostics` key. **This is a contract change** —
see Breaking changes below.

**F6 — golangci-lint pin contradicted itself.**
`v2.11.4` in `internal/config/config.go`, README, CHANGELOG, all three
`examples/config/*.yaml`, and `Dockerfile`; `v2.12.0` in `Makefile` and
`.github/workflows/lint.yml`. Latest upstream was **v2.14.0**.

**F7 — CI actions were 2–3 majors stale.**
`actions/checkout@v4`→v7, `actions/setup-go@v5`→v7,
`golangci/golangci-lint-action@v7`→v9,
`goreleaser/goreleaser-action@v6`→v7. `.github/dependabot.yml` tracked
`gomod` only, so action versions never bumped.

**F8 — `govulncheck` and `nilaway` pinned to `latest`.**
In config defaults and baked as `@latest` in the Dockerfile. Non-reproducible;
the Docker image is a moving target. Deferred, see O6.

**F9 — Go was not installed on the audit machine.**
`go` absent from PATH, no `/usr/local/go`, no module cache. Nothing could be
built or tested locally. Resolved by installing Go 1.25.9 to `~/.local/go`
(see Environment below); each contributor still needs it, and CI already pins 1.25.

**F10 — README claimed "Go 1.21+".**
`go.mod` says `go 1.25.9` and CI requires 1.25.

**F11 — `examples/client.go` was not runnable (pre-existing, unrelated to MCP).**
`transport.NewStdio` takes the *executable* as its first argument; the example
passed `"go-quality"` there and `"go", "run", ...` as args, so it always died
with `exec: "go-quality": executable file not found in $PATH`. It also fetched
the server via `@latest`, which would have tested a published build rather than
the working tree. Fixed while updating the example for the new output envelope.

**F12 — `nilaway@latest` now requires Go >= 1.26.0.**
Installing it under the repo's Go 1.25.9 triggers `GOTOOLCHAIN=auto` to
download a second toolchain (go1.26.9) for the install. Relevant to O6: pinning
nilaway also pins which Go toolchain contributors need.

**F13 — `docs/architecture.html` never matched the tree (pre-existing).**
It says "based on spec v1" and its file map lists `checkers/checkers.go`,
`checkers/orchestrate.go`, and `discovery/tools.go` — none of which exist; the
real layout is `internal/diagnostic/diagnostic.go`,
`internal/checkers/orchestrator.go`, and `internal/discover/discover.go`. It
also omits `internal/pathutil`, `internal/root`, and `internal/toolname`, and
still claims "No caching between calls" despite the version cache. Fixed under O10
(see `../TODO.md` Done).

**F14 — Issue template names a binary that does not exist (pre-existing).**
`.github/ISSUE_TEMPLATE/bug_report.md:22` asks for
`go-quality --version`; the binary is `mcp-server-go-quality`. Fixed under O10
(see `../TODO.md` Done).

**F15 — Timeout classification is unreachable on the real subprocess path (pre-existing, MAJOR).**
`formatHandlerError` (`main.go`) and `formatCheckerError`
(`internal/checkers/orchestrator.go`) classify failures with
`errors.Is(err, context.DeadlineExceeded)` / `context.Canceled`. But when a tool
is killed by its deadline, `os/exec` prefers the process's own error and
discards the context error, so `ExecRunner.Run` returns
`*runner.ExitError{ExitCode: -1}` whose chain contains `signal: killed`, never
`context.DeadlineExceeded`.

Measured directly (`ExecRunner.Run` with a 150 ms deadline running `sleep 5`):

```
elapsed=152ms  err=Tool command failed with exit code -1. (*runner.ExitError)
errors.Is(err, context.DeadlineExceeded) = false
errors.Is(err, context.Canceled)         = false
ctx.Err()                                = context deadline exceeded
```

Consequence: a genuine golangci-lint/nilaway timeout surfaces as
`"Tool command failed with exit code -1. Stderr: ..."` instead of
`"timed out after 5m0s"`, contradicting `docs/agents/reference.md`. The unit
tests that appear to cover this feed a bare `context.DeadlineExceeded` sentinel,
which production never produces. Not introduced by this change
(`internal/runner/runner.go` and `internal/checkers/orchestrator.go` were not in
the original diff). **Fixed under O9**: `ExecRunner` reattaches the context error
only when the subprocess was signal-killed, so a real error that merely
coincides with a done context still reports itself, while a genuine deadline
reports `timed out after <duration>`. Reverting the fix reproduces the failure.
The residual Windows gap is tracked as T3 in `../TODO.md`.

---

## Actions taken (O1–O5)

### O1 — mcp-go v0.54.1 → v1.2.1
- `go get github.com/mark3labs/mcp-go@v1.2.1`, `go mod tidy`.
- Server advertises `2026-07-28` and negotiates down for older clients.
- No source changes required; see F4.
- Verified over stdio: `server/discover` responds, `tools/list` returns
  `resultType: complete`, `ttlMs: 0`, `cacheScope: private` (the 2026-07-28
  cacheable-result fields), and a legacy `initialize` with
  `protocolVersion: 2026-07-28` correctly negotiates down to `2025-11-25`.

### O2 — `outputSchema` + valid structured output
- All five tools declare an `outputSchema`.
- Check tools return `{"diagnostics": [...]}`; `install_tools` returns its
  `{installed, already_present, failed}` object.
- `structuredContent` and the text fallback carry the same JSON.
- `server.WithOutputSchemaValidation()` is enabled, so a result that drifts from
  the declared schema fails loudly instead of reaching the client.
- `Diagnostic.Native` is `json.RawMessage`, which the schema generator infers as
  a byte array; the schema widens it to `{}` (any JSON value). If that widening
  target ever disappears, construction now panics instead of silently shipping a
  schema that rejects every real result.
- Server construction was extracted into `newServer()` so tests exercise the real
  option set rather than a copy of it.

### O3 — single golangci-lint pin
- Added `config.GolangciLintVersion` (`v2.14.0`) as the single source; the load
  path and `Default()` both reference it.
- Updated `Makefile`, `.github/workflows/lint.yml`, `Dockerfile`,
  `examples/config/*.yaml`, README, CHANGELOG, and agent docs to match.
- Added `TestGolangciLintVersionIsConsistent`, which fails if any of those files
  references a different `v2.x.y`, so the drift in F6 cannot recur.

### O4 — Dependabot + action bumps
- Added `package-ecosystem: "github-actions"` to `.github/dependabot.yml`.
- Bumped all four actions to current majors in all three workflows, and the
  copy-paste example in `examples/github-actions/quality.yml`.

### O5 — doc drift
- README MCP badge → `2026-07-28`; Go prerequisite → 1.25+; output schema
  section rewritten for the envelope.
- `docs/agents/AGENTS.md` and `docs/agents/reference.md` updated for the
  envelope and the `structuredContent` location.
- CHANGELOG gained a `Changed` section covering the protocol bump, the breaking
  output change, and the tooling bumps.

---

## Review pass

Three independent reviewers ran against the change set. Their verdicts and the
disposition of every finding:

**CI action bumps — READY.** All four new majors were checked against the
upstream `action.yml` at each tag (fetched, HTTP 200, `inputs:` block compared
verbatim against every `with:` the workflows pass). No input was renamed,
removed, or newly required. `golangci-lint-action@v9` still accepts
`version: v2.14.0` in its default `binary` install mode; `goreleaser-action@v7`
still defaults `distribution: goreleaser` and `version: ~> v2`; `checkout@v7`
still declares `fetch-depth`; `setup-go@v7` still declares `go-version`. These
majors ship the node24 runtime, satisfied by `ubuntu-latest`. Residual
uncertainty: default-value changes for inputs we do *not* set were not diffed.

**MCP code — READY, with test gaps (all now closed).** The reviewer independently
derived from the mcp-go v1.2.1 source that the `native` widening path is real
(jsonschema-go inlines nested structs; `Properties` is `map[string]any`, so no
`$ref` indirection), that `json.RawMessage` really does infer as a byte array,
and that no legitimate result can become an `isError`. It flagged that this
reasoning was asserted rather than executed, so the regression was demonstrated
directly: disabling the widening makes
`TestConformingResultPassesOutputValidation` fail with
`output schema validation failed: /diagnostics/0/native: Got:object Want:[null array]`.
The canary is genuine.

**Doc drift — NOT READY, now fixed.** The reviewer found seven live,
consumer-facing artifacts that still promised a top-level `Diagnostic[]` — most
importantly the four MCP tool descriptions in `main.go`, which are what every
client actually reads via `tools/list`, plus `glama.json`, four README lines,
and `docs/agents/AGENTS.md`. All were stale claims introduced by O2 and missed
by the original pass.

Fixes applied in the review pass:

| ID | Fix |
|---|---|
| R1 | Tool descriptions in `main.go` (all four check tools) now state the envelope |
| R2 | `glama.json` master and `run_code_checks` descriptions updated |
| R3 | README tagline, summary, comparison table, and tools table updated |
| R4 | `docs/agents/AGENTS.md` intro updated |
| R5 | `examples/github-actions/quality.yml` actions bumped to v7/v7 |
| R6 | `CONTRIBUTING.md` now marks `docs/superpowers/` as historical and points at this file |
| R7 | Added `TestInstallResultPassesOutputValidation` — no test validated a real `InstallResult` payload against its schema (F-review-1) |
| R8 | Added `TestNativeSchemaIsWidened` — pins the `native` widening so `TestAllToolsDeclareOutputSchema` alone cannot miss a regression (F-review-2) |
| R9 | Negative test now asserts the failure text contains `output schema validation failed`, not merely `isError` (F-review-3) |
| R10 | Marshal tests now assert `StructuredContent`, not only the text block (F-review-3) |
| R11 | Widening failure now panics with a specific message instead of silently no-oping (F-review-4) |
| R12 | Removed a dangling "Task 2 fix" work-item comment from shipped test code (F-review-5) |

Reviewer findings accepted as other work: F15 (timeout classification, O9),
F13 (architecture.html, O10), F14 (issue template, O10).

Reviewer caveats carried forward: the reviewers had no shell access, so every
command output they reasoned from was run and supplied by this session; each
such claim above was re-verified here rather than taken on trust.

---

## Verification

Run with Go 1.25.9 against `mark3labs/mcp-go v1.2.1`:

| Check | Result |
|---|---|
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `gofumpt -l .` / `goimports -l .` (CI parity) | clean |
| `golangci-lint run ./...` (v2.14.0, the new pin) | 0 issues |
| `go test ./...` (full suite, incl. integration) | pass |
| `go test -short -race -coverprofile` | pass, total coverage 65.9% (CI threshold 60%) |
| `go mod tidy` | no further diff |
| stdio `server/discover` + `tools/list` on 2026-07-28 | correct schema, `resultType`, `ttlMs`, `cacheScope` |
| stdio `run_code_checks` on `testdata/sample_project` | 74 diagnostics, `structuredContent` populated, validation passed |
| stdio `install_tools` (real payload) | validation passed; `already_present` reports `golangci-lint v2.14.0` |
| `go run examples/client.go testdata/sample_project` | runs end-to-end |
| Widening disabled → probe test | fails with the expected schema error (canary proven) |
| Widening target renamed → `newServer` | panics loudly instead of silently degrading |

Test coverage of the new behaviour: `TestAllToolsDeclareOutputSchema`,
`TestNativeSchemaIsWidened`, `TestConformingResultPassesOutputValidation`,
`TestNonConformingResultFailsOutputValidation` (negative — a silently
uncompiled schema would otherwise make the positive test pass for the wrong
reason), and `TestInstallResultPassesOutputValidation`. Error-only and
empty-result payload shapes were checked separately and both validate.

---

## Breaking changes introduced

1. **Check tools now return an object, not a bare array.** `run_code_checks`,
   `run_lint`, `run_vuln_check`, and `run_nil_check` return
   `{"diagnostics": [...]}` instead of `[...]`. The text content block carries
   the same JSON as `structuredContent`. Consumers reading the top-level array
   must read `.diagnostics`.
2. **Protocol version is now `2026-07-28`.** Clients on older revisions
   negotiate down and keep working; clients on `2026-07-28` get the stateless
   core.
3. The project is still pre-1.0 (`v0.1.0`, released 2026-10-13), so these are
   recorded under the 0.1.x entries rather than a new major.

---

## Backlog

Open work lives in [`../TODO.md`](../TODO.md) — the live work list. Everything
raised below has since been closed (O6, O7, O9, O10) or moved there (O8 → T1).

For the record, the detailed backlog items raised by this audit, and what
became of them:

- **O6 — pin `govulncheck` and `nilaway` (F8).** Done: `config.DefaultVersion`
  is the single source for all three pins, mirrored in the `Dockerfile`, docs,
  and example configs, with `TestPinnedVersionsAreConsistent` enforcing it.
  Pinned to `golang.org/x/vuln v1.9.0` and
  `go.uber.org/nilaway v0.0.0-20260918162853-acb8859b9031`. The residual problem
  — nothing bumps these automatically — is T4.
- **O7 — historical plan docs (F2).** Done by annotation:
  `docs/superpowers/README.md` indexes the directory and states it is
  historical, and `specs/spec-v3.md` and `docs/tools-research.md` carry banners.
  Whether to delete them outright is T2.
- **O8 — release ordering (F-follow-up).** Moved to T1. `examples/client.go`
  reads `structuredContent.diagnostics` and builds from the working tree, so it
  is correct in-repo; the contract change still needs to ship before external
  consumers read `@latest`.
- **O9 — timeout classification was unreachable (F15).** Done. `ExecRunner` now
  reattaches the context error when a subprocess was signal-killed, so a real
  deadline reports `timed out after <duration>`. Verified by reverting the fix:
  `TestExecRunnerTimeoutSurfacesDeadline` fails with `exit code -1` without it.
  The Windows gap is T3.
- **O10 — cosmetic pre-existing drift (F13, F14).** Done.
  `docs/architecture.html` file map corrected and the stale cache claim
  replaced; the issue template now names `mcp-server-go-quality --version`.

---

## Environment

Go 1.25.9 installed to `~/.local/go`, symlinked into `~/.local/bin`, and
`export PATH="$HOME/.local/go/bin:$PATH"` appended to `~/.zshrc`. Verified with
`go version`. No other machine-level changes.

---

## Not recommendations

Deliberately not adopted, recorded so the decision is not relitigated:

- **Stateless `_meta` plumbing, `server/discover` handlers, `subscriptions/listen`,
  task extension, MRTR.** All handled inside mcp-go v1.2.1. This server is
  tool-only over stdio and has no use for them. Writing handlers here would be
  dead code.
- **Streamable HTTP transport.** Out of scope; this is a stdio server.
- **`json.RawMessage` → `any` for `Diagnostic.Native`** to make the generated
  schema correct. Rejected: `json.RawMessage` preserves the raw tool output
  verbatim by design, and widening the schema to `{}` achieves the same result
  with a smaller diff.
- **Hand-written `outputSchema` JSON** instead of deriving it from
  `DiagnosticsResponse`. Rejected: derivation keeps the schema in sync when
  `Diagnostic` gains a field, at the cost of one targeted widening.
