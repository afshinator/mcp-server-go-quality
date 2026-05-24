package checkers

import (
	"context"

	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
)

type Checker interface {
	Name() string
	Run(ctx context.Context, r runner.CommandRunner, projectPath string) ([]diagnostic.Diagnostic, error)
}

type runResult struct {
	diagnostics []diagnostic.Diagnostic
}
