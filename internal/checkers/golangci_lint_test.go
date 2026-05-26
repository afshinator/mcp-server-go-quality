package checkers

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/afshinator/mcp-server-go-quality/internal/discover"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
)

type mockRunner struct {
	outputs map[string][]byte
	err     error
}

func (m *mockRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	if len(args) > 0 {
		key := name + ":" + args[0]
		if out, ok := m.outputs[key]; ok {
			return out, nil
		}
	}
	return nil, nil
}

func TestParseGolangciLintOutput(t *testing.T) {
	input := `{"Issues": [
		{"FromLinter":"gocognit","Text":"cognitive complexity 18 is high (> 15)","Severity":"warning","Pos":{"Filename":"cmd/main.go","Line":115,"Column":1}},
		{"FromLinter":"gosec","Text":"G601: Implicit memory aliasing in for loop","Severity":"error","Pos":{"Filename":"internal/auth/auth.go","Line":42,"Column":10}}
	]}`

	diags, err := parseGolangciLintOutput([]byte(input), "/project/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics, want 2", len(diags))
	}

	d0 := diags[0]
	if d0.Tool != toolname.GolangciLint {
		t.Errorf("tool = %q, want %s", d0.Tool, toolname.GolangciLint)
	}
	if d0.File != "cmd/main.go" {
		t.Errorf("file = %q, want cmd/main.go", d0.File)
	}
	if d0.Line != 115 {
		t.Errorf("line = %d, want 115", d0.Line)
	}
	if d0.Column != 1 {
		t.Errorf("column = %d, want 1", d0.Column)
	}
	if d0.Severity != "warning" {
		t.Errorf("severity = %q, want warning", d0.Severity)
	}
	if d0.Message != "cognitive complexity 18 is high (> 15)" {
		t.Errorf("message = %q", d0.Message)
	}
	if d0.Native == nil {
		t.Error("native should not be nil")
	}
}

func TestParseGolangciLintEmptyOutput(t *testing.T) {
	input := `{"Issues": []}`
	diags, err := parseGolangciLintOutput([]byte(input), "/project/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Errorf("got %d diagnostics, want 0", len(diags))
	}
}

func TestGolangciLintHandlerWithMock(t *testing.T) {
	input := `{"Issues": [{"FromLinter":"gocritic","Text":"test","Severity":"warning","Pos":{"Filename":"main.go","Line":1,"Column":1}}]}`
	r := &mockRunner{
		outputs: map[string][]byte{
			"/fake/bin/golangci-lint:run": []byte(input),
		},
	}
	handler := NewGolangciLintHandler("/fake/bin")
	diags, err := handler.Run(context.Background(), r, "/project/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(diags))
	}
	if diags[0].Tool != toolname.GolangciLint {
		t.Errorf("tool = %q", diags[0].Tool)
	}
}

func TestParseGolangciLintPathNormalization(t *testing.T) {
	input := `{"Issues": [
		{"FromLinter":"test","Text":"test","Severity":"warning","Pos":{"Filename":"/project/myapp/cmd/main.go","Line":1,"Column":1}}
	]}`
	diags, err := parseGolangciLintOutput([]byte(input), "/project/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if diags[0].File != "cmd/main.go" {
		t.Errorf("file = %q, want cmd/main.go", diags[0].File)
	}
}

func TestGolangciLintHandlerExitErrorNotPrefixed(t *testing.T) {
	exitErr := &runner.ExitError{ExitCode: 1, Stderr: "no linter config found"}
	r := &mockRunner{err: exitErr}
	handler := NewGolangciLintHandler("/fake/bin")

	_, err := handler.Run(context.Background(), r, "/project/myapp")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if strings.HasPrefix(err.Error(), "golangci-lint: ") {
		t.Errorf("error must not have tool prefix, got: %q", err.Error())
	}
	var e *runner.ExitError
	if !errors.As(err, &e) {
		t.Errorf("error should unwrap to *runner.ExitError, got %T: %v", err, err)
	}
}

func TestIntegrationGolangciLint(t *testing.T) {
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
	handler := NewGolangciLintHandler(binDir)
	ctx := context.Background()
	diags, err := handler.Run(ctx, r, sampleDir)
	if err != nil {
		t.Fatalf("golangci-lint failed: %v", err)
	}
	t.Logf("golangci-lint found %d issues", len(diags))
	for _, d := range diags {
		t.Logf("  %s:%d:%d [%s] %s", d.File, d.Line, d.Column, d.Severity, d.Message)
	}
}
