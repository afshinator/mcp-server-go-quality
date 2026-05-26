# Reference — mcp-server-go-quality

Detailed error handling, remediation, and troubleshooting. Load on demand when
the compact [AGENTS.md](AGENTS.md) doesn't cover what you need.

---

## Response schema

```
Diagnostic {
  tool:     "golangci-lint" | "govulncheck" | "nilaway"
  file:     string          // relative path from project root; "" if unknown
  line:     number          // 0 if unknown
  column:   number          // absent from JSON when 0 (omitempty)
  severity: "error" | "warning" | ""   // "" for govulncheck/nilaway; absent when empty
  message:  string          // human-readable summary
  error:    string          // "" on success; non-empty on tool failure
  native:   object          // full raw tool output; also populated for govulncheck parse error diagnostics as a JSON array of error strings
}
```

---

## Error handling

### Tool-level errors (in the Diagnostic array)

These appear as entries with a non-empty `error` field. Other tools' results are
still present in the same response.

| `error` value | Meaning | What to do |
|---|---|---|
| `"Tool command failed with exit code N. Stderr: ..."` | Tool crashed or found a config problem | Show stderr to user; check `.golangci.yml` or project config |
| `"timed out after 5m0s"` | Tool exceeded per-tool deadline | Increase `timeout` in `.go-quality.yaml` |
| `"cancelled"` | Client disconnected while tool was running | Safe to retry |
| `"unexpected output format from <tool>..."` | Tool version mismatch with server's parser | Run `install_tools` to reinstall the pinned version |
| `"failed to parse govulncheck output: ..."` | govulncheck output had malformed lines | Likely a govulncheck version issue; run `install_tools`. Inspect `native` for the raw parse error. |

### Fatal errors (JSON-RPC error, no Diagnostic array)

| Error message | Meaning | What to do |
|---|---|---|
| `"not a Go project: no go.mod or go.work found"` | No `go.mod` or `go.work` walking up from `project_path` | Verify the path is inside a Go module |
| `"config error: ..."` | `.go-quality.yaml` is malformed or unreadable | Fix the YAML or delete the file to use defaults |
| `"unknown tool: \"<value>\". valid values: golangci-lint, govulncheck, nilaway"` | Bad value in `tools` parameter | Check the `tools` array for typos |
| `"installing <tool>: ..."` | Tool installation failed | Report to user; try running `go install <path>@<version>` manually |
| `"...resolving Go binary directory..."` | `go` is not on PATH | The Go toolchain must be installed separately; the server will not start |

---

## Timeout guidance

The `timeout` field in `.go-quality.yaml` is a **per-tool deadline** — each tool
gets this budget independently. Default: 5 minutes.

```yaml
timeout: 10m   # give each tool up to 10 minutes
```

**Why govulncheck is slow the first time:** govulncheck downloads the Go vulnerability
database (~40 MB) on its first invocation. This happens inside tool execution, not
during `install_tools`. The database is cached afterward. If govulncheck times out on
the first run, increase `timeout` to `10m` for the first scan, then revert.

**Why nilaway is slow on large projects:** nilaway performs whole-program analysis.
On a monorepo with many packages it routinely takes 3–10 minutes. Use the `tools`
subset parameter to skip nilaway during iterative development.

---

## Using native output for remediation

### golangci-lint — apply suggested fixes

```python
for d in diagnostics:
    if d["tool"] != "golangci-lint":
        continue
    fixes = d["native"].get("SuggestedFixes", [])
    for fix in fixes:
        # fix contains TextEdits with exact byte ranges — apply them directly
        apply_text_edits(fix["TextEdits"])
```

### govulncheck — report CVE with fix version

```python
for d in diagnostics:
    if d["tool"] != "govulncheck":
        continue
    native = d["native"]
    osv_id   = native["finding"]["osv"]
    fixed_at = native["finding"].get("fixed_version", "no fix available")
    cve_ids  = native.get("osv", {}).get("aliases", [])
    cve_str  = ", ".join(c for c in cve_ids if c.startswith("CVE-"))
    print(f"Vulnerability {osv_id} ({cve_str}): fix available in {fixed_at}")
    for frame in native["finding"]["trace"]:
        pos = frame.get("position", {})
        print(f"  {frame['package']} {pos.get('filename','')}:{pos.get('line','')}")
```

### nilaway — show the nil propagation chain

nilaway's `message` field contains the full nil-propagation chain. The `Native`
object carries the `posn` of the panic site and `end` of the nil origin:

```python
for d in diagnostics:
    if d["tool"] != "nilaway":
        continue
    print(f"Nil panic at {d['file']}:{d['line']}")
    print(f"  {d['message']}")
```

---

## Multi-module workspaces

The server fully supports `go.work` workspaces. Pass any directory within the
workspace as `project_path` — the server walks up to find `go.work` automatically.

For nilaway in a multi-module workspace, the server automatically collects all
module paths from the `use` directives and passes them as `-include-pkgs` so nilaway
scans all local modules and ignores vendor/stdlib. No manual configuration is needed.

---

## Troubleshooting

**"unexpected output format from golangci-lint..."**
The installed golangci-lint version doesn't match the server's parser (pinned to
v2.11.4) or is a different major version. Run `install_tools` to restore the pinned
version.

**govulncheck returns no findings on a project with known old dependencies**
govulncheck only reports vulnerabilities that are reachable via your call graph.
A dependency may have a CVE but if your code never calls the vulnerable function,
govulncheck correctly reports nothing. This is by design.

**nilaway reports too many false positives**
Add `--exclude-pkgs=<pattern>` to `extra_args` in `.go-quality.yaml` to skip
packages you don't control. Generated code packages are a common exclusion.

**All three tools time out in CI**
CI environments often have cold caches and slow disk. Set `timeout: 15m` in
`.go-quality.yaml` and call `install_tools` explicitly in the CI setup step before
running checks, to separate install time from analysis time.

**nilaway exits with build errors**
nilaway requires a fully type-checked program. If the project has syntax errors
or missing imports, nilaway will exit non-zero with compiler errors in stderr.
Check the `error` field for `syntax error` or `undefined:` patterns; fix the build
errors then re-run.

**govulncheck exits with "missing go.sum entry"**
govulncheck requires an up-to-date `go.sum`. If `go mod tidy` was never run,
govulncheck exits non-zero with a checksum verification error. Prompt the user
to run `go mod tidy` and retry.

**Large projects produce very large responses**
golangci-lint on a large project with many linters enabled can return thousands
of issues, each with a `native` field containing the full raw JSON. Responses may
exceed the transport's message size limit. The server has no pagination or
truncation — reduce the linter set in `.golangci.yml` or run individual tools
separately.

---

## Configuration reference

Create `.go-quality.yaml` in the project root (or server CWD) to override defaults.
All fields optional.

```yaml
timeout: 5m          # per-tool deadline; increase for large monorepos

tools:
  golangci-lint:
    version: v2.11.4   # pinned; override only if you need a specific version
    extra_args: []     # appended after required flags; cannot override --output.text.path or --output.json.path
  govulncheck:
    version: latest
    extra_args: []
  nilaway:
    version: latest
    extra_args: ["--exclude-pkgs=github.com/myorg/vendor"]
```

The server's required flags (`--output.text.path stderr --output.json.path stdout`
for golangci-lint, `-json` for govulncheck, `-json -pretty-print=false` for
nilaway) are always applied and cannot be overridden via `extra_args`.

---

## install_tools response schema

```json
{
  "installed":      [{"tool": "golangci-lint", "version": "v2.11.4"}],
  "already_present": [{"tool": "govulncheck", "version": "v1.3.0"}],
  "failed":          [{"tool": "nilaway", "version": "latest", "command": "go install go.uber.org/nilaway/cmd/nilaway@latest", "stderr": "..."}]
}
```

- `installed` — tools that were newly downloaded this call
- `already_present` — tools already at the correct version (fast cache hit)
- `failed` — tools that could not be installed; contains the full `go install` command and stderr
