package checkers

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/discover"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
)

func TestParseNilawayOutput(t *testing.T) {
	input := `{
  "github.com/myorg/myapp/internal/engine": {
    "nilaway": [
      {
        "posn": "/project/myapp/internal/engine/pulse.go:42:12",
        "end": "/project/myapp/internal/engine/pulse.go:42:15",
        "message": "Potential nil panic detected. Observed nil flow from source to dereference point: \n\t- engine/pulse.go:42:12: variable \"resp\" used as non-nil\n"
      }
    ]
  },
  "github.com/myorg/myapp/internal/api": {
    "nilaway": [
      {
        "posn": "/project/myapp/internal/api/client.go:288:4",
        "end": "/project/myapp/internal/api/client.go:288:4",
        "message": "Potential nil panic detected. Deep read from local variable \"counts\"\n"
      }
    ]
  }
}`
	diags, err := parseNilawayOutput([]byte(input), "/project/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics, want 2", len(diags))
	}

	findByFile := func(file string) *diagnostic.Diagnostic {
		for i := range diags {
			if diags[i].File == file {
				return &diags[i]
			}
		}
		t.Fatalf("diagnostic not found for file %q", file)
		return nil
	}

	d0 := findByFile("internal/engine/pulse.go")
	if d0.Tool != toolname.Nilaway {
		t.Errorf("tool = %q, want %s", d0.Tool, toolname.Nilaway)
	}
	if d0.Line != 42 {
		t.Errorf("line = %d, want 42", d0.Line)
	}
	if d0.Column != 12 {
		t.Errorf("column = %d, want 12", d0.Column)
	}
	if d0.Severity != "" {
		t.Error("severity should be empty for nilaway (no severity concept)")
	}
	if d0.Message == "" {
		t.Error("message is empty")
	}
	if d0.Native == nil {
		t.Error("native is nil")
	}

	var native map[string]interface{}
	if err := json.Unmarshal(d0.Native, &native); err != nil {
		t.Errorf("native should be valid JSON: %v", err)
	}

	d1 := findByFile("internal/api/client.go")
	if d1.Tool != toolname.Nilaway {
		t.Errorf("tool = %q, want %s", d1.Tool, toolname.Nilaway)
	}
	if d1.Line != 288 {
		t.Errorf("line = %d, want 288", d1.Line)
	}
}

func TestParseNilawayEmptyOutput(t *testing.T) {
	input := `{}`
	diags, err := parseNilawayOutput([]byte(input), "/project/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics for empty {}, got %d", len(diags))
	}
}

func TestParseNilawayEmptyStdout(t *testing.T) {
	input := ``
	_, err := parseNilawayOutput([]byte(input), "/project/myapp")
	if err == nil {
		t.Error("expected error for empty stdout (distinct from {})")
	}
}

func TestParsePosn(t *testing.T) {
	tests := []struct {
		posn     string
		wantFile string
		wantLine int
		wantCol  int
	}{
		{"/project/myapp/pkg/handler.go:123:45", "/project/myapp/pkg/handler.go", 123, 45},
		{"/project/myapp/main.go:10:1", "/project/myapp/main.go", 10, 1},
		{"/project/myapp/file.go:5", "/project/myapp/file.go", 5, 0},
		{"/project/myapp/file.go", "/project/myapp/file.go", 0, 0},
		{"", "", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.posn, func(t *testing.T) {
			file, line, col := parsePosn(tt.posn)
			if file != tt.wantFile {
				t.Errorf("file = %q, want %q", file, tt.wantFile)
			}
			if line != tt.wantLine {
				t.Errorf("line = %d, want %d", line, tt.wantLine)
			}
			if col != tt.wantCol {
				t.Errorf("col = %d, want %d", col, tt.wantCol)
			}
		})
	}
}

func TestExtractFirstSentence(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Potential nil panic detected. More details here.", "Potential nil panic detected."},
		{"Single line with no period", "Single line with no period"},
		{"First. Second. Third.", "First."},
		{"", ""},
		{"No period but newline\n second line", "No period but newline"},
		{"Multiple newlines\n\n after", "Multiple newlines"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractFirstSentence(tt.input)
			if got != tt.want {
				t.Errorf("extractFirstSentence(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNilawayHandlerWithMock(t *testing.T) {
	input := `{}`
	r := &mockRunner{
		outputs: map[string][]byte{
			"/fake/bin/nilaway:-json": []byte(input),
		},
	}
	handler := &NilawayHandler{BinDir: "/fake/bin"}
	diags, err := handler.Run(context.Background(), r, "/project/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Errorf("expected 0 diagnostics, got %d", len(diags))
	}
}

func TestIntegrationNilaway(t *testing.T) {
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
	handler := &NilawayHandler{
		BinDir:      binDir,
		IncludePkgs: "github.com/afshinator/mcp-server-go-quality/testdata/sample_project",
	}
	ctx := context.Background()
	diags, err := handler.Run(ctx, r, sampleDir)
	if err != nil {
		t.Fatalf("nilaway failed: %v", err)
	}
	t.Logf("nilaway found %d nil issues", len(diags))
	for _, d := range diags {
		t.Logf("  %s:%d:%d %s", d.File, d.Line, d.Column, d.Message)
	}
}
