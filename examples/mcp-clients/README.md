# Using go-quality with any MCP harness

The server is a local command that speaks JSON-RPC over stdio, so any MCP client
that can spawn a process can run it. The client-specific part is only *where the
config lives* and *what the entry is called* — the server definition itself is
the same shape everywhere:

```json
{
  "mcpServers": {
    "go-quality": {
      "command": "mcp-server-go-quality",
      "args": []
    }
  }
}
```

Install the server once, then add that definition where your client looks for
it. A relative `command` resolves from the client's working directory, so either
install into `$GOBIN` (the default) or use an absolute path.

| Client | Config file | Entry |
|---|---|---|
| Claude Desktop | `claude_desktop_config.json` | `mcpServers` |
| Cursor | `.cursor/mcp.json` | `mcpServers` |
| Gemini CLI | `~/.gemini/settings.json` | `mcpServers` |
| Codex CLI | `~/.codex/config.toml` | `[mcp_servers.go-quality]` (TOML: `command`, `args`) |
| Anything else | the client's docs | most follow the `mcpServers` shape above |

## Tools exposed

| Tool | Use it when |
|---|---|
| `run_code_checks` | the whole sweep: golangci-lint, govulncheck and nilaway in parallel |
| `run_lint` | golangci-lint only — fast feedback |
| `run_vuln_check` | govulncheck only — reachable CVEs |
| `run_nil_check` | nilaway only — nil-panic paths |
| `install_tools` | pre-install the pinned binaries at session start |

## No MCP needed: the CLI

Any harness with shell access can skip the protocol entirely and call the
`go-quality` binary directly. It runs the same three checkers against the same
config and prints the same `{"diagnostics": [...]}` envelope, which makes it
usable from scripts, CI and hooks:

```bash
go install github.com/afshinator/mcp-server-go-quality/cmd/go-quality@v0.1.0

go-quality --json . | jq '.diagnostics'          # machine-readable
go-quality --tools=golangci-lint .               # fast subset
go-quality --fail-on=none .                      # report only
```

Exit codes gate it: `0` clean, `1` findings, `2` the run did not complete.
stdout carries only the payload; progress and the summary go to stderr, so a
pipe is never corrupted by a first-run tool install.

## Both entrypoints, one engine

The CLI and the MCP server resolve the same `.go-quality.yaml`, auto-install the
same pinned tool versions and return the same envelope, so a config recipe or a
`jq` query written for one works for the other.
