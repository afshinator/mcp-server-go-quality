APP_NAME              := mcp-server-go-quality
CMD_DIR               := ./cmd/$(APP_NAME)
GOLANGCI_LINT_VERSION := v2.12.0

.PHONY: build install test test-all lint audit nilcheck vet clean fmt fmt-check run check setup-dev

build:
	go build -o bin/$(APP_NAME) $(CMD_DIR)

install:
	go install $(CMD_DIR)

test:
	go test -short ./...

test-all:
	go test -timeout 10m ./...

lint:
	golangci-lint run ./...

audit:
	govulncheck -json ./...

nilcheck:
	nilaway -json -pretty-print=false ./...

vet:
	go vet ./...

clean:
	rm -rf bin/

fmt:
	gofumpt -w ./
	goimports -w ./

fmt-check:
	@out=$$(gofumpt -d .); if [ -n "$$out" ]; then printf '%s\n' "$$out"; echo "ERROR: gofumpt found formatting issues. Run 'make fmt'."; exit 1; fi
	@out=$$(goimports -l .); if [ -n "$$out" ]; then printf '%s\n' "$$out"; echo "ERROR: goimports found import issues. Run 'make fmt'."; exit 1; fi
	@echo "Formatting OK."

run:
	go run $(CMD_DIR)

check: fmt vet lint test
	@echo "All checks passed."

setup-dev:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install mvdan.cc/gofumpt@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/evilmartians/lefthook@latest
	lefthook install
	@echo "Dev environment ready. golangci-lint $(GOLANGCI_LINT_VERSION) installed to match CI."
