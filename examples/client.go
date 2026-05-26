// Example MCP client for mcp-server-go-quality.
//
// Build and run:
//
//	go run examples/client.go testdata/sample_project
//
// This demonstrates connecting to the server over stdio, listing tools,
// installing quality tools, and calling run_code_checks against a Go project.

package main

import (
	"context"
	"encoding/json"
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
		"go-quality",
		nil,
		"go", "run", "github.com/afshinator/mcp-server-go-quality/cmd/mcp-server-go-quality@latest",
	)

	c := client.NewClient(
		stdio,
		client.WithClientCapabilities(mcp.ClientCapabilities{}),
	)
	defer c.Close()

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

	for _, content := range result.Content {
		if text, ok := content.(mcp.TextContent); ok {
			var diags []map[string]any
			if err := json.Unmarshal([]byte(text.Text), &diags); err != nil {
				fmt.Fprintf(os.Stderr, "failed to parse diagnostics: %v\n", err)
				os.Exit(1)
			}
			if len(diags) == 0 {
				fmt.Println("No issues found.")
				return
			}
			for _, d := range diags {
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
	}
}
