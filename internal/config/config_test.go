package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()
	if cfg.Timeout != 5*time.Minute {
		t.Errorf("default timeout = %v, want 5m", cfg.Timeout)
	}
	if cfg.Tools["golangci-lint"].Version != GolangciLintVersion {
		t.Errorf("golangci-lint default version = %q, want %q", cfg.Tools["golangci-lint"].Version, GolangciLintVersion)
	}
	if cfg.Tools["govulncheck"].Version != GovulncheckVersion {
		t.Errorf("govulncheck default version = %q, want %q", cfg.Tools["govulncheck"].Version, GovulncheckVersion)
	}
	if cfg.Tools["nilaway"].Version != NilawayVersion {
		t.Errorf("nilaway default version = %q, want %q", cfg.Tools["nilaway"].Version, NilawayVersion)
	}
}

func TestDefaultVersionFallsBackToLatest(t *testing.T) {
	if got := DefaultVersion("some-unknown-tool"); got != "latest" {
		t.Errorf("DefaultVersion(unknown) = %q, want latest", got)
	}
	tools := map[string]string{
		"golangci-lint": GolangciLintVersion,
		"govulncheck":   GovulncheckVersion,
		"nilaway":       NilawayVersion,
	}
	for name, want := range tools {
		got := DefaultVersion(name)
		if got != want {
			t.Errorf("DefaultVersion(%q) = %q, want %q", name, got, want)
		}
		if got == "latest" {
			t.Errorf("DefaultVersion(%q) is unpinned, want a concrete version", name)
		}
	}
}

func TestLoadFileNotFound(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timeout != 5*time.Minute {
		t.Errorf("timeout = %v", cfg.Timeout)
	}
}

func TestLoadValidFile(t *testing.T) {
	yamlContent := `
timeout: 10m
tools:
  golangci-lint:
    version: v2.14.0
    extra_args: ["--no-config"]
  govulncheck:
    version: latest
    extra_args: []
  nilaway:
    version: latest
    extra_args: ["--exclude-pkgs=github.com/myorg/vendor"]
`
	dir := t.TempDir()
	path := filepath.Join(dir, ".go-quality.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timeout != 10*time.Minute {
		t.Errorf("timeout = %v, want 10m", cfg.Timeout)
	}
	if cfg.Tools["golangci-lint"].Version != "v2.14.0" {
		t.Errorf("golangci-lint version = %q", cfg.Tools["golangci-lint"].Version)
	}
	if len(cfg.Tools["golangci-lint"].ExtraArgs) != 1 {
		t.Errorf("golangci-lint extra_args len = %d, want 1", len(cfg.Tools["golangci-lint"].ExtraArgs))
	}
	if cfg.Tools["golangci-lint"].ExtraArgs[0] != "--no-config" {
		t.Errorf("extra_args[0] = %q", cfg.Tools["golangci-lint"].ExtraArgs[0])
	}
}

func TestLoadPartialConfig(t *testing.T) {
	yamlContent := `
timeout: 3m
tools:
  golangci-lint:
    extra_args: ["--disable-all"]
  govulncheck:
    version: v1.2.0
`
	dir := t.TempDir()
	path := filepath.Join(dir, ".go-quality.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timeout != 3*time.Minute {
		t.Errorf("timeout = %v, want 3m", cfg.Timeout)
	}
	if cfg.Tools["golangci-lint"].Version != GolangciLintVersion {
		t.Errorf("golangci-lint version should default to %s when omitted, got %q", GolangciLintVersion, cfg.Tools["golangci-lint"].Version)
	}
	if len(cfg.Tools["golangci-lint"].ExtraArgs) != 1 || cfg.Tools["golangci-lint"].ExtraArgs[0] != "--disable-all" {
		t.Errorf("golangci-lint extra_args = %v", cfg.Tools["golangci-lint"].ExtraArgs)
	}
	if cfg.Tools["govulncheck"].Version != "v1.2.0" {
		t.Errorf("govulncheck version = %q, want v1.2.0", cfg.Tools["govulncheck"].Version)
	}
	if cfg.Tools["nilaway"].Version != NilawayVersion {
		t.Errorf("nilaway version should default to %s when omitted, got %q", NilawayVersion, cfg.Tools["nilaway"].Version)
	}
}

func TestValidateExtraArgsReservedFlag(t *testing.T) {
	cfg := Config{
		Timeout: 5 * time.Minute,
		Tools: map[string]ToolConfig{
			"golangci-lint": {
				Version:   "v2.14.0",
				ExtraArgs: []string{"--out-format=text"},
			},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for reserved flag in extra_args")
	}
}

func TestReservedFlags(t *testing.T) {
	flags := ReservedFlags("golangci-lint")
	found := false
	for _, f := range flags {
		if f == "--out-format" {
			found = true
		}
	}
	if !found {
		t.Error("expected --out-format in golangci-lint reserved flags")
	}
}

func TestLoadRequiredMissingFile(t *testing.T) {
	_, err := LoadRequired(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Error("LoadRequired must return an error when file does not exist, got nil")
	}
}

func TestLoadRequiredExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("timeout: 3m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadRequired(path)
	if err != nil {
		t.Fatalf("LoadRequired returned unexpected error: %v", err)
	}
	if cfg.Timeout != 3*time.Minute {
		t.Errorf("timeout = %v, want 3m", cfg.Timeout)
	}
}

func TestResolveTimeout(t *testing.T) {
	cfg := Default()
	if cfg.ResolveTimeout() != 5*time.Minute {
		t.Errorf("default resolve = %v", cfg.ResolveTimeout())
	}
	cfg.Timeout = 0
	if cfg.ResolveTimeout() != 5*time.Minute {
		t.Errorf("zero timeout should fall back to default")
	}
	cfg.Timeout = 10 * time.Minute
	if cfg.ResolveTimeout() != 10*time.Minute {
		t.Errorf("explicit timeout = %v", cfg.ResolveTimeout())
	}
}

// TestGolangciLintVersionIsConsistent guards against the pinned golangci-lint
// version drifting between the Go default and the build/CI/docs copies of it.
func TestGolangciLintVersionIsConsistent(t *testing.T) {
	files := []string{
		"../../Makefile",
		"../../Dockerfile",
		"../../README.md",
		"../../.github/workflows/lint.yml",
		"../../docs/agents/AGENTS.md",
		"../../docs/agents/reference.md",
		"../../examples/config/ci.yaml",
		"../../examples/config/dev-iteration.yaml",
		"../../examples/config/monorepo.yaml",
	}
	re := regexp.MustCompile(`v2\.\d+\.\d+`)
	for _, f := range files {
		body, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		found := re.FindAllString(string(body), -1)
		if len(found) == 0 {
			t.Errorf("%s does not reference the golangci-lint pin %s", f, GolangciLintVersion)
			continue
		}
		for _, v := range found {
			if v != GolangciLintVersion {
				t.Errorf("%s references %s, want %s", f, v, GolangciLintVersion)
			}
		}
	}
}

// TestPinnedVersionsAreConsistent guards the other two pins. Both are installed
// by the Dockerfile, so drift there ships an image whose pre-baked tools the
// server then reinstalls on every start.
func TestPinnedVersionsAreConsistent(t *testing.T) {
	files := []string{
		"../../Dockerfile",
		"../../README.md",
		"../../docs/agents/AGENTS.md",
		"../../docs/agents/reference.md",
		"../../examples/config/ci.yaml",
		"../../examples/config/dev-iteration.yaml",
		"../../examples/config/monorepo.yaml",
	}
	pins := map[string]string{
		"golangci-lint": GolangciLintVersion,
		"govulncheck":   GovulncheckVersion,
		"nilaway":       NilawayVersion,
	}
	for _, f := range files {
		body, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		text := string(body)
		for name, version := range pins {
			if !strings.Contains(text, version) {
				t.Errorf("%s does not reference the %s pin %s", f, name, version)
			}
		}
	}
	// The Dockerfile pre-bakes the tools; an unpinned install there ships an
	// image the server immediately repairs on first use.
	dockerfile, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatalf("reading Dockerfile: %v", err)
	}
	if strings.Contains(string(dockerfile), "@latest") {
		t.Error("Dockerfile installs a tool @latest; pins must be explicit")
	}
}
