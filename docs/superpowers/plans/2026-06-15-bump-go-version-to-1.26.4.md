# Bump Go Version to 1.26.4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade the project from Go 1.25.9 to Go 1.26.4 across all config files, CI workflows, test fixtures, and docs.

**Architecture:** Mechanical string version bumps only — no code changes needed. Go 1.26 is fully backward compatible per the Go 1 compatibility promise. All dependencies are compatible (highest dep requirement is `mcp-go v0.54.1` → `go 1.25.5`). CI `setup-go` accepts `"1.26"` as a semver range.

**Proof of safety:**
- Go 1.26 release notes: *"maintains the Go 1 promise of compatibility...almost all Go programs to continue to compile and run as before"* → https://go.dev/doc/go1.26
- Language changes are additive only: `new(expr)` and self-referential generic type params
- No `GODEBUG`, `iter.Seq`, `log/slog`, or `//go:build` version guards in the codebase (verified via grep)
- All dependency `go` directives satisfied by Go 1.26

**Tech Stack:** Go 1.26.4, mark3labs/mcp-go v0.54.1

---

### Task 1: Update go.mod directive

**Files:**
- Modify: `go.mod:3`

- [ ] **Step 1: Bump the go directive**

  Change line 3 from:
  ```
  go 1.25.9
  ```
  to:
  ```
  go 1.26.4
  ```

- [ ] **Step 2: Run `go mod tidy` to reconcile dependencies**

  Run: `go mod tidy`
  Expected: Succeeds, may update `go.sum` entries, no errors.

- [ ] **Step 3: Verify the module builds**

  Run: `go build ./cmd/mcp-server-go-quality/`
  Expected: Binary compiles without errors.

- [ ] **Step 4: Commit**

  ```bash
  git add go.mod go.sum
  git commit -m "chore: bump go directive to 1.26.4"
  ```

---

### Task 2: Update testdata sample project go.mod

**Files:**
- Modify: `testdata/sample_project/go.mod:3`

- [ ] **Step 1: Bump the go directive**

  Change line 3 from:
  ```
  go 1.25
  ```
  to:
  ```
  go 1.26
  ```

- [ ] **Step 2: Commit**

  ```bash
  git add testdata/sample_project/go.mod
  git commit -m "chore: bump testdata sample project go directive to 1.26"
  ```

---

### Task 3: Update CI workflows (3 files)

**Files:**
- Modify: `.github/workflows/test.yml:13`
- Modify: `.github/workflows/lint.yml:17`
- Modify: `.github/workflows/release.yml:21`

- [ ] **Step 1: Update test.yml**

  Change line 13 from:
  ```yaml
          go-version: ["1.25"]
  ```
  to:
  ```yaml
          go-version: ["1.26"]
  ```

- [ ] **Step 2: Update lint.yml**

  Change line 17 from:
  ```yaml
            go-version: "1.25"
  ```
  to:
  ```yaml
            go-version: "1.26"
  ```

- [ ] **Step 3: Update release.yml**

  Change line 21 from:
  ```yaml
            go-version: "1.25"
  ```
  to:
  ```yaml
            go-version: "1.26"
  ```

- [ ] **Step 4: Commit**

  ```bash
  git add .github/workflows/test.yml .github/workflows/lint.yml .github/workflows/release.yml
  git commit -m "ci: bump Go version from 1.25 to 1.26 in all workflows"
  ```

---

### Task 4: Update example workflow

**Files:**
- Modify: `examples/github-actions/quality.yml:22`

- [ ] **Step 1: Bump the go-version**

  Change line 22 from:
  ```yaml
            go-version: "1.25"
  ```
  to:
  ```yaml
            go-version: "1.26"
  ```

- [ ] **Step 2: Commit**

  ```bash
  git add examples/github-actions/quality.yml
  git commit -m "docs: bump Go version in example CI workflow to 1.26"
  ```

---

### Task 5: Update test fixtures — root_test.go (11 occurrences)

**Files:**
- Modify: `internal/root/root_test.go`

- [ ] **Step 1: Bump all `go 1.25` strings to `go 1.26`**

  Use `replaceAll` to change every occurrence of `go 1.25` → `go 1.26` in the file.
  Affects lines: 16, 19, 34, 53, 73, 76, 99, 113, 124, 127, 142.
  These are string literals inside mock `go.work` and `go.mod` content — not the actual Go directive.

- [ ] **Step 2: Run the affected tests**

  Run: `go test -short -run 'TestDiscover|TestWorkspaceModules|TestParseUseDirectives' ./internal/root/`
  Expected: All pass.

- [ ] **Step 3: Commit**

  ```bash
  git add internal/root/root_test.go
  git commit -m "test: bump mock Go version strings in root_test.go to 1.26"
  ```

---

### Task 6: Update test fixtures — discover_test.go (4 occurrences)

**Files:**
- Modify: `internal/discover/discover_test.go`

- [ ] **Step 1: Bump all `go1.25.9` strings to `go1.26.4`**

  Use `replaceAll` to change every occurrence of `go1.25.9` → `go1.26.4` in the file.
  Affects lines: 23, 37, 48, 59.
  These are mock `go version -m` binary metadata outputs.

- [ ] **Step 2: Run the affected tests**

  Run: `go test -short -run 'TestParseGoVersion' ./internal/discover/`
  Expected: All pass.

- [ ] **Step 3: Commit**

  ```bash
  git add internal/discover/discover_test.go
  git commit -m "test: bump mock binary Go version strings in discover_test.go to 1.26.4"
  ```

---

### Task 7: Update test fixtures — govulncheck_test.go (6 occurrences)

**Files:**
- Modify: `internal/checkers/govulncheck_test.go`

- [ ] **Step 1: Bump mock stdlib vulnerability versions**

  The test fixtures mock CVE data using Go stdlib versions. These are purely test data — the parser doesn't validate version values.

  Change every occurrence of `"version":"v1.25.9"` → `"version":"v1.26.3"` (vulnerable stdlib version).
  Affects 2 occurrences on lines 17, 18.

  Change every occurrence of `"fixed_version":"v1.25.10"` → `"fixed_version":"v1.26.4"` (fixed-in version).
  Affects 6 occurrences on lines 17, 18, 70, 87, 89, 163.

- [ ] **Step 2: Run the affected tests**

  Run: `go test -short -run 'TestParseGovulncheck' ./internal/checkers/`
  Expected: All pass.

- [ ] **Step 3: Commit**

  ```bash
  git add internal/checkers/govulncheck_test.go
  git commit -m "test: update mock CVE fixture versions in govulncheck_test.go for Go 1.26"
  ```

---

### Task 8: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md:12`

- [ ] **Step 1: Bump the stack declaration**

  Change line 12 from:
  ```
  **Stack:** Go 1.25
  ```
  to:
  ```
  **Stack:** Go 1.26
  ```

- [ ] **Step 2: Commit**

  ```bash
  git add CLAUDE.md
  git commit -m "docs: update stack Go version in CLAUDE.md to 1.26"
  ```

---

### Task 9: Verify everything

- [ ] **Step 1: Run full test suite**

  Run: `go test -short -race ./...`
  Expected: All tests pass.

- [ ] **Step 2: Run vet**

  Run: `go vet ./...`
  Expected: No issues.

- [ ] **Step 3: Run lint (if golangci-lint installed)**

  Run: `golangci-lint run ./...`
  Expected: Clean.

- [ ] **Step 4: Final summary**

  All config, CI, test fixtures, and docs have been updated from Go 1.25.9 to Go 1.26.4.
