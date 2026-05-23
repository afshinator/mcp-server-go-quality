# Go Code Quality Tools — Research & Overlap Analysis

## Tools Under Consideration

### 1. golangci-lint (v2.11.4 installed)

**What it is:** A fast Go linters runner — aggregates ~100+ linters, runs them in parallel,
caches results, supports YAML config. The industry-standard Go linting orchestrator.

**Relevant built-in linters already in `cryptospect-cli/.golangci.yml`:**
- `govet` — suspicious constructs (part of `default: standard`)
- `errcheck` — unchecked error returns (part of `default: standard`)
- `revive` — golint replacement, configurable rules
- `gocritic` — opinionated bug/performance/style diagnostics
- `nilerr` — returning nil when err != nil
- `bodyclose` — unclosed HTTP response bodies
- `errorlint` — error wrapping best practices

**Additional built-in linters available but NOT yet enabled:**
- `gocyclo` — cyclomatic complexity (classic)
- `cyclop` — cyclomatic complexity (newer, package-level support)
- `gocognit` — cognitive complexity (structural branching depth)
- `gosec` — security problems in source code
- `nilnesserr` — reports nil error returns after nil check
- `nilnil` — simultaneous return of nil error + invalid value

**Output:** `golangci-lint run --out-format=json` produces structured JSON with file, line,
column, severity, linter name, and message per issue.

### 2. govulncheck (installed)

**What it is:** Official Go vulnerability scanner from `golang.org/x/vuln`. Uses call-graph
static analysis to find only *reachable* vulnerabilities (not just any dependency with a CVE).
Queries the Go vulnerability database at vuln.go.dev.

**Key capabilities:**
- Source analysis (`govulncheck ./...`)
- Binary analysis (`govulncheck -mode binary <binary>`)
- Streaming JSON output (`-json`)
- SARIF and OpenVEX formats

**Overlap with golangci-lint:** NONE. golangci-lint's `gosec` checks for insecure coding
patterns (weak crypto, hardcoded credentials). govulncheck checks for known CVEs in
dependencies. They are complementary.

**Limitations:** No silencing mechanism for findings. Conservative function pointer analysis
may produce false positives. Cannot see through `reflect` calls.

### 3. nilaway (Uber — NOT installed)

**What it is:** Uber's static analysis tool for detecting potential nil panics at compile
time. Tracks nil flows across function boundaries using sophisticated inter-procedural
analysis.

**Key capabilities:**
- Cross-function nil flow tracking (unlike simple nil checks)
- Conditional nil detection
- JSON output: `-json -pretty-print=false`
- Package filtering: `-include-pkgs`, `-exclude-pkgs`

**Overlap with golangci-lint:** PARTIAL. golangci-lint has `nilerr` (returning nil when
err != nil), `nilnesserr` (returning different nil after check), and `nilnil` (simultaneous
nil + invalid value returns). But nilaway does *deep inter-procedural nil flow analysis*
that none of these can match. It tracks nil values as they flow from function returns
through callers.

**Integration options:**
- **Standalone binary:** `nilaway -json -pretty-print=false -include-pkgs="..." ./...`
- **golangci-lint module plugin:** Requires golangci-lint >= 1.57.0, custom build via
  `golangci-lint custom`

### 4. gocyclo (NOT installed separately)

**What it is:** Classic cyclomatic complexity counter. Measures the number of linearly
independent paths through a function's control flow graph.

**Status:** **BUILT INTO golangci-lint** as the `gocyclo` linter. No separate binary needed.
Also has `cyclop` as a newer alternative with package-level complexity support.

**Overlap:** COMPLETE. Running `gocyclo` standalone would be fully redundant with enabling
the `gocyclo` linter in golangci-lint.

### 5. gocognit (NOT installed separately)

**What it is:** Measures cognitive complexity — how hard a function is to understand
(structural nesting, recursion, logical operators). More human-relevant than cyclomatic.

**Status:** **BUILT INTO golangci-lint** as the `gocognit` linter. No separate binary needed.

**Overlap:** COMPLETE. Running `gocognit` standalone would be fully redundant.

## Summary: What Actually Belongs in the MCP Server

| Tool | Run separately? | Reason |
|---|---|---|
| **golangci-lint** | YES — primary orchestrator | Covers linting, complexity (gocyclo/gocognit), security patterns (gosec), nil basics |
| **govulncheck** | YES — standalone | Unique capability: CVE scanning of dependency tree. No golangci-lint overlap. |
| **nilaway** | YES — standalone or plugin | Unique capability: deep nil flow analysis. Partial overlap but adds value beyond golangci-lint nil linters. |
| **gocyclo** | NO | Already inside golangci-lint |
| **gocognit** | NO | Already inside golangci-lint |

## Recommended Approach

Run **3 tools** via the MCP server:

1. `golangci-lint run --out-format=json ./...` — with `gocyclo`, `gocognit`, and `gosec`
   added to the config. This covers: linting, complexity, security patterns.
2. `govulncheck -json ./...` — dependency vulnerability scanning.
3. `nilaway -json -pretty-print=false -include-pkgs="<module>" ./...` — deep nil-panic
   detection.
