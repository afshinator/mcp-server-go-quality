package toolname

import "testing"

func TestIsValid(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"golangci-lint", true},
		{"govulncheck", true},
		{"nilaway", true},
		{"unknown-tool", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValid(tt.name); got != tt.want {
				t.Errorf("IsValid(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestAll(t *testing.T) {
	all := All()
	if len(all) != 3 {
		t.Fatalf("got %d tools, want 3", len(all))
	}
	seen := map[string]bool{}
	for _, name := range all {
		seen[name] = true
	}
	for _, name := range []string{
		GolangciLint,
		Govulncheck,
		Nilaway,
	} {
		if !seen[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestInstallPath(t *testing.T) {
	tests := []struct {
		tool string
		want string
	}{
		{GolangciLint, "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"},
		{Govulncheck, "golang.org/x/vuln/cmd/govulncheck"},
		{Nilaway, "go.uber.org/nilaway/cmd/nilaway"},
		{"unknown", ""},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			if got := InstallPath(tt.tool); got != tt.want {
				t.Errorf("InstallPath(%q) = %q, want %q", tt.tool, got, tt.want)
			}
		})
	}
}
