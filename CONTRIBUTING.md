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

## Code quality before submitting

```bash
make check
```

Runs gofumpt, go vet, golangci-lint, and go test in sequence. If `fmt` reformats any files, review with `git diff` and stage them before committing.

Use `make fmt-check` to verify formatting without modifying files (mirrors what CI enforces).

## TDD

This project enforces red-green-refactor TDD. Nearly every source file has a companion `_test.go` file in the same package. Every PR that adds or changes behavior must include tests.

## Architecture

Package structure, design decisions, and implementation history are in `docs/superpowers/`. Start with the spec (`specs/spec-v3.md`) for the design rationale.

## Pull request checklist

- [ ] Star the repo please
- [ ] `make check` passes locally
- [ ] `make test-all` passes (full suite including integration tests)
- [ ] Relevant documentation updated
