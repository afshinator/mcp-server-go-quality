package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/mark3labs/mcp-go/mcp"
)

// --- the --fail-on trip table -------------------------------------------------

// TestTripsThresholdTable pins the whole table. `any` and `none` are not points
// on the severity axis, so they are asserted separately from the ranked tiers.
func TestTripsThresholdTable(t *testing.T) {
	tests := []struct {
		threshold string
		severity  string
		want      bool
	}{
		// any: severity-agnostic, trips on everything.
		{"any", "error", true},
		{"any", "warning", true},

		// none: trips on nothing at all.
		{"none", "error", false},
		{"none", "warning", false},

		// error: only error.
		{"error", "error", true},
		{"error", "warning", false},

		// warning: a threshold, so it trips on error too.
		{"warning", "error", true},
		{"warning", "warning", true},

		// an unknown severity must not be able to bypass a gate.
		{"error", "info", true},
		{"error", "", true},
		{"warning", "critical", true},
	}
	for _, tc := range tests {
		name := tc.threshold + "/" + tc.severity
		if tc.severity == "" {
			name = tc.threshold + "/<empty>"
		}
		t.Run(name, func(t *testing.T) {
			if got := Trips(tc.threshold, tc.severity); got != tc.want {
				t.Errorf("Trips(%q, %q) = %v, want %v", tc.threshold, tc.severity, got, tc.want)
			}
		})
	}
}

// TestTripsWarningTripsOnErrorSeverity is called out on its own because the
// obvious implementation (`severity == threshold`) fails it, and the failure is
// silent: `--fail-on=warning` would let an error-severity finding through.
func TestTripsWarningTripsOnErrorSeverity(t *testing.T) {
	if !Trips("warning", "error") {
		t.Error("--fail-on=warning must trip on an error-severity finding; it is a threshold, not an equality")
	}
}

// --- exit codes ---------------------------------------------------------------

func finding(severity string) diagnostic.Diagnostic {
	return diagnostic.Diagnostic{Tool: "golangci-lint", File: "main.go", Line: 1, Severity: severity, Message: "m"}
}

func failure() diagnostic.Diagnostic {
	return diagnostic.Diagnostic{Tool: "govulncheck", Error: "timed out after 5m0s"}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		name      string
		diags     []diagnostic.Diagnostic
		threshold string
		want      int
	}{
		{"clean", nil, "any", 0},
		{"empty slice", []diagnostic.Diagnostic{}, "any", 0},
		{"one finding, any", []diagnostic.Diagnostic{finding("warning")}, "any", 1},
		{"one finding, none", []diagnostic.Diagnostic{finding("error")}, "none", 0},
		{"error finding, error tier", []diagnostic.Diagnostic{finding("error")}, "error", 1},
		{"warning finding, error tier", []diagnostic.Diagnostic{finding("warning")}, "error", 0},
		{"tool failure, any", []diagnostic.Diagnostic{failure()}, "any", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCode(tc.diags, tc.threshold); got != tc.want {
				t.Errorf("ExitCode(%v, %q) = %d, want %d", tc.diags, tc.threshold, got, tc.want)
			}
		})
	}
}

// TestExitCodeToolFailureOutranksEverything is the T12 contract: a checker that
// failed to run is not a clean result, so it must not be silenceable.
func TestExitCodeToolFailureOutranksEverything(t *testing.T) {
	diags := []diagnostic.Diagnostic{finding("error"), failure()}
	for _, threshold := range failOnValues {
		if got := ExitCode(diags, threshold); got != exitFailure {
			t.Errorf("ExitCode(finding+failure, %q) = %d, want %d: a tool failure must be unconditional", threshold, got, exitFailure)
		}
	}
	// ...and a failure alone must not be reported as a finding.
	if got := ExitCode([]diagnostic.Diagnostic{failure()}, "none"); got != exitFailure {
		t.Errorf("ExitCode(failure, none) = %d, want %d", got, exitFailure)
	}
}

// --- envelope parity with the MCP server --------------------------------------

// TestJSONEnvelopeMatchesMCP is the contract Decision 2 rests on: the CLI's
// --json payload must be byte-identical to what the MCP tools put in their text
// block (and therefore in structuredContent). Both paths build the same
// diagnostic.Response, and this asserts the bytes rather than trusting that.
func TestJSONEnvelopeMatchesMCP(t *testing.T) {
	diags := []diagnostic.Diagnostic{
		{Tool: "golangci-lint", File: "cmd/main.go", Line: 115, Column: 1, Severity: "warning", Message: "complexity"},
		{Tool: "govulncheck", File: "main.go", Line: 3, Severity: "error", Message: "CVE", Native: json.RawMessage(`{"osv":"GO-2026-1"}`)},
		{Tool: "nilaway", Error: "timed out after 5m0s"},
	}

	cliBytes, err := MarshalJSON(diags)
	if err != nil {
		t.Fatal(err)
	}

	mcpResult, err := mcp.NewToolResultJSON(diagnostic.NewResponse(diags))
	if err != nil {
		t.Fatal(err)
	}
	var mcpText string
	for _, c := range mcpResult.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			mcpText = tc.Text
		}
	}
	if mcpText == "" {
		t.Fatal("no text content in the MCP result")
	}

	if string(cliBytes) != mcpText {
		t.Errorf("CLI --json and MCP payload differ\n CLI: %s\n MCP: %s", cliBytes, mcpText)
	}
	if !bytes.Equal(cliBytes, []byte(mcpText)) {
		t.Error("byte comparison disagrees with string comparison")
	}
}

// TestJSONEnvelopeShape guards the envelope itself, so a consumer's
// `jq '.diagnostics'` keeps working.
func TestJSONEnvelopeShape(t *testing.T) {
	for _, tc := range []struct {
		name  string
		diags []diagnostic.Diagnostic
	}{
		{"nil becomes an empty array", nil},
		{"empty stays an empty array", []diagnostic.Diagnostic{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := MarshalJSON(tc.diags)
			if err != nil {
				t.Fatal(err)
			}
			want := `{"diagnostics":[]}`
			if string(b) != want {
				t.Errorf("MarshalJSON = %s, want %s (null would break jq consumers)", b, want)
			}
		})
	}
}

// --- flag handling ------------------------------------------------------------

func TestParseFlagsFailOnValidation(t *testing.T) {
	if _, err := parseFlags([]string{"--fail-on=bogus"}, &bytes.Buffer{}); err == nil {
		t.Error("expected an error for an unknown --fail-on value")
	}
	for _, v := range failOnValues {
		if _, err := parseFlags([]string{"--fail-on=" + v}, &bytes.Buffer{}); err != nil {
			t.Errorf("--fail-on=%s should be accepted: %v", v, err)
		}
	}
}

func TestParseFlagsProjectPath(t *testing.T) {
	o, err := parseFlags([]string{"/some/project"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if o.projectPath != "/some/project" {
		t.Errorf("projectPath = %q", o.projectPath)
	}
	if o, _ := parseFlags(nil, &bytes.Buffer{}); o.projectPath != "." {
		t.Errorf("default projectPath = %q, want .", o.projectPath)
	}
	if _, err := parseFlags([]string{"a", "b"}, &bytes.Buffer{}); err == nil {
		t.Error("expected an error for two project paths")
	}
}

func TestRequestedTools(t *testing.T) {
	all, err := requestedTools("")
	if err != nil || len(all) != 3 {
		t.Errorf("empty selection should resolve to all tools, got %v (%v)", all, err)
	}
	sub, err := requestedTools("golangci-lint, nilaway")
	if err != nil || len(sub) != 2 {
		t.Errorf("got %v (%v), want two tools", sub, err)
	}
	if _, err := requestedTools("not-a-tool"); err == nil {
		t.Error("expected an error for an unknown tool")
	}
}

// TestRunRejectsBadFailOn confirms a bad flag is exit 2 and never reaches the
// checkers.
func TestRunRejectsBadFailOn(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--fail-on=bogus"}, &stdout, &stderr); code != exitFailure {
		t.Errorf("exit = %d, want %d", code, exitFailure)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay empty on a flag error, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "fail-on") {
		t.Errorf("stderr should name the bad flag, got %q", stderr.String())
	}
}

// TestRunProjectRootFailure confirms a missing project is exit 2 with nothing on
// stdout, which is what keeps `--json | jq` safe.
func TestRunProjectRootFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", t.TempDir()}, &stdout, &stderr)
	if code != exitFailure {
		t.Errorf("exit = %d, want %d", code, exitFailure)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay empty when the run cannot start, got %q", stdout.String())
	}
}

// --- text rendering -----------------------------------------------------------

func TestRenderText(t *testing.T) {
	got := renderText([]diagnostic.Diagnostic{
		{Tool: "golangci-lint", File: "cmd/main.go", Line: 115, Column: 1, Severity: "warning", Message: "complexity"},
		{Tool: "govulncheck", File: "main.go", Line: 3, Severity: "error", Message: "CVE"},
		{Tool: "nilaway", Error: "timed out after 5m0s"},
	})
	want := "cmd/main.go:115:1  golangci-lint  warning  complexity\n" +
		"main.go:3  govulncheck  error  CVE\n" +
		"nilaway: FAILED: timed out after 5m0s\n"
	if got != want {
		t.Errorf("renderText() =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderTextEmpty(t *testing.T) {
	if got := renderText(nil); got != "" {
		t.Errorf("renderText(nil) = %q, want empty", got)
	}
}
