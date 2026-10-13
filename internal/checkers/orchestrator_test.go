package checkers

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/discover"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
)

func TestRunAllWithSubset(t *testing.T) {
	lintOutput := `{"Issues": [{"FromLinter":"gocritic","Text":"test","Severity":"warning","Pos":{"Filename":"main.go","Line":1,"Column":1}}]}`
	vulnOutput := `{"config":{"protocol_version":"v1.0.0"}}
`
	nilawayOutput := `{}`

	r := &mockRunner{
		outputs: map[string][]byte{
			"/fake/bin/golangci-lint:run": []byte(lintOutput),
			"/fake/bin/govulncheck:-json": []byte(vulnOutput),
			"/fake/bin/nilaway:-json":     []byte(nilawayOutput),
		},
	}

	handlers := []Checker{
		NewGolangciLintHandler("/fake/bin"),
		&GovulncheckHandler{BinDir: "/fake/bin", WorkspaceModules: []string{"github.com/myorg/myapp"}},
		&NilawayHandler{BinDir: "/fake/bin"},
	}

	diags := RunAllChecks(context.Background(), r, handlers, "/project/myapp", 5*time.Minute)
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics, want 1 (only lint has findings)", len(diags))
	}
	if diags[0].Tool != "golangci-lint" {
		t.Errorf("tool = %q", diags[0].Tool)
	}
}

func TestRunAllEmptySubset(t *testing.T) {
	r := &mockRunner{
		outputs: map[string][]byte{},
	}
	diags := RunAllChecks(context.Background(), r, nil, "/project/myapp", 5*time.Minute)
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics for empty subset, got %d", len(diags))
	}
}

func TestRunAllPanicRecovery(t *testing.T) {
	panicChecker := &panicTestChecker{name: "panic-tool"}
	r := &mockRunner{outputs: map[string][]byte{}}
	diags := RunAllChecks(context.Background(), r, []Checker{panicChecker}, "/project/myapp", 5*time.Second)
	foundPanicError := false
	for _, d := range diags {
		if d.Error != "" && d.Tool == "panic-tool" {
			foundPanicError = true
		}
	}
	if !foundPanicError {
		t.Error("expected panic recovery diagnostic, got none")
	}
}

type panicTestChecker struct {
	name string
}

func (p *panicTestChecker) Name() string { return p.name }

func (p *panicTestChecker) Run(ctx context.Context, r runner.CommandRunner, projectPath string) ([]diagnostic.Diagnostic, error) {
	panic("test panic")
}

func TestRunAllSortsResults(t *testing.T) {
	lintOutput := `{"Issues": [
		{"FromLinter":"test","Text":"later","Severity":"warning","Pos":{"Filename":"z.go","Line":10,"Column":1}},
		{"FromLinter":"test","Text":"earlier","Severity":"warning","Pos":{"Filename":"a.go","Line":5,"Column":1}},
		{"FromLinter":"test","Text":"same file","Severity":"warning","Pos":{"Filename":"a.go","Line":1,"Column":1}}
	]}`
	r := &mockRunner{
		outputs: map[string][]byte{
			"/fake/bin/golangci-lint:run": []byte(lintOutput),
		},
	}
	handlers := []Checker{NewGolangciLintHandler("/fake/bin")}

	diags := RunAllChecks(context.Background(), r, handlers, "/project/myapp", 5*time.Minute)
	if len(diags) < 3 {
		t.Fatalf("got %d diagnostics, want 3", len(diags))
	}
	if diags[0].File != "a.go" || diags[1].File != "a.go" || diags[2].File != "z.go" {
		t.Errorf("diagnostics not sorted by file: %v", diags)
	}
}

func TestFormatCheckerErrorPrefersTrueErrorOverContextState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // context is cancelled, but err is a real tool failure

	realErr := errors.New("Tool command failed with exit code 1. Stderr: syntax error")
	result := FormatCheckerError(ctx, 5*time.Minute, realErr)
	if result != realErr.Error() {
		t.Errorf("got %q, want real error %q (context state should not shadow tool failure)", result, realErr.Error())
	}
}

func TestFormatCheckerErrorReportsContextAndExitFacts(t *testing.T) {
	processErr := &runner.ExitError{
		ExitCode: 2,
		Stderr:   "syntax error\n",
		Err:      errors.Join(errors.New("exit status 2"), context.DeadlineExceeded),
	}

	got := FormatCheckerError(context.Background(), 5*time.Minute, processErr)
	for _, want := range []string{"timed out after 5m0s", "exit code 2", "syntax error"} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatCheckerError() = %q, want it to contain %q", got, want)
		}
	}

	processErr.Err = errors.Join(errors.New("exit status 2"), context.Canceled)
	got = FormatCheckerError(context.Background(), 5*time.Minute, processErr)
	for _, want := range []string{"cancelled", "exit code 2", "syntax error"} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatCheckerError() = %q, want it to contain %q", got, want)
		}
	}
}

func TestFormatCheckerErrorDeadlineExceededError(t *testing.T) {
	result := FormatCheckerError(context.Background(), 5*time.Minute, context.DeadlineExceeded)
	if result != "timed out after 5m0s" {
		t.Errorf("got %q, want timeout message", result)
	}
}

func TestFormatCheckerErrorCancelledError(t *testing.T) {
	result := FormatCheckerError(context.Background(), 5*time.Minute, context.Canceled)
	if result != "cancelled" {
		t.Errorf("got %q, want 'cancelled'", result)
	}
}

func TestRunAllNilHandlerDoesNotCrash(t *testing.T) {
	r := &mockRunner{outputs: map[string][]byte{}}

	// Passing a nil Checker interface currently crashes the whole process.
	// After the fix it must produce an error Diagnostic without panicking.
	diags := RunAllChecks(context.Background(), r, []Checker{nil}, "/project/myapp", 5*time.Second)

	if len(diags) == 0 {
		t.Error("expected at least one error diagnostic for nil handler, got none")
	}
	if len(diags) > 0 && diags[0].Error == "" {
		t.Error("expected non-empty Error field in diagnostic for nil handler")
	}
	if len(diags) > 0 && !strings.Contains(diags[0].Error, "internal panic") {
		t.Errorf("expected panic-recovery error, got %q", diags[0].Error)
	}
}

func TestIntegrationRunAllChecks(t *testing.T) {
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
	handlers := []Checker{
		NewGolangciLintHandler(binDir),
		&GovulncheckHandler{
			BinDir:           binDir,
			WorkspaceModules: []string{"github.com/afshinator/mcp-server-go-quality/testdata/sample_project"},
		},
		&NilawayHandler{
			BinDir:      binDir,
			IncludePkgs: "github.com/afshinator/mcp-server-go-quality/testdata/sample_project",
		},
	}
	ctx := context.Background()
	diags := RunAllChecks(ctx, r, handlers, sampleDir, 2*time.Minute)
	t.Logf("runAllChecks found %d total diagnostics", len(diags))
	for _, d := range diags {
		if d.Error != "" {
			t.Logf("[%s] ERROR: %s", d.Tool, d.Error)
			continue
		}
		t.Logf("[%s] %s:%d:%d %s", d.Tool, d.File, d.Line, d.Column, d.Message)
	}
	if len(diags) == 0 {
		t.Error("expected at least one diagnostic from test project")
	}
}

// TestRunAllChecksOrderIsTotal pins the ordering contract. Module-level
// govulncheck findings all share file "" and line 0, so a comparator using only
// (file, line) left 64 of them tied and the output depended on tool emission
// order. The same input in a different sequence must sort to the same output.
func TestRunAllChecksOrderIsTotal(t *testing.T) {
	// Two diagnostics that are identical in every key except `Error`: the one
	// pair the comparator used to leave unspecified, reachable when the same
	// tool is selected twice.
	errA := diagnostic.Diagnostic{Tool: toolname.Govulncheck, File: "", Line: 0, Severity: "", Message: "", Error: "timed out after 5m0s"}
	errB := diagnostic.Diagnostic{Tool: toolname.Govulncheck, File: "", Line: 0, Severity: "", Message: "", Error: "exit code 1"}

	mk := func(msg, tool string) diagnostic.Diagnostic {
		return diagnostic.Diagnostic{Tool: tool, File: "", Line: 0, Severity: "error", Message: msg}
	}
	base := []diagnostic.Diagnostic{
		mk("c", toolname.Govulncheck), mk("a", toolname.Govulncheck),
		mk("b", toolname.Nilaway), mk("a", toolname.GolangciLint),
		mk("b", toolname.Govulncheck), mk("a", toolname.Govulncheck),
		errA, errB,
	}

	order := func(diags []diagnostic.Diagnostic) []string {
		h := &sliceHandler{name: "mock", out: diags}
		got := RunAllChecks(context.Background(), &mockRunner{outputs: map[string][]byte{}}, []Checker{h}, "/p", time.Second)
		out := make([]string, 0, len(got))
		for _, d := range got {
			// Key on Error too: two diagnostics can be identical in every other
			// field and differ only in the failure they report.
			out = append(out, d.Tool+"/"+d.Message+"/"+d.Error)
		}
		return out
	}

	want := order(append([]diagnostic.Diagnostic(nil), base...))
	if len(want) != len(base) {
		t.Fatalf("got %d diagnostics, want %d", len(want), len(base))
	}
	// every permutation of an all-tied set must produce the same order
	for _, perm := range [][]int{{7, 6, 5, 4, 3, 2, 1, 0}, {2, 6, 0, 4, 1, 7, 5, 3}, {1, 7, 3, 6, 5, 0, 2, 4}} {
		shuffled := make([]diagnostic.Diagnostic, 0, len(base))
		for _, i := range perm {
			shuffled = append(shuffled, base[i])
		}
		got := order(shuffled)
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("order depends on input sequence: permutation %v gave %v, want %v", perm, got, want)
			}
		}
	}
}

// sliceHandler is a Checker that returns a fixed set of diagnostics.
type sliceHandler struct {
	name string
	out  []diagnostic.Diagnostic
}

func (h *sliceHandler) Name() string { return h.name }

func (h *sliceHandler) Run(context.Context, runner.CommandRunner, string) ([]diagnostic.Diagnostic, error) {
	return h.out, nil
}
