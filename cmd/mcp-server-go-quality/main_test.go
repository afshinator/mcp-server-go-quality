package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/afshinator/mcp-server-go-quality/internal/config"
	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/discover"
	"github.com/afshinator/mcp-server-go-quality/internal/preflight"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// makeRequest constructs a CallToolRequest with the given arguments.
func makeRequest(args map[string]any) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

// extractText pulls the first TextContent.Text from a CallToolResult.
func extractText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatal("no TextContent found in CallToolResult")
	return ""
}

// --- resolveProjectPath ---

func TestResolveProjectPathDefault(t *testing.T) {
	req := makeRequest(nil)
	path, err := resolveProjectPath(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path when no project_path arg")
	}
}

func TestResolveProjectPathExplicit(t *testing.T) {
	req := makeRequest(map[string]any{"project_path": "/some/path"})
	path, err := resolveProjectPath(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/some/path" {
		t.Errorf("path = %q, want /some/path", path)
	}
}

// --- resolveRequestedTools ---

func TestResolveRequestedToolsNilParam(t *testing.T) {
	req := makeRequest(nil) // no "tools" key
	tools, err := resolveRequestedTools(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 3 {
		t.Errorf("got %d tools, want 3 (all)", len(tools))
	}
}

func TestResolveRequestedToolsEmptySlice(t *testing.T) {
	req := makeRequest(map[string]any{"tools": []any{}})
	tools, err := resolveRequestedTools(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 3 {
		t.Errorf("got %d tools, want 3 (all)", len(tools))
	}
}

func TestResolveRequestedToolsSubset(t *testing.T) {
	req := makeRequest(map[string]any{"tools": []any{"golangci-lint", "govulncheck"}})
	tools, err := resolveRequestedTools(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 2 {
		t.Errorf("got %d tools, want 2", len(tools))
	}
	found := make(map[string]bool)
	for _, tool := range tools {
		found[tool] = true
	}
	if !found["golangci-lint"] || !found["govulncheck"] {
		t.Errorf("expected golangci-lint and govulncheck, got %v", tools)
	}
}

func TestResolveRequestedToolsInvalidName(t *testing.T) {
	req := makeRequest(map[string]any{"tools": []any{"unknown-tool"}})
	_, err := resolveRequestedTools(req)
	if err == nil {
		t.Error("expected error for unknown tool name, got nil")
	}
}

// --- marshalDiagnostics ---

func TestMarshalDiagnosticsNilIsEmptyEnvelope(t *testing.T) {
	result, err := marshalDiagnostics(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	// The JSON text content must carry an empty array, not null.
	text := extractText(t, result)
	var out diagnostic.Response
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if out.Diagnostics == nil {
		t.Error("unmarshalled nil — expected empty array []")
	}
	if len(out.Diagnostics) != 0 {
		t.Errorf("got %d diagnostics, want 0", len(out.Diagnostics))
	}
	// structuredContent must carry the same envelope, not just the text block.
	sc, ok := result.StructuredContent.(diagnostic.Response)
	if !ok {
		t.Fatalf("structuredContent = %T, want diagnostic.Response", result.StructuredContent)
	}
	if sc.Diagnostics == nil {
		t.Error("structuredContent diagnostics is nil — expected empty array []")
	}
}

func TestMarshalDiagnosticsWithFindings(t *testing.T) {
	diags := []diagnostic.Diagnostic{
		{Tool: "golangci-lint", File: "main.go", Line: 10, Message: "unused var"},
	}
	result, err := marshalDiagnostics(diags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := extractText(t, result)
	var out diagnostic.Response
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(out.Diagnostics) != 1 {
		t.Errorf("got %d diagnostics, want 1", len(out.Diagnostics))
	}
	if out.Diagnostics[0].Tool != "golangci-lint" {
		t.Errorf("Tool = %q, want golangci-lint", out.Diagnostics[0].Tool)
	}
	if out.Diagnostics[0].Line != 10 {
		t.Errorf("Line = %d, want 10", out.Diagnostics[0].Line)
	}
	if out.Diagnostics[0].Message != "unused var" {
		t.Errorf("Message = %q, want 'unused var'", out.Diagnostics[0].Message)
	}
	sc, ok := result.StructuredContent.(diagnostic.Response)
	if !ok {
		t.Fatalf("structuredContent = %T, want diagnostic.Response", result.StructuredContent)
	}
	if len(sc.Diagnostics) != 1 || sc.Diagnostics[0].Tool != "golangci-lint" {
		t.Errorf("structuredContent = %+v, want 1 golangci-lint diagnostic", sc.Diagnostics)
	}
}

// --- output schema ---

// callTool drives the real server over JSON-RPC so the library's own output
// schema validation runs on the result. It returns the result object.
func callTool(t *testing.T, s *mcpserver.MCPServer, name string) map[string]any {
	t.Helper()
	resp := s.HandleMessage(context.Background(), []byte(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+name+`"}}`,
	))
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshaling response: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshaling response: %v", err)
	}
	result, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in response: %v", out)
	}
	return result
}

// probeResult is the payload every probe handler returns.
func probeResult(t *testing.T) *mcp.CallToolResult {
	t.Helper()
	res, err := marshalDiagnostics([]diagnostic.Diagnostic{{
		Tool:    "golangci-lint",
		File:    "main.go",
		Line:    3,
		Message: "unused var",
		Native:  json.RawMessage(`{"FromLinter":"unused"}`),
	}})
	if err != nil {
		t.Fatalf("marshalDiagnostics: %v", err)
	}
	return res
}

func TestAllToolsDeclareOutputSchema(t *testing.T) {
	s := newServer(config.Default(), "/fake/bin", discover.NewCache())
	names := []string{"run_code_checks", "run_lint", "run_vuln_check", "run_nil_check", "install_tools"}
	for _, name := range names {
		tool := s.GetTool(name)
		if tool == nil {
			t.Fatalf("tool %q is not registered", name)
		}
		if tool.Tool.OutputSchema.Type != "object" {
			t.Errorf("tool %q outputSchema type = %q, want \"object\"", name, tool.Tool.OutputSchema.Type)
		}
		if len(tool.Tool.OutputSchema.Properties) == 0 {
			t.Errorf("tool %q declares no outputSchema properties", name)
		}
	}
}

// TestNativeSchemaIsWidened guards the widening in diagnosticsOutputSchema. The
// probe test catches a regression indirectly; this pins the schema itself.
func TestNativeSchemaIsWidened(t *testing.T) {
	tool := mcp.NewTool("probe", diagnosticsOutputSchema())
	diag, ok := tool.OutputSchema.Properties["diagnostics"].(map[string]any)
	if !ok {
		t.Fatalf("no diagnostics property in outputSchema: %v", tool.OutputSchema.Properties)
	}
	items, ok := diag["items"].(map[string]any)
	if !ok {
		t.Fatalf("no items schema for diagnostics: %v", diag)
	}
	props, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatalf("no properties in diagnostics items: %v", items)
	}
	if _, ok := props["native"].(map[string]any); !ok {
		t.Errorf("native schema = %v, want {} (any JSON value)", props["native"])
	}
}

// TestInstallResultPassesOutputValidation covers the install_tools payload,
// which is never exercised through a real call in tests (it would run go install).
func TestInstallResultPassesOutputValidation(t *testing.T) {
	s := newServer(config.Default(), "/fake/bin", discover.NewCache())
	s.AddTool(mcp.NewTool("install_probe", mcp.WithOutputSchema[preflight.InstallResult]()), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return marshalInstallResult(preflight.InstallResult{
			Installed:      []preflight.ToolEntry{{Tool: "nilaway", Version: "v0.0.0"}},
			AlreadyPresent: []preflight.ToolEntry{{Tool: "golangci-lint", Version: config.GolangciLintVersion}},
			Failed:         []preflight.FailedEntry{{Tool: "govulncheck", Version: "latest", Command: "go install x@latest", Stderr: "boom"}},
		})
	})

	resp := callTool(t, s, "install_probe")
	if isErr, ok := resp["isError"].(bool); ok && isErr {
		t.Fatalf("install_tools payload rejected by its own output schema: %v", resp)
	}
	structured, ok := resp["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("no structuredContent in response: %v", resp)
	}
	for _, key := range []string{"installed", "already_present", "failed"} {
		if _, ok := structured[key].([]any); !ok {
			t.Errorf("structuredContent[%q] = %v, want an array", key, structured[key])
		}
	}
}

// TestConformingResultPassesOutputValidation proves the declared schema
// accepts a real payload and that structuredContent reaches the client.
func TestConformingResultPassesOutputValidation(t *testing.T) {
	s := newServer(config.Default(), "/fake/bin", discover.NewCache())
	s.AddTool(mcp.NewTool("probe", diagnosticsOutputSchema()), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return probeResult(t), nil
	})

	resp := callTool(t, s, "probe")
	if isErr, ok := resp["isError"].(bool); ok && isErr {
		t.Fatalf("conforming result was rejected: %v", resp)
	}
	structured, ok := resp["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("no structuredContent in response: %v", resp)
	}
	diags, ok := structured["diagnostics"].([]any)
	if !ok || len(diags) != 1 {
		t.Fatalf("structuredContent.diagnostics = %v, want 1 entry", structured["diagnostics"])
	}
}

// TestNonConformingResultFailsOutputValidation proves validation is actually
// active — without this, a silently-skipped schema would pass the test above.
func TestNonConformingResultFailsOutputValidation(t *testing.T) {
	s := newServer(config.Default(), "/fake/bin", discover.NewCache())
	s.AddTool(mcp.NewTool("bad_probe", diagnosticsOutputSchema()), func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Bare array: valid under the old contract, invalid as structuredContent.
		return mcp.NewToolResultJSON([]diagnostic.Diagnostic{{Tool: "golangci-lint"}})
	})

	resp := callTool(t, s, "bad_probe")
	if isErr, ok := resp["isError"].(bool); !ok || !isErr {
		t.Fatalf("non-conforming result was accepted; output validation is not active: %v", resp)
	}
	// Pin the failure to schema validation, not some unrelated error.
	var text string
	for _, c := range resp["content"].([]any) {
		if m, ok := c.(map[string]any); ok {
			text += fmt.Sprint(m["text"])
		}
	}
	if !strings.Contains(text, "output schema validation failed") {
		t.Errorf("error is not an output schema failure: %q", text)
	}
}

// --- marshalInstallResult ---

func TestMarshalInstallResultEmpty(t *testing.T) {
	result, err := marshalInstallResult(preflight.InstallResult{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	text := extractText(t, result)
	var out preflight.InstallResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if strings.Contains(text, `"installed":null`) {
		t.Error(`"installed" field serialized as null, want []`)
	}
	if strings.Contains(text, `"already_present":null`) {
		t.Error(`"already_present" field serialized as null, want []`)
	}
	if strings.Contains(text, `"failed":null`) {
		t.Error(`"failed" field serialized as null, want []`)
	}
}

func TestCheckArgsRejectsPositionalArguments(t *testing.T) {
	if err := checkArgs(nil); err != nil {
		t.Errorf("checkArgs(nil) = %v, want nil (no positional args is valid)", err)
	}

	// flag.Parse stops at the first non-flag argument, so a positional path --
	// and any flag after it, including --version -- is silently ignored while
	// the server serves the working directory instead.
	err := checkArgs([]string{"/tmp/myproject"})
	if err == nil {
		t.Fatal("checkArgs must reject a positional path: it would be silently ignored and the wrong directory checked")
	}
	if !strings.Contains(err.Error(), "/tmp/myproject") {
		t.Errorf("error should name the offending argument, got %q", err)
	}
}
