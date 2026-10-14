package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/afshinator/mcp-server-go-quality/internal/checkers"
	"github.com/afshinator/mcp-server-go-quality/internal/config"
	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/discover"
	"github.com/afshinator/mcp-server-go-quality/internal/preflight"
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

	if err := checkArgs(flag.Args()); err != nil {
		log.Fatalf("%v", err)
	}

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

	s := newServer(cfg, binDir, versionCache)

	log.Printf("[init] mcp-server-go-quality %s ready", version.String())

	if err := mcpserver.ServeStdio(s); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// checkArgs rejects positional arguments. The flag package stops parsing at the
// first non-flag argument, so "mcp-server-go-quality ~/myproject" would silently
// drop the path -- and every flag after it, including --version -- and then serve
// the working directory. The server takes no positional path: it reads
// .go-quality.yaml from its working directory, or the file named by --config.
func checkArgs(args []string) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf("unexpected argument(s): %s. This server takes no positional path; it reads .go-quality.yaml from its working directory. Use --config to point at another file", strings.Join(args, " "))
}

// newServer builds the MCP server with all tools registered. Output schema
// validation is enabled so a result that drifts from a tool's declared
// outputSchema fails loudly instead of reaching the client.
func newServer(cfg config.Config, binDir string, versionCache *discover.Cache) *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer(
		"go-quality",
		version.String(),
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithOutputSchemaValidation(),
	)
	registerTools(s, cfg, binDir, versionCache)
	return s
}

func registerTools(s *mcpserver.MCPServer, cfg config.Config, binDir string, versionCache *discover.Cache) {
	s.AddTool(mcp.NewTool(
		"run_code_checks",
		mcp.WithDescription("Run golangci-lint, govulncheck, and nilaway in parallel against a Go project. Returns {\"diagnostics\": [...]}, a unified array sorted by file:line:column. Use when you want a full quality sweep in one call."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("project_path", mcp.Description("Absolute or relative path to the Go project root. Omit to use the server's working directory.")),
		mcp.WithArray("tools", mcp.Description("Subset of checkers to run. Valid values: golangci-lint, govulncheck, nilaway. Omit to run all three.")),
		diagnosticsOutputSchema(),
	), makeRunAllHandler(cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool(
		"run_lint",
		mcp.WithDescription("Run golangci-lint against a Go project. Returns {\"diagnostics\": [...]} containing lint violations, complexity issues, and security patterns. Use when you need fast lint feedback without running the full checker suite."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("project_path", mcp.Description("Absolute or relative path to the Go project root. Omit to use the server's working directory.")),
		diagnosticsOutputSchema(),
	), makeSingleHandler(toolname.GolangciLint, cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool(
		"run_vuln_check",
		mcp.WithDescription("Run govulncheck against a Go project. Returns {\"diagnostics\": [...]} of known CVEs reachable from your code via call-graph analysis. Use when auditing dependencies for security vulnerabilities."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("project_path", mcp.Description("Absolute or relative path to the Go project root. Omit to use the server's working directory.")),
		diagnosticsOutputSchema(),
	), makeSingleHandler(toolname.Govulncheck, cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool(
		"run_nil_check",
		mcp.WithDescription("Run nilaway against a Go project. Returns {\"diagnostics\": [...]} of potential nil-panic paths detected via inter-procedural static analysis. Use when hardening new code or diagnosing nil-dereference bugs."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("project_path", mcp.Description("Absolute or relative path to the Go project root. Omit to use the server's working directory.")),
		diagnosticsOutputSchema(),
	), makeSingleHandler(toolname.Nilaway, cfg, binDir, versionCache))

	s.AddTool(mcp.NewTool(
		"install_tools",
		mcp.WithDescription("Pre-install golangci-lint, govulncheck, and nilaway with pinned versions into GOBIN. Use once at session start to eliminate first-run latency on quality checks."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithArray("tools", mcp.Description("Subset of tools to install. Valid values: golangci-lint, govulncheck, nilaway. Omit to install all three.")),
		mcp.WithOutputSchema[preflight.InstallResult](),
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

		if err := preflight.EnsureToolsAvailable(ctx, toolNames, cfg, binDir, versionCache); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		r := &runner.ExecRunner{Dir: projectRoot}
		handlers := preflight.BuildHandlers(toolNames, cfg, projectRoot, binDir)

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

		if err := preflight.EnsureToolsAvailable(ctx, []string{toolName}, cfg, binDir, versionCache); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		timeout := cfg.ResolveTimeout()
		toolCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		r := &runner.ExecRunner{Dir: projectRoot}
		handlers := preflight.BuildHandlers([]string{toolName}, cfg, projectRoot, binDir)

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
				Error: checkers.FormatCheckerError(toolCtx, timeout, runErr),
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

		response := preflight.InstallResult{}
		for _, name := range toolNames {
			versionStr := "latest"
			if tc, ok := cfg.Tools[name]; ok && tc.Version != "" {
				versionStr = tc.Version
			}

			versionCache.InvalidateResolved(name) // install_tools always forces fresh resolution
			instResult, err := discover.EnsureInstalled(ctx, versionCache, binDir, name,
				toolname.ModulePath(name), toolname.InstallPath(name), versionStr)
			installCmd := "go install " + toolname.InstallPath(name) + "@" + versionStr
			if err != nil {
				response.Failed = append(response.Failed, preflight.FailedEntry{
					Tool:    name,
					Version: versionStr,
					Command: installCmd,
					Stderr:  err.Error(),
				})
				continue
			}

			entry := preflight.ToolEntry{Tool: name, Version: instResult.Version}
			if instResult.NewlyInstalled {
				response.Installed = append(response.Installed, entry)
			} else {
				response.AlreadyPresent = append(response.AlreadyPresent, entry)
			}
		}

		return marshalInstallResult(response)
	}
}

// diagnosticsOutputSchema declares the output schema for the check tools,
// derived from diagnostic.Response. The generator infers json.RawMessage
// (Diagnostic.Native) as a byte array, so native is widened to any JSON value.
func diagnosticsOutputSchema() mcp.ToolOption {
	return func(t *mcp.Tool) {
		mcp.WithOutputSchema[diagnostic.Response]()(t)
		diag, _ := t.OutputSchema.Properties["diagnostics"].(map[string]any)
		items, _ := diag["items"].(map[string]any)
		props, ok := items["properties"].(map[string]any)
		if !ok {
			// The generated schema no longer has the shape this widening expects
			// (e.g. mcp-go switched to $ref/$defs). Fail loudly rather than ship a
			// schema that rejects every real result.
			panic("mcp-server-go-quality: diagnostics outputSchema item properties not found; " +
				"mcp-go changed its schema generation — see diagnosticsOutputSchema")
		}
		props["native"] = map[string]any{}
	}
}

func marshalDiagnostics(diags []diagnostic.Diagnostic) (*mcp.CallToolResult, error) {
	result, err := mcp.NewToolResultJSON(diagnostic.NewResponse(diags))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling results: %v", err)), nil
	}
	return result, nil
}

func marshalInstallResult(result preflight.InstallResult) (*mcp.CallToolResult, error) {
	res, err := mcp.NewToolResultJSON(result.Normalize())
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling results: %v", err)), nil
	}
	return res, nil
}
