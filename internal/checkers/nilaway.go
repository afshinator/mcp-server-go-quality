package checkers

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/pathutil"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
)

type nilawayIssue struct {
	Posn    string `json:"posn"`
	End     string `json:"end"`
	Message string `json:"message"`
}

type nilawayPackageResult struct {
	Nilaway []nilawayIssue `json:"nilaway"`
}

type nilawayOutput map[string]nilawayPackageResult

type NilawayHandler struct {
	BinDir      string
	IncludePkgs string
	ExtraArgs   []string
}

func (h *NilawayHandler) Name() string {
	return toolname.Nilaway
}

func (h *NilawayHandler) Run(ctx context.Context, r runner.CommandRunner, projectPath string) ([]diagnostic.Diagnostic, error) {
	args := []string{"-json", "-pretty-print=false"}
	if h.IncludePkgs != "" {
		args = append(args, "-include-pkgs="+h.IncludePkgs)
	}
	args = append(args, h.ExtraArgs...)
	args = append(args, "./...")

	binary := filepath.Join(h.BinDir, "nilaway")

	output, err := r.Run(ctx, binary, args...)
	if err != nil {
		return nil, fmt.Errorf("nilaway: %w", err)
	}

	return parseNilawayOutput(output, projectPath)
}

func parseNilawayOutput(output []byte, projectRoot string) ([]diagnostic.Diagnostic, error) {
	if len(output) == 0 {
		return nil, fmt.Errorf("unexpected output format from nilaway")
	}

	var raw nilawayOutput
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("unexpected output format from nilaway: %w", err)
	}

	if len(raw) == 0 {
		return []diagnostic.Diagnostic{}, nil
	}

	var diags []diagnostic.Diagnostic
	pkgs := make([]string, 0, len(raw))
	for pkg := range raw {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	for _, pkgName := range pkgs {
		pkg := raw[pkgName]
		for _, issue := range pkg.Nilaway {
			absFile, line, col := parsePosn(issue.Posn)
			if absFile == "" {
				native, _ := json.Marshal(issue)
				diags = append(diags, diagnostic.Diagnostic{
					Tool:   toolname.Nilaway,
					Error:  "unable to parse nilaway position",
					Native: native,
				})
				continue
			}
			file := pathutil.Rel(projectRoot, absFile)

			native, _ := json.Marshal(issue)

			diags = append(diags, diagnostic.Diagnostic{
				Tool:    toolname.Nilaway,
				File:    file,
				Line:    line,
				Column:  col,
				Message: extractFirstSentence(issue.Message),
				Native:  native,
			})
		}
	}
	return diags, nil
}

func parsePosn(posn string) (file string, line int, col int) {
	if posn == "" {
		return "", 0, 0
	}

	lastColon := strings.LastIndex(posn, ":")
	if lastColon < 0 {
		return posn, 0, 0
	}

	secondLastColon := strings.LastIndex(posn[:lastColon], ":")
	if secondLastColon < 0 {
		file = posn[:lastColon]
		lineStr := posn[lastColon+1:]
		line, _ = strconv.Atoi(lineStr)
		return file, line, 0
	}

	colStr := posn[lastColon+1:]
	col, _ = strconv.Atoi(colStr)

	lineStr := posn[secondLastColon+1 : lastColon]
	line, _ = strconv.Atoi(lineStr)

	file = posn[:secondLastColon]
	return file, line, col
}

func extractFirstSentence(s string) string {
	if s == "" {
		return ""
	}

	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\n' {
			return string(runes[:i])
		}
		if runes[i] == '.' && i+2 < len(runes) && runes[i+1] == ' ' && unicode.IsUpper(runes[i+2]) {
			return string(runes[:i+1])
		}
	}
	return s
}
