# Using go-quality in Claude Code

Two ways to use this server with Claude Code. Both run the same three checkers
and return the same `{"diagnostics": [...]}` envelope.

## 1. The MCP server (recommended for interactive work)

Register it once:

```bash
claude mcp add go-quality -- go run github.com/afshinator/mcp-server-go-quality/cmd/mcp-server-go-quality@v0.1.1
```

Or in `.mcp.json` / `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "go-quality": {
      "command": "go-quality",
      "args": []
    }
  }
}
```

Then ask for what you want:

> run the code checks on this project and tell me about the security issues

Claude Code manages the server lifecycle, calls `install_tools` at session
start, and reads the `structuredContent` envelope — you never see the protocol.

## 2. The CLI (one-shot runs, scripts, hooks)

If you have shell access and just want the findings:

```bash
go install github.com/afshinator/mcp-server-go-quality/cmd/go-quality@v0.1.1

cd ~/my-go-project
go-quality                      # human-readable on stdout
go-quality --json | jq '.diagnostics'   # machine-readable
```

Exit codes make it gateable:

```bash
go-quality --fail-on=error . || echo "blocking findings"
```

## Which one

| | MCP server | CLI |
|---|---|---|
| Interactive agent sessions | yes | — |
| One-shot terminal runs | — | yes |
| Scripts, CI, pre-commit | — | yes |
| Envelope | `structuredContent` | `--json` (byte-identical) |
| Tools auto-install | yes | yes |
