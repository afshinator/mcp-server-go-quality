# Regenerate the README demo GIF:  make demo
#
# Records both entrypoints over one engine, against testdata/sample_project.
# `make demo` builds bin/go-quality and bin/mcp-server-go-quality, puts bin/ on
# PATH, then runs this tape from the repo root, so every command below is what a
# user actually types.
#
# Tape syntax reference: `vhs new` prints it. Two gotchas, both hit while
# writing this file:
#   - escaping quotes inside a double-quoted Type ("... \"x\" ...") does not
#     parse. When the command itself contains double quotes, delimit Type with
#     single quotes instead.
#   - `Output` rejects an unquoted absolute path; use a relative name.
#
# Commands stay under ~100 columns so they do not wrap mid-token, which is
# harder to read than the output wrapping.

Output demo.gif

Require go
Require jq
Require go-quality
Require mcp-server-go-quality

Set Shell "bash"
Set FontFamily "Menlo"
Set FontSize 17
Set Width 1160
Set Height 820
Set Padding 24
Set Theme "Dracula"
Set TypingSpeed 18ms
Set Framerate 24
Set PlaybackSpeed 1.5

Hide
Type "clear" Enter
Show

# 1. The CLI. One run, three checkers, one unified result.
Type "go-quality --version" Enter
Sleep 900ms

Type "go-quality testdata/sample_project" Enter
Sleep 4s

# 2. The same engine, machine-readable. Every finding carries a severity.
Type "go-quality --json testdata/sample_project | jq '.diagnostics[0] | {tool,file,line,severity}'" Enter
Sleep 4s

# 3. The MCP entrypoint: the same engine, spoken as a protocol.
Type 'R="{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}"' Enter
Sleep 700ms
Type 'echo "$R" | mcp-server-go-quality 2>/dev/null | jq -r ".result.tools[].name"' Enter
Sleep 3s
