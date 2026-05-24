package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()
	if cfg.Timeout != 5*time.Minute {
		t.Errorf("default timeout = %v, want 5m", cfg.Timeout)
	}
	if cfg.Tools["golangci-lint"].Version != "v2.11.4" {
		t.Errorf("golangci-lint default version = %q, want v2.11.4", cfg.Tools["golangci-lint"].Version)
	}
	if cfg.Tools["govulncheck"].Version != "latest" {
		t.Errorf("govulncheck default version = %q, want latest", cfg.Tools["govulncheck"].Version)
	}
	if cfg.Tools["nilaway"].Version != "latest" {
		t.Errorf("nilaway default version = %q, want latest", cfg.Tools["nilaway"].Version)
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
    version: v2.11.4
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
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timeout != 10*time.Minute {
		t.Errorf("timeout = %v, want 10m", cfg.Timeout)
	}
	if cfg.Tools["golangci-lint"].Version != "v2.11.4" {
		t.Errorf("golangci-lint version = %q", cfg.Tools["golangci-lint"].Version)
	}
	if len(cfg.Tools["golangci-lint"].ExtraArgs) != 1 {
		t.Errorf("golangci-lint extra_args len = %d, want 1", len(cfg.Tools["golangci-lint"].ExtraArgs))
	}
	if cfg.Tools["golangci-lint"].ExtraArgs[0] != "--no-config" {
		t.Errorf("extra_args[0] = %q", cfg.Tools["golangci-lint"].ExtraArgs[0])
	}
}

func TestValidateExtraArgsReservedFlag(t *testing.T) {
	cfg := Config{
		Timeout: 5 * time.Minute,
		Tools: map[string]ToolConfig{
			"golangci-lint": {
				Version:   "v2.11.4",
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
