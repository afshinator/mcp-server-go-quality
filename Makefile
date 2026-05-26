APP_NAME := mcp-server-go-quality
CMD_DIR := ./cmd/$(APP_NAME)

.PHONY: build test test-all lint vet clean fmt run

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

run:
	go run $(CMD_DIR)
