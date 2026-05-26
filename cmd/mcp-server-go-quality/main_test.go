package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/afshinator/mcp-server-go-quality/internal/config"
	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/discover"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
	"github.com/mark3labs/mcp-go/mcp"
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

// --- buildHandlers ---

func TestBuildHandlersAllThree(t *testing.T) {
	cfg := config.Default()
	handlers := buildHandlers(toolname.All(), cfg, "/fake/project", "/fake/bin")
	if len(handlers) != 3 {
		t.Errorf("got %d handlers, want 3", len(handlers))
	}
	names := make(map[string]bool)
	for _, h := range handlers {
		names[h.Name()] = true
	}
	for _, name := range toolname.All() {
		if !names[name] {
			t.Errorf("handler for %q not built", name)
		}
	}
}

func TestBuildHandlersSingle(t *testing.T) {
	cfg := config.Default()
	handlers := buildHandlers([]string{toolname.GolangciLint}, cfg, "/fake/project", "/fake/bin")
	if len(handlers) != 1 {
		t.Errorf("got %d handlers, want 1", len(handlers))
	}
	if handlers[0].Name() != toolname.GolangciLint {
		t.Errorf("handler name = %q, want %s", handlers[0].Name(), toolname.GolangciLint)
	}
}

// --- ensureToolsAvailable ---

func TestEnsureToolsAvailableUnknownToolReturnsError(t *testing.T) {
	cfg := config.Default()
	// Remove golangci-lint from config to trigger the error path.
	delete(cfg.Tools, toolname.GolangciLint)

	ctx := context.Background()
	err := ensureToolsAvailable(ctx, []string{toolname.GolangciLint}, cfg, "/fake/bin", discover.NewCache())
	if err == nil {
		t.Error("expected error when tool not in config, got nil")
	}
	if !strings.Contains(err.Error(), "not found in config") {
		t.Errorf("error should mention 'not found in config', got %q", err.Error())
	}
}

// --- marshalDiagnostics ---

func TestMarshalDiagnosticsNilIsEmptyArray(t *testing.T) {
	result, err := marshalDiagnostics(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	// The JSON text content must be "[]", not "null".
	text := extractText(t, result)
	var out []diagnostic.Diagnostic
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if out == nil {
		t.Error("unmarshalled nil — expected empty array []")
	}
	if len(out) != 0 {
		t.Errorf("got %d diagnostics, want 0", len(out))
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
	var out []diagnostic.Diagnostic
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(out) != 1 {
		t.Errorf("got %d diagnostics, want 1", len(out))
	}
	if out[0].Tool != "golangci-lint" {
		t.Errorf("Tool = %q, want golangci-lint", out[0].Tool)
	}
	if out[0].Line != 10 {
		t.Errorf("Line = %d, want 10", out[0].Line)
	}
	if out[0].Message != "unused var" {
		t.Errorf("Message = %q, want 'unused var'", out[0].Message)
	}
}

// --- marshalInstallResult ---

func TestMarshalInstallResultEmpty(t *testing.T) {
	result, err := marshalInstallResult(InstallResult{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	text := extractText(t, result)
	var out InstallResult
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

// --- formatHandlerError (after Task 2 fix: uses errors.Is) ---

func TestFormatHandlerErrorRealErrorWhenCtxDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // context is cancelled, but err is a real tool failure

	realErr := errors.New("Tool command failed with exit code 1. Stderr: syntax error")
	got := formatHandlerError(ctx, 5*time.Minute, realErr)
	if got != realErr.Error() {
		t.Errorf("got %q, want real error string %q (context state must not shadow tool failure)", got, realErr.Error())
	}
}

func TestFormatHandlerErrorDeadlineExceeded(t *testing.T) {
	got := formatHandlerError(context.Background(), 5*time.Minute, context.DeadlineExceeded)
	if got != "timed out after 5m0s" {
		t.Errorf("got %q, want timeout message", got)
	}
}

func TestFormatHandlerErrorCancelled(t *testing.T) {
	got := formatHandlerError(context.Background(), 5*time.Minute, context.Canceled)
	if got != "cancelled" {
		t.Errorf("got %q, want 'cancelled'", got)
	}
}
