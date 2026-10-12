// Example MCP client for mcp-server-go-quality.
//
// Build and run:
//
//	go run examples/client.go testdata/sample_project   # from the repo root
//
// This demonstrates connecting to the server over stdio, listing tools,
// installing quality tools, and calling run_code_checks against a Go project.

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: go run examples/client.go <project-path>\n")
		os.Exit(1)
	}
	projectPath := os.Args[1]

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	stdio := transport.NewStdio(
		"go",
		nil,
		"run", "./cmd/mcp-server-go-quality",
	)

	c := client.NewClient(
		stdio,
		client.WithClientCapabilities(mcp.ClientCapabilities{}),
	)
	defer func() {
		if err := c.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to close client: %v\n", err)
		}
	}()

	if err := c.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start client: %v\n", err)
		os.Exit(1)
	}

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "example-client", Version: "1.0.0"}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		fmt.Fprintf(os.Stderr, "initialize failed: %v\n", err)
		os.Exit(1)
	}

	// Install tools (fast no-op if already at correct version).
	installReq := mcp.CallToolRequest{}
	installReq.Params.Name = "install_tools"
	if _, err := c.CallTool(ctx, installReq); err != nil {
		fmt.Fprintf(os.Stderr, "install_tools failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Tools installed.")

	// Run code checks.
	callReq := mcp.CallToolRequest{}
	callReq.Params.Name = "run_code_checks"
	callReq.Params.Arguments = map[string]any{"project_path": projectPath}
	result, err := c.CallTool(ctx, callReq)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run_code_checks failed: %v\n", err)
		os.Exit(1)
	}

	// The result carries the diagnostics envelope as structuredContent (and a
	// JSON text fallback for clients that do not read structuredContent).
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		fmt.Fprintf(os.Stderr, "unexpected result shape: %T\n", result.StructuredContent)
		os.Exit(1)
	}
	diags, ok := structured["diagnostics"].([]any)
	if !ok {
		fmt.Fprintf(os.Stderr, "missing diagnostics array in result\n")
		os.Exit(1)
	}
	if len(diags) == 0 {
		fmt.Println("No issues found.")
		return
	}
	for _, entry := range diags {
		d, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if errMsg, ok := d["error"].(string); ok && errMsg != "" {
			fmt.Printf("[%s] ERROR: %s\n", d["tool"], errMsg)
			continue
		}
		severity := ""
		if s, ok := d["severity"].(string); ok && s != "" {
			severity = fmt.Sprintf(" (%s)", s)
		}
		fmt.Printf("[%s]%s %s:%v — %s\n", d["tool"], severity, d["file"], d["line"], d["message"])
	}
}
