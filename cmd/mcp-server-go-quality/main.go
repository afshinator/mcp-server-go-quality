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
		cfg, err = config.LoadRequired(*configPath)
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
		mcp.WithDescription("Run golangci-lint, govulncheck, and nilaway in parallel against a Go project. Returns unified Diagnostic[] sorted by file:line:column. Use when you want a full quality sweep in one call."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("project_path", mcp.Description("Absolute or relative path to the Go project root. Omit to use the server's working directory.")),
		mcp.WithArray("tools", mcp.Description("Subset of checkers to run. Valid values: golangci-lint, govulncheck, nilaway. Omit to run all three.")),
	), makeRunAllHandler(cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool(
		"run_lint",
		mcp.WithDescription("Run golangci-lint against a Go project. Returns lint violations, complexity issues, and security patterns as Diagnostic[]. Use when you need fast lint feedback without running the full checker suite."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("project_path", mcp.Description("Absolute or relative path to the Go project root. Omit to use the server's working directory.")),
	), makeSingleHandler(toolname.GolangciLint, cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool(
		"run_vuln_check",
		mcp.WithDescription("Run govulncheck against a Go project. Returns known CVEs reachable from your code via call-graph analysis as Diagnostic[]. Use when auditing dependencies for security vulnerabilities."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("project_path", mcp.Description("Absolute or relative path to the Go project root. Omit to use the server's working directory.")),
	), makeSingleHandler(toolname.Govulncheck, cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool(
		"run_nil_check",
		mcp.WithDescription("Run nilaway against a Go project. Returns potential nil-panic paths detected via inter-procedural static analysis as Diagnostic[]. Use when hardening new code or diagnosing nil-dereference bugs."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("project_path", mcp.Description("Absolute or relative path to the Go project root. Omit to use the server's working directory.")),
	), makeSingleHandler(toolname.Nilaway, cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool(
		"install_tools",
		mcp.WithDescription("Pre-install golangci-lint, govulncheck, and nilaway with pinned versions into GOBIN. Use once at session start to eliminate first-run latency on quality checks."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithArray("tools", mcp.Description("Subset of tools to install. Valid values: golangci-lint, govulncheck, nilaway. Omit to install all three.")),
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
			return fmt.Errorf("tool %q not found in config (this is an internal error — report it)", name)
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
			if tc, ok := cfg.Tools[name]; ok && tc.Version != "" {
				versionStr = tc.Version
			}

			versionCache.InvalidateResolved(name) // install_tools always forces fresh resolution
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
	if result.Installed == nil {
		result.Installed = []ToolEntry{}
	}
	if result.AlreadyPresent == nil {
		result.AlreadyPresent = []ToolEntry{}
	}
	if result.Failed == nil {
		result.Failed = []FailedEntry{}
	}
	res, err := mcp.NewToolResultJSON(result)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling results: %v", err)), nil
	}
	return res, nil
}
