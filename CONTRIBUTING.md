# Contributing

## Development setup

```bash
git clone https://github.com/afshinator/mcp-server-go-quality.git
cd mcp-server-go-quality
go mod download
```

## Testing

```bash
make test       # unit tests (fast)
make test-all   # full suite including integration
```

## Code quality before submitting

```bash
make lint       # golangci-lint
make vet        # go vet
make fmt        # gofumpt + goimports
make build      # verify clean build
```

## TDD

This project enforces red-green-refactor TDD. Nearly every source file has a companion `_test.go` file in the same package. Every PR that adds or changes behavior must include tests.

## Architecture

Package structure, design decisions, and implementation history are in `docs/superpowers/`. Start with the spec (`specs/spec-v3.md`) for the design rationale.

## Pull request checklist

- [ ] Tests pass: `make test && make test-all`
- [ ] Lint clean: `make lint`
- [ ] Formatting clean: `gofumpt -d .` shows no diffs
- [ ] Relevant documentation updated
