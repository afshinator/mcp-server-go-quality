package checkers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/pathutil"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
)

const (
	vulnDBLockPhrase = "database is locked"
	maxVulnRetries   = 3
	vulnRetryBackoff = 2 * time.Second
)

type positionJSON struct {
	Filename string `json:"filename"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

type traceEntryJSON struct {
	Module   string        `json:"module"`
	Version  string        `json:"version"`
	Package  string        `json:"package"`
	Function string        `json:"function"`
	Position *positionJSON `json:"position,omitempty"`
}

type findingJSON struct {
	OSV          string           `json:"osv"`
	FixedVersion string           `json:"fixed_version"`
	Trace        []traceEntryJSON `json:"trace"`
}

type osvJSON struct {
	ID      string   `json:"id"`
	Summary string   `json:"summary"`
	Aliases []string `json:"aliases"`
}

type GovulncheckHandler struct {
	BinDir           string
	WorkspaceModules []string
	ExtraArgs        []string
}

func (h *GovulncheckHandler) Name() string {
	return toolname.Govulncheck
}

func (h *GovulncheckHandler) Run(ctx context.Context, r runner.CommandRunner, projectPath string) ([]diagnostic.Diagnostic, error) {
	args := []string{"-json"}
	args = append(args, h.ExtraArgs...)
	args = append(args, "./...")

	binary := filepath.Join(h.BinDir, "govulncheck")

	var output []byte
	var exitErr error

	for attempt := 0; attempt < maxVulnRetries; attempt++ {
		output, exitErr = r.Run(ctx, binary, args...)

		if exitErr != nil {
			stderr, _ := runner.Stderr(exitErr)
			code, _ := runner.ExitCode(exitErr)
			if code == 1 && strings.Contains(stderr, vulnDBLockPhrase) {
				select {
				case <-time.After(vulnRetryBackoff):
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		}
		break
	}

	diags, parseErr := parseGovulncheckOutput(output, projectPath, h.WorkspaceModules)
	if parseErr != nil {
		return []diagnostic.Diagnostic{{
			Tool:  toolname.Govulncheck,
			Error: fmt.Sprintf("unexpected output format from govulncheck: %v", parseErr),
		}}, nil
	}

	if exitErr != nil && len(diags) == 0 {
		diags = append(diags, diagnostic.Diagnostic{
			Tool:  toolname.Govulncheck,
			Error: exitErr.Error(),
		})
	}

	return diags, nil
}

type govulnLine struct {
	OSV     *osvJSON     `json:"osv,omitempty"`
	Finding *findingJSON `json:"finding,omitempty"`
}

type GovulncheckNativeContainer struct {
	Finding json.RawMessage `json:"finding"`
	OSV     json.RawMessage `json:"osv"`
}

func NativeContainer(native json.RawMessage) *GovulncheckNativeContainer {
	if native == nil {
		return nil
	}
	var c GovulncheckNativeContainer
	if err := json.Unmarshal(native, &c); err != nil {
		return nil
	}
	return &c
}

func parseGovulncheckOutput(output []byte, projectRoot string, workspaceModules []string) ([]diagnostic.Diagnostic, error) {
	decoder := json.NewDecoder(bytes.NewReader(output))

	osvMap := make(map[string]*osvJSON)
	var findings []findingJSON
	var parseErrors []string

	for {
		var entry govulnLine
		if err := decoder.Decode(&entry); err != nil {
			if err == io.EOF {
				break
			}
			parseErrors = append(parseErrors, err.Error())
			break
		}

		if entry.OSV != nil {
			osvMap[entry.OSV.ID] = entry.OSV
		}
		if entry.Finding != nil {
			findings = append(findings, *entry.Finding)
		}
	}

	diags := make([]diagnostic.Diagnostic, 0, len(findings)+1)

	for _, f := range findings {
		message := f.OSV
		if osv, ok := osvMap[f.OSV]; ok && osv.Summary != "" {
			message = osv.Summary
		}

		entry := chooseTraceEntry(f.Trace, workspaceModules)
		file := ""
		line := 0
		column := 0
		if entry != nil {
			file = pathutil.Rel(projectRoot, entry.Position.Filename)
			line = entry.Position.Line
			column = entry.Position.Column
		}

		findingRaw, err := json.Marshal(f)
		if err != nil {
			findingRaw = nil
		}

		var osvRaw json.RawMessage
		if osv, ok := osvMap[f.OSV]; ok {
			osvRaw, err = json.Marshal(osv)
			if err != nil {
				osvRaw = nil
			}
		}

		container := GovulncheckNativeContainer{
			Finding: findingRaw,
			OSV:     osvRaw,
		}
		native, err := json.Marshal(container)
		if err != nil {
			native = nil
		}

		diags = append(diags, diagnostic.Diagnostic{
			Tool:    toolname.Govulncheck,
			File:    file,
			Line:    line,
			Column:  column,
			Message: message,
			Native:  native,
		})
	}

	if len(parseErrors) > 0 {
		nativeRaw, err := json.Marshal(parseErrors)
		if err != nil {
			nativeRaw = nil
		}
		diags = append(diags, diagnostic.Diagnostic{
			Tool:   toolname.Govulncheck,
			Error:  fmt.Sprintf("failed to parse govulncheck output: %s", parseErrors[0]),
			Native: nativeRaw,
		})
	}

	return diags, nil
}

func chooseTraceEntry(trace []traceEntryJSON, workspaceModules []string) *traceEntryJSON {
	if len(trace) == 0 {
		return nil
	}
	for i := 0; i < len(trace); i++ {
		for _, mod := range workspaceModules {
			if trace[i].Module == mod {
				return &trace[i]
			}
		}
	}
	for i := 0; i < len(trace); i++ {
		if trace[i].Position != nil && trace[i].Position.Filename != "" {
			return &trace[i]
		}
	}
	return nil
}
