package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/afshinator/mcp-server-go-quality/internal/checkers"
	"github.com/afshinator/mcp-server-go-quality/internal/config"
	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/discover"
	"github.com/afshinator/mcp-server-go-quality/internal/root"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
	"github.com/afshinator/mcp-server-go-quality/internal/version"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func main() {
	configPath := flag.String("config", "", "path to .go-quality.yaml (default: .go-quality.yaml in server CWD)")
	verbose := flag.Bool("verbose", false, "emit diagnostic logging to stderr")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.String())
		os.Exit(0)
	}

	if *verbose {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	} else {
		log.SetOutput(os.Stderr)
		log.SetFlags(0)
	}

	binDir, err := discover.ResolveGoBinDir()
	if err != nil {
		log.Fatalf("resolving Go binary directory: %v", err)
	}
	log.Printf("[init] Go binary directory: %s", binDir)

	versionCache := discover.NewCache()

	var cfg config.Config
	if *configPath != "" {
		cfg, err = config.Load(*configPath)
		if err != nil {
			log.Fatalf("config error: %v", err)
		}
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("getting working directory: %v", err)
		}
		cfg, err = config.Load(filepath.Join(cwd, ".go-quality.yaml"))
		if err != nil {
			log.Fatalf("config error: %v", err)
		}
	}

	s := mcpserver.NewMCPServer(
		"go-quality",
		version.String(),
		mcpserver.WithToolCapabilities(true),
	)

	registerTools(s, cfg, binDir, versionCache)

	log.Printf("[init] mcp-server-go-quality %s ready", version.String())

	if err := mcpserver.ServeStdio(s); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func registerTools(s *mcpserver.MCPServer, cfg config.Config, binDir string, versionCache *discover.Cache) {
	s.AddTool(mcp.NewTool(
		"run_code_checks",
		mcp.WithDescription("Run Go code quality checks in parallel. Returns unified diagnostics with file, line, message, and native tool output."),
		mcp.WithString("project_path", mcp.Description("Path to Go project root (default: server CWD)")),
		mcp.WithArray("tools", mcp.Description("Subset of checkers to run. Valid: golangci-lint, govulncheck, nilaway. Omit for all three.")),
	), makeRunAllHandler(cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool(
		"run_lint",
		mcp.WithDescription("Run golangci-lint only. Returns lint violations, complexity, and security pattern issues."),
		mcp.WithString("project_path", mcp.Description("Path to Go project root (default: server CWD)")),
	), makeSingleHandler(toolname.GolangciLint, cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool(
		"run_vuln_check",
		mcp.WithDescription("Run govulncheck only. Returns known CVEs in the dependency graph via call-graph analysis."),
		mcp.WithString("project_path", mcp.Description("Path to Go project root (default: server CWD)")),
	), makeSingleHandler(toolname.Govulncheck, cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool(
		"run_nil_check",
		mcp.WithDescription("Run nilaway only. Returns potential nil panics detected via static analysis."),
		mcp.WithString("project_path", mcp.Description("Path to Go project root (default: server CWD)")),
	), makeSingleHandler(toolname.Nilaway, cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool(
		"install_tools",
		mcp.WithDescription("Pre-install required Go quality tools with pinned versions. Call at session start."),
		mcp.WithArray("tools", mcp.Description("Subset of tools to install. Valid: golangci-lint, govulncheck, nilaway. Omit for all three.")),
	), makeInstallHandler(cfg, binDir, versionCache))
}

func resolveProjectPath(request mcp.CallToolRequest) (string, error) {
	if path := request.GetString("project_path", ""); path != "" {
		return path, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting current working directory: %w", err)
	}
	return cwd, nil
}

func resolveRequestedTools(request mcp.CallToolRequest) ([]string, error) {
	tools := request.GetStringSlice("tools", nil)
	if tools == nil {
		return toolname.All(), nil
	}

	if len(tools) == 0 {
		return toolname.All(), nil
	}

	for _, s := range tools {
		if !toolname.IsValid(s) {
			return nil, fmt.Errorf("unknown tool: %q. valid values: golangci-lint, govulncheck, nilaway", s)
		}
	}
	return tools, nil
}

func buildHandlers(toolNames []string, cfg config.Config, projectRoot string, binDir string) []checkers.Checker {
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

func ensureToolsAvailable(ctx context.Context, toolNames []string, cfg config.Config, binDir string, versionCache *discover.Cache) error {
	for _, name := range toolNames {
		tc, ok := cfg.Tools[name]
		if !ok {
			continue
		}
		result, err := discover.EnsureInstalled(ctx, versionCache, binDir, name,
			resolveModulePath(name), toolname.InstallPath(name), tc.Version)
		if err != nil {
			return fmt.Errorf("installing %s: %w", name, err)
		}
		if result.NewlyInstalled {
			log.Printf("[pre-flight] installed %s@%s", name, result.Version)
		}
	}
	return nil
}

func makeRunAllHandler(cfg config.Config, binDir string, versionCache *discover.Cache) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		projectPath, err := resolveProjectPath(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		projectRoot, err := root.Discover(projectPath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		toolNames, err := resolveRequestedTools(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if err := ensureToolsAvailable(ctx, toolNames, cfg, binDir, versionCache); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		r := &runner.ExecRunner{Dir: projectRoot}
		handlers := buildHandlers(toolNames, cfg, projectRoot, binDir)

		diags := checkers.RunAllChecks(ctx, r, handlers, projectRoot, cfg.ResolveTimeout())

		return marshalDiagnostics(diags)
	}
}

func makeSingleHandler(toolName string, cfg config.Config, binDir string, versionCache *discover.Cache) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		projectPath, err := resolveProjectPath(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		projectRoot, err := root.Discover(projectPath)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if err := ensureToolsAvailable(ctx, []string{toolName}, cfg, binDir, versionCache); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		timeout := cfg.ResolveTimeout()
		toolCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		r := &runner.ExecRunner{Dir: projectRoot}
		handlers := buildHandlers([]string{toolName}, cfg, projectRoot, binDir)

		var (
			diags  []diagnostic.Diagnostic
			runErr error
		)

		if len(handlers) > 0 {
			diags, runErr = handlers[0].Run(toolCtx, r, projectRoot)
		}

		if runErr != nil {
			diags = []diagnostic.Diagnostic{{
				Tool:  toolName,
				Error: formatHandlerError(toolCtx, timeout, runErr),
			}}
		}

		return marshalDiagnostics(diags)
	}
}

func makeInstallHandler(cfg config.Config, binDir string, versionCache *discover.Cache) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		toolNames, err := resolveRequestedTools(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		response := InstallResult{}
		for _, name := range toolNames {
			versionStr := "latest"
			if name == toolname.GolangciLint {
				versionStr = "v2.11.4"
			}
			if tc, ok := cfg.Tools[name]; ok && tc.Version != "" {
				versionStr = tc.Version
			}

			instResult, err := discover.EnsureInstalled(ctx, versionCache, binDir, name,
				resolveModulePath(name), toolname.InstallPath(name), versionStr)
			installCmd := "go install " + toolname.InstallPath(name) + "@" + versionStr
			if err != nil {
				response.Failed = append(response.Failed, FailedEntry{
					Tool:    name,
					Version: versionStr,
					Command: installCmd,
					Stderr:  err.Error(),
				})
				continue
			}

			entry := ToolEntry{Tool: name, Version: instResult.Version}
			if instResult.NewlyInstalled {
				response.Installed = append(response.Installed, entry)
			} else {
				response.AlreadyPresent = append(response.AlreadyPresent, entry)
			}
		}

		return marshalInstallResult(response)
	}
}

type ToolEntry struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
}

type FailedEntry struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	Command string `json:"command"`
	Stderr  string `json:"stderr"`
}

type InstallResult struct {
	Installed      []ToolEntry   `json:"installed"`
	AlreadyPresent []ToolEntry   `json:"already_present"`
	Failed         []FailedEntry `json:"failed"`
}

func resolveModulePath(toolName string) string {
	switch toolName {
	case toolname.GolangciLint:
		return "github.com/golangci/golangci-lint/v2"
	case toolname.Govulncheck:
		return "golang.org/x/vuln"
	case toolname.Nilaway:
		return "go.uber.org/nilaway"
	default:
		return ""
	}
}

func formatHandlerError(ctx context.Context, timeout time.Duration, err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("timed out after %s", timeout)
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return err.Error()
}

func marshalDiagnostics(diags []diagnostic.Diagnostic) (*mcp.CallToolResult, error) {
	if diags == nil {
		diags = []diagnostic.Diagnostic{}
	}
	result, err := mcp.NewToolResultJSON(diags)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling results: %v", err)), nil
	}
	return result, nil
}

func marshalInstallResult(result InstallResult) (*mcp.CallToolResult, error) {
	res, err := mcp.NewToolResultJSON(result)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling results: %v", err)), nil
	}
	return res, nil
}
