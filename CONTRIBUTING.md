# Contributing

## Development setup

```bash
git clone https://github.com/afshinator/mcp-server-go-quality.git
cd mcp-server-go-quality
go mod download
make setup-dev
```

`make setup-dev` installs golangci-lint (pinned to the same version CI uses), gofumpt, goimports, and lefthook, then wires up the pre-commit and pre-push git hooks. Run once after cloning. The pre-commit hook formats staged files and runs incremental lint; the pre-push hook runs a full lint pass and short tests before anything reaches GitHub.

## Testing

```bash
make test       # unit tests (fast)
make test-all   # full suite including integration
```

### Why the integration workflow exists

`make test` runs `go test -short`, which skips the four tests that actually
invoke golangci-lint, govulncheck and nilaway. A release is built from
binaries, not from tests, so without a real run of the checkers we could tag a
release and only find out after a user installs it that a pinned tool no longer
installs, or emits output our parser no longer understands.

The `.github/workflows/integration.yml` workflow closes that gap: on every merge
to `main`, weekly, and on demand, it installs the pinned tools
(`go run ./cmd/go-quality --install-only`) and runs the real checkers against
`testdata/sample_project`. If a pinned tool stops installing or its output stops
parsing, that workflow fails days before anyone cuts a release -- not after a
user files a bug.

## Code quality before submitting

```bash
make check
```

Runs gofumpt, go vet, golangci-lint, and go test in sequence. If `fmt` reformats any files, review with `git diff` and stage them before committing.

Use `make fmt-check` to verify formatting without modifying files. Note: CI checks only `gofumpt`; `make fmt-check` also checks `goimports`, so it is stricter.

## TDD

This project enforces red-green-refactor TDD. Nearly every source file has a companion `_test.go` file in the same package. Every PR that adds or changes behavior must include tests.

## Architecture

Open work and known gaps are tracked in [`TODO.md`](TODO.md) — that is the work
list. Package structure, design decisions, and implementation history are in
`docs/superpowers/`, which are historical records; see its
[README](docs/superpowers/README.md). The MCP-currency audit and its evidence
are in [`docs/mcp-spec-currency.md`](docs/mcp-spec-currency.md).

## Pull request checklist

- [ ] Star the repo please
- [ ] `make check` passes locally
- [ ] `make test-all` passes (full suite including integration tests)
- [ ] Formatting clean: `make fmt-check` shows no issues
- [ ] Relevant documentation updated
