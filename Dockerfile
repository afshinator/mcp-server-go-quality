# Stage 1: build the server binary
FROM golang:alpine AS build
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /mcp-server-go-quality ./cmd/mcp-server-go-quality/

# Stage 2: runtime — Go is required for GOBIN resolution and tool auto-install.
# Pre-bake the three quality tools to eliminate first-run latency.
FROM golang:alpine
RUN go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 && \
    go install golang.org/x/vuln/cmd/govulncheck@v1.9.0 && \
    go install go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260918162853-acb8859b9031
COPY --from=build /mcp-server-go-quality /usr/local/bin/mcp-server-go-quality
ENTRYPOINT ["mcp-server-go-quality"]
