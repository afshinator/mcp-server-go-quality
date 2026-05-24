package checkers

import (
	"context"
	"os"
	"testing"

	"github.com/afshinator/mcp-server-go-quality/internal/discover"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
)

func TestParseGovulncheckOutputFindsVulnerabilities(t *testing.T) {
	input := `{"config":{"protocol_version":"v1.0.0"}}
{"osv":{"id":"GO-2026-4918","summary":"Infinite loop in HTTP/2 transport","aliases":["CVE-2026-33814"]}}
{"finding":{"osv":"GO-2026-4918","fixed_version":"v1.25.10","trace":[{"module":"github.com/myorg/myapp","version":"v1.0.0","package":"github.com/myorg/myapp/internal/httpclient","function":"Get","position":{"filename":"internal/httpclient/client.go","line":78,"column":25}},{"module":"stdlib","version":"v1.25.9","package":"net/http","function":"Do","position":{"filename":"src/net/http/client.go","line":586,"column":18}}]}}
{"finding":{"osv":"GO-2026-4971","fixed_version":"v1.25.10","trace":[{"module":"stdlib","version":"v1.25.9","package":"net"}]}}
{"progress":{"message":"done"}}
`
	projectRoot := "/project/myapp"
	workspaceModules := []string{"github.com/myorg/myapp"}

	diags, err := parseGovulncheckOutput([]byte(input), projectRoot, workspaceModules)
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics, want 2", len(diags))
	}

	d0 := diags[0]
	if d0.Tool != toolname.Govulncheck {
		t.Errorf("tool = %q, want %s", d0.Tool, toolname.Govulncheck)
	}
	if d0.File != "internal/httpclient/client.go" {
		t.Errorf("file = %q, want internal/httpclient/client.go", d0.File)
	}
	if d0.Line != 78 {
		t.Errorf("line = %d, want 78", d0.Line)
	}
	if d0.Column != 25 {
		t.Errorf("column = %d, want 25", d0.Column)
	}
	if d0.Message == "" {
		t.Error("message is empty")
	}
	if d0.Severity != "" {
		t.Error("severity should be empty for govulncheck (no severity concept)")
	}

	d1 := diags[1]
	if d1.File != "" {
		t.Errorf("file should be empty for module-level finding without position, got %q", d1.File)
	}

	container := NativeContainer(d0.Native)
	if container == nil {
		t.Fatal("NativeContainer returned nil")
	}
	if container.Finding == nil {
		t.Error("native container should have finding")
	}
	if container.OSV == nil {
		t.Error("native container should have osv")
	}
}

func TestParseGovulncheckOutputMissingOSVMap(t *testing.T) {
	input := `{"finding":{"osv":"UNKNOWN-ID","fixed_version":"v1.25.10","trace":[{"module":"github.com/myorg/myapp","version":"v1.0.0","package":"github.com/myorg/myapp/internal/httpclient","function":"Get","position":{"filename":"internal/httpclient/client.go","line":78,"column":25}}]}}
`
	diags, err := parseGovulncheckOutput([]byte(input), "/project/myapp", []string{"github.com/myorg/myapp"})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diags))
	}
	if diags[0].Message != "UNKNOWN-ID" {
		t.Errorf("message should fall back to OSV ID, got %q", diags[0].Message)
	}
}

func TestParseGovulncheckOutputNDJSONParseError(t *testing.T) {
	input := `{"config":{"protocol_version":"v1.0.0"}}
{"osv":{"id":"GO-2026-4918","summary":"Test vuln"}}
{"finding":{"osv":"GO-2026-4918","fixed_version":"v1.25.10","trace":[{"module":"github.com/myorg/myapp","version":"v1.0.0","package":"github.com/myorg/myapp/internal/httpclient","function":"Get","position":{"filename":"internal/httpclient/client.go","line":78,"column":25}}]}}
{invalid json line here}
{"finding":{"osv":"GO-2026-4999","fixed_version":"v1.25.10","trace":[{"module":"github.com/myorg/myapp","version":"v1.0.0","package":"net/http","function":"Do","position":{"filename":"src/net/http/client.go","line":586,"column":18}}]}}
`

	diags, err := parseGovulncheckOutput([]byte(input), "/project/myapp", []string{"github.com/myorg/myapp"})
	if err != nil {
		t.Fatal(err)
	}

	hasFindings := false
	hasParseError := false
	for _, d := range diags {
		if d.Error != "" {
			hasParseError = true
		}
		if d.Message != "" {
			hasFindings = true
		}
	}
	if !hasFindings {
		t.Error("should have findings for parseable lines before the error")
	}
	if !hasParseError {
		t.Error("should have parse error diagnostic for malformed value")
	}
}

func TestGovulncheckHandlerWithMock(t *testing.T) {
	input := `{"config":{"protocol_version":"v1.0.0"}}
{"osv":{"id":"GO-2026-4918","summary":"Test vuln"}}
`
	r := &mockRunner{
		outputs: map[string][]byte{
			"/fake/bin/govulncheck:-json": []byte(input),
		},
	}
	handler := &GovulncheckHandler{BinDir: "/fake/bin", WorkspaceModules: []string{"github.com/myorg/myapp"}}
	diags, err := handler.Run(context.Background(), r, "/project/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics (no findings), got %d", len(diags))
	}
}

func TestChooseTraceEntry(t *testing.T) {
	trace := []traceEntryJSON{
		{Module: "github.com/myorg/myapp", Package: "github.com/myorg/myapp/internal/httpclient", Position: &positionJSON{Filename: "internal/httpclient/client.go", Line: 78, Column: 25}},
		{Module: "stdlib", Package: "net/http", Position: &positionJSON{Filename: "src/net/http/client.go", Line: 586, Column: 18}},
	}
	workspaceModules := []string{"github.com/myorg/myapp"}
	entry := chooseTraceEntry(trace, workspaceModules)
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if entry.Position.Line != 78 {
		t.Errorf("should pick first workspace-local entry (trace[0]), got line %d", entry.Position.Line)
	}
}

func TestChooseTraceEntryNoWorkspaceMatch(t *testing.T) {
	trace := []traceEntryJSON{
		{Module: "stdlib", Package: "net/http", Position: &positionJSON{Filename: "src/net/http/client.go", Line: 586, Column: 18}},
	}
	entry := chooseTraceEntry(trace, []string{"github.com/myorg/myapp"})
	if entry == nil {
		t.Fatal("expected non-nil entry")
	}
	if entry.Position.Line != 586 {
		t.Error("should fall back to trace[0] when no workspace match")
	}
}

func TestParseGovulncheckFindingBeforeOSV(t *testing.T) {
	input := `{"finding":{"osv":"GO-2026-4918","fixed_version":"v1.25.10","trace":[{"module":"github.com/myorg/myapp","version":"v1.0.0","package":"github.com/myorg/myapp/internal/httpclient","function":"Get","position":{"filename":"internal/httpclient/client.go","line":78,"column":25}}]}}
{"osv":{"id":"GO-2026-4918","summary":"Infinite loop in HTTP/2 transport","aliases":["CVE-2026-33814"]}}
`
	diags, err := parseGovulncheckOutput([]byte(input), "/project/myapp", []string{"github.com/myorg/myapp"})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diags))
	}
	if diags[0].Message != "Infinite loop in HTTP/2 transport" {
		t.Errorf("message = %q, want resolved osv summary", diags[0].Message)
	}
}

func TestIntegrationGovulncheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	binDir, err := discover.ResolveGoBinDir()
	if err != nil {
		t.Skipf("skipping: cannot resolve Go bin dir: %v", err)
	}
	sampleDir := "../../testdata/sample_project"
	if _, err := os.Stat(sampleDir); err != nil {
		t.Skipf("skipping: testdata not found: %v", err)
	}
	r := &runner.ExecRunner{Dir: sampleDir}
	handler := &GovulncheckHandler{
		BinDir:           binDir,
		WorkspaceModules: []string{"github.com/afshinator/mcp-server-go-quality/testdata/sample_project"},
	}
	ctx := context.Background()
	diags, err := handler.Run(ctx, r, sampleDir)
	if err != nil {
		t.Fatalf("govulncheck failed: %v", err)
	}
	t.Logf("govulncheck found %d issues", len(diags))
	for _, d := range diags {
		if d.Error != "" {
			t.Logf("  error: %s", d.Error)
			continue
		}
		t.Logf("  %s:%d:%d %s", d.File, d.Line, d.Column, d.Message)
	}
}
