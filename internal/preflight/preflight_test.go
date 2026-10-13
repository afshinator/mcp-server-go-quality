package preflight

import (
	"context"
	"strings"
	"testing"

	"github.com/afshinator/mcp-server-go-quality/internal/config"
	"github.com/afshinator/mcp-server-go-quality/internal/discover"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
)

func TestBuildHandlersAllThree(t *testing.T) {
	cfg := config.Default()
	handlers := BuildHandlers(toolname.All(), cfg, "/fake/project", "/fake/bin")
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
	handlers := BuildHandlers([]string{toolname.GolangciLint}, cfg, "/fake/project", "/fake/bin")
	if len(handlers) != 1 {
		t.Errorf("got %d handlers, want 1", len(handlers))
	}
	if handlers[0].Name() != toolname.GolangciLint {
		t.Errorf("handler name = %q, want %s", handlers[0].Name(), toolname.GolangciLint)
	}
}

func TestEnsureToolsAvailableUnknownToolReturnsError(t *testing.T) {
	cfg := config.Default()
	// Remove golangci-lint from config to trigger the error path.
	delete(cfg.Tools, toolname.GolangciLint)

	ctx := context.Background()
	err := EnsureToolsAvailable(ctx, []string{toolname.GolangciLint}, cfg, "/fake/bin", discover.NewCache())
	if err == nil {
		t.Error("expected error when tool not in config, got nil")
	}
	if !strings.Contains(err.Error(), "not found in config") {
		t.Errorf("error should mention 'not found in config', got %q", err.Error())
	}
}
