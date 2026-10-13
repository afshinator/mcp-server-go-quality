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

func TestModulePath(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{GolangciLint, "github.com/golangci/golangci-lint/v2"},
		{Govulncheck, "golang.org/x/vuln"},
		{Nilaway, "go.uber.org/nilaway"},
	}
	for _, tc := range tests {
		if got := ModulePath(tc.name); got != tc.want {
			t.Errorf("ModulePath(%q) = %q, want %q", tc.name, got, tc.want)
		}
		if ModulePath("nope") != "" {
			t.Error("ModulePath(unknown) should be empty")
		}
	}
	// InstallPath and ModulePath must stay in lockstep: the installer needs both
	// for every tool.
	for _, name := range All() {
		if InstallPath(name) == "" || ModulePath(name) == "" {
			t.Errorf("tool %q is missing an InstallPath or ModulePath", name)
		}
	}
}

func TestResolveDedupesAndValidates(t *testing.T) {
	got, err := Resolve([]string{GolangciLint, GolangciLint, Nilaway})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != GolangciLint || got[1] != Nilaway {
		t.Errorf("Resolve with a duplicate = %v, want %v", got, []string{GolangciLint, Nilaway})
	}
	if _, err := Resolve([]string{"nope"}); err == nil {
		t.Error("expected an error for an unknown tool")
	}
	if got, err := Resolve(nil); err != nil || len(got) != len(All()) {
		t.Errorf("Resolve(nil) = %v (%v), want all tools", got, err)
	}
}
