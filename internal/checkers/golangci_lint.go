package checkers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/pathutil"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
)

type golangciLintIssue struct {
	FromLinter string `json:"FromLinter"`
	Text       string `json:"Text"`
	Severity   string `json:"Severity"`
	Pos        struct {
		Filename string `json:"Filename"`
		Line     int    `json:"Line"`
		Column   int    `json:"Column"`
	} `json:"Pos"`
}

type golangciLintOutput struct {
	Issues []golangciLintIssue `json:"Issues"`
}

type GolangciLintHandler struct {
	BinDir    string
	ExtraArgs []string
}

func NewGolangciLintHandler(binDir string) *GolangciLintHandler {
	return &GolangciLintHandler{BinDir: binDir}
}

func (h *GolangciLintHandler) Name() string {
	return toolname.GolangciLint
}

func (h *GolangciLintHandler) Run(ctx context.Context, r runner.CommandRunner, projectPath string) ([]diagnostic.Diagnostic, error) {
	args := []string{"run", "--output.text.path", "stderr", "--output.json.path", "stdout"}
	args = append(args, h.ExtraArgs...)
	args = append(args, "./...")
	binary := filepath.Join(h.BinDir, "golangci-lint")
	output, exitErr := r.Run(ctx, binary, args...)

	diags, parseErr := parseGolangciLintOutput(output, projectPath)
	if parseErr != nil {
		return nil, parseErr
	}

	if exitErr != nil && len(diags) == 0 {
		return nil, exitErr
	}

	return diags, nil
}

func parseGolangciLintOutput(output []byte, projectRoot string) ([]diagnostic.Diagnostic, error) {
	var result golangciLintOutput
	decoder := json.NewDecoder(bytes.NewReader(output))
	if err := decoder.Decode(&result); err != nil && err != io.EOF {
		return nil, fmt.Errorf("unexpected output format from golangci-lint: %w", err)
	}

	diags := make([]diagnostic.Diagnostic, 0, len(result.Issues))
	for _, issue := range result.Issues {
		file := pathutil.Rel(projectRoot, issue.Pos.Filename)

		native, err := json.Marshal(issue)
		if err != nil {
			native = nil
		}

		diags = append(diags, diagnostic.Diagnostic{
			Tool:     toolname.GolangciLint,
			File:     file,
			Line:     issue.Pos.Line,
			Column:   issue.Pos.Column,
			Severity: issue.Severity,
			Message:  issue.Text,
			Native:   native,
		})
	}
	return diags, nil
}
