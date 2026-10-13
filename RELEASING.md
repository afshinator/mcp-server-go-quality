# Releasing

A release is three edits plus a tag. The tag triggers goreleaser
(`.github/workflows/release.yml`), which builds both binaries and publishes the
archives. Do all three edits in one commit, then tag that commit.

1. **Version constant** — `internal/version/version.go`, `version.Value`.
   Goreleaser does not inject an ldflags version, so this constant is what both
   binaries report.

2. **Install references** — every `@v0.1.0` (or whichever the current pin is)
   must move to the new tag. They live in four files:

   - `README.md`
   - `docs/agents/AGENTS.md`
   - `examples/claude-code/README.md`
   - `examples/mcp-clients/README.md`

   The install examples must not point at `@latest`: a pin that trails the
   release is a doc bug, and a `@latest` that races the tag is worse.

3. **Changelog** — add a `## [X.Y.Z] — <date>` section to the top of
   `CHANGELOG.md`, above the previous release, under the Keep a Changelog
   header.

Then commit those three changes together and tag:

```sh
git commit -am "release vX.Y.Z"
git tag vX.Y.Z
git push origin main vX.Y.Z
```

Goreleaser fires on the `v*` tag; verify the release page shows one archive per
platform, each containing both binaries.
