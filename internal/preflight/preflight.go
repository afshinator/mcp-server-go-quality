// Package preflight holds the setup and dispatch glue that both entrypoints
// share: turning a tool selection plus a config into runnable checkers, and
// making sure those checkers' binaries are installed before a run starts.
//
// It exists so the CLI and the MCP server cannot drift in how they decide what
// to run or which versions to install.
package preflight

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/afshinator/mcp-server-go-quality/internal/checkers"
	"github.com/afshinator/mcp-server-go-quality/internal/config"
	"github.com/afshinator/mcp-server-go-quality/internal/discover"
	"github.com/afshinator/mcp-server-go-quality/internal/root"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
)

// BuildHandlers turns a list of tool names into checkers configured from cfg.
// Workspace modules are resolved from projectRoot so nilaway can be scoped to
// them.
func BuildHandlers(toolNames []string, cfg config.Config, projectRoot string, binDir string) []checkers.Checker {
	workspaceModules, _ := root.WorkspaceModules(projectRoot)

	var handlers []checkers.Checker
	for _, name := range toolNames {
		switch name {
		case toolname.GolangciLint:
			h := checkers.NewGolangciLintHandler(binDir)
			if tc, ok := cfg.Tools[name]; ok {
				h.ExtraArgs = tc.ExtraArgs
			}
			handlers = append(handlers, h)
		case toolname.Govulncheck:
			h := &checkers.GovulncheckHandler{
				BinDir:           binDir,
				WorkspaceModules: workspaceModules,
			}
			if tc, ok := cfg.Tools[name]; ok {
				h.ExtraArgs = tc.ExtraArgs
			}
			handlers = append(handlers, h)
		case toolname.Nilaway:
			var includePkgs string
			if len(workspaceModules) > 0 {
				includePkgs = strings.Join(workspaceModules, ",")
			}
			h := &checkers.NilawayHandler{
				BinDir:      binDir,
				IncludePkgs: includePkgs,
			}
			if tc, ok := cfg.Tools[name]; ok {
				h.ExtraArgs = tc.ExtraArgs
			}
			handlers = append(handlers, h)
		}
	}
	return handlers
}

// EnsureToolsAvailable installs any tool whose pinned version is missing, so a
// run does not fail on a cold machine. It is a fast no-op when everything is
// already present.
func EnsureToolsAvailable(ctx context.Context, toolNames []string, cfg config.Config, binDir string, versionCache *discover.Cache) error {
	for _, name := range toolNames {
		tc, ok := cfg.Tools[name]
		if !ok {
			return fmt.Errorf("tool %q not found in config (this is an internal error — report it)", name)
		}
		result, err := discover.EnsureInstalled(ctx, versionCache, binDir, name,
			toolname.ModulePath(name), toolname.InstallPath(name), tc.Version)
		if err != nil {
			return fmt.Errorf("installing %s: %w", name, err)
		}
		if result.NewlyInstalled {
			log.Printf("[pre-flight] installed %s@%s", name, result.Version)
		}
	}
	return nil
}

// ToolEntry is one tool at one version, as reported by an install.
type ToolEntry struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
}

// FailedEntry is a tool that could not be installed.
type FailedEntry struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	Command string `json:"command"`
	Stderr  string `json:"stderr"`
}

// InstallResult is the payload returned by an install run. The three slices are
// always non-nil so the JSON carries `[]` rather than `null`.
type InstallResult struct {
	Installed      []ToolEntry   `json:"installed"`
	AlreadyPresent []ToolEntry   `json:"already_present"`
	Failed         []FailedEntry `json:"failed"`
}

// Normalize replaces nil slices with empty ones, so an install payload is
// marshalled with `[]` and never `null`.
func (r InstallResult) Normalize() InstallResult {
	if r.Installed == nil {
		r.Installed = []ToolEntry{}
	}
	if r.AlreadyPresent == nil {
		r.AlreadyPresent = []ToolEntry{}
	}
	if r.Failed == nil {
		r.Failed = []FailedEntry{}
	}
	return r
}
