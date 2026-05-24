# Go Quality MCP — Part 3: Config and Tools (Tasks 7–9)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** YAML config loading, tool name validation, binary directory resolution, version-aware tool discovery with double-check lock pattern.

**Architecture:** Config reads `.go-quality.yaml` with validated `extra_args`. Tool discovery resolves `$GOBIN/$GOPATH/bin` at startup, caches resolved versions, uses global `InstallMu` to serialize installs.

**Tech Stack:** Go 1.25, `gopkg.in/yaml.v3`

**Prerequisite:** Parts 1–2 complete

---

## File Structure

```
mcp-server-go-quality/
├── internal/
│   ├── config/
│   │   ├── config.go                          # YAML loader, extra_args validation, defaults
│   │   └── config_test.go
│   ├── discover/
│   │   ├── discover.go                        # Binary dir resolution, version-aware cache, install
│   │   └── discover_test.go
│   └── toolname/
│       ├── toolname.go                        # Valid tool names constants and validation
│       └── toolname_test.go
```

---

### Task 7: Config Loading

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/config/config_test.go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — undefined: Config, Default, Load, etc.

- [ ] **Step 3: Write implementation**

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ToolConfig struct {
	Version   string   `yaml:"version"`
	ExtraArgs []string `yaml:"extra_args"`
}

type Config struct {
	Timeout time.Duration         `yaml:"timeout"`
	Tools   map[string]ToolConfig `yaml:"tools"`
}

var reservedByTool = map[string][]string{
	"golangci-lint": {"--out-format"},
	"govulncheck":   {"-json"},
	"nilaway":       {"-json", "-pretty-print"},
}

func ReservedFlags(toolName string) []string {
	return reservedByTool[toolName]
}

func Default() Config {
	return Config{
		Timeout: 5 * time.Minute,
		Tools: map[string]ToolConfig{
			"golangci-lint": {Version: "v2.11.4"},
			"govulncheck":   {Version: "latest"},
			"nilaway":       {Version: "latest"},
		},
	}
}

func (c Config) ResolveTimeout() time.Duration {
	if c.Timeout <= 0 {
		return 5 * time.Minute
	}
	return c.Timeout
}

func (c Config) Validate() error {
	for toolName, tc := range c.Tools {
		reserved, ok := reservedByTool[toolName]
		if !ok {
			continue
		}
		for _, arg := range tc.ExtraArgs {
			argName := strings.SplitN(arg, "=", 2)[0]
			for _, r := range reserved {
				if argName == r {
					return fmt.Errorf("config error: extra_args for %s contains reserved flag %s", toolName, r)
				}
			}
		}
	}
	return nil
}

func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("config read error: %w", err)
	}

	var raw Config
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("config parse error: %w", err)
	}

	// Merge: only override fields explicitly present in YAML.
	// Direct unmarshal into cfg would replace the entire Tools map.
	if raw.Timeout > 0 {
		cfg.Timeout = raw.Timeout
	}
	for name, tc := range raw.Tools {
		existing := cfg.Tools[name]
		if tc.Version != "" {
			existing.Version = tc.Version
		}
		if len(tc.ExtraArgs) > 0 {
			existing.ExtraArgs = tc.ExtraArgs
		}
		cfg.Tools[name] = existing
	}

	// Fill empty versions on the merged (defaults-preserving) config.
	for name := range cfg.Tools {
		tc := cfg.Tools[name]
		if tc.Version == "" {
			tc.Version = "latest"
			if name == "golangci-lint" {
				tc.Version = "v2.11.4"
			}
			cfg.Tools[name] = tc
		}
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS (after `go mod tidy` to fetch yaml.v3)

- [ ] **Step 5: Tidy dependencies**

```bash
go mod tidy
```

- [ ] **Step 6: Run tests again**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go go.mod go.sum
git commit -m "feat: add config loading with extra_args reserved flag validation"
```

---

### Task 8: Tool Name Constants + Validation

**Files:**
- Create: `internal/toolname/toolname.go`
- Create: `internal/toolname/toolname_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/toolname/toolname_test.go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/toolname/ -v`
Expected: FAIL — undefined symbols

- [ ] **Step 3: Write implementation**

```go
// internal/toolname/toolname.go
package toolname

const (
	GolangciLint = "golangci-lint"
	Govulncheck  = "govulncheck"
	Nilaway      = "nilaway"
)

func IsValid(name string) bool {
	switch name {
	case GolangciLint, Govulncheck, Nilaway:
		return true
	default:
		return false
	}
}

func All() []string {
	return []string{GolangciLint, Govulncheck, Nilaway}
}

func InstallPath(name string) string {
	switch name {
	case GolangciLint:
		return "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
	case Govulncheck:
		return "golang.org/x/vuln/cmd/govulncheck"
	case Nilaway:
		return "go.uber.org/nilaway/cmd/nilaway"
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/toolname/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/toolname/toolname.go internal/toolname/toolname_test.go
git commit -m "feat: add tool name constants and validation"
```

---

### Task 9: Binary Directory Resolution + Tool Discovery & Version-Aware Cache

**Files:**
- Create: `internal/discover/discover.go`
- Create: `internal/discover/discover_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/discover/discover_test.go
package discover

import (
	"testing"
)

func TestResolveGoBinDir(t *testing.T) {
	binDir, err := ResolveGoBinDir()
	if err != nil {
		t.Fatal(err)
	}
	if binDir == "" {
		t.Error("binDir must not be empty")
	}
	t.Logf("resolved binDir: %s", binDir)
}

func TestParseGoVersionOutput(t *testing.T) {
	output := []byte(`/home/user/go/bin/golangci-lint: devel go1.25.9
	path	github.com/golangci/golangci-lint/v2/cmd/golangci-lint
	mod	github.com/golangci/golangci-lint/v2	v2.11.4	h1:abc123=
	dep	github.com/BurntSushi/toml	v1.4.0	h1:def456=
	build	-buildmode=exe
	build	-compiler=gc
`)
	version := ParseModuleVersion(output, "github.com/golangci/golangci-lint/v2")
	if version != "v2.11.4" {
		t.Errorf("version = %q, want v2.11.4", version)
	}
}

func TestParseGoVersionOutputNilaway(t *testing.T) {
	output := []byte(`/home/user/go/bin/nilaway: devel go1.25.9
	path	go.uber.org/nilaway/cmd/nilaway
	mod	go.uber.org/nilaway	v0.0.0-20260515015210-fd187751154f	h1:abc=
`)
	version := ParseModuleVersion(output, "go.uber.org/nilaway")
	if version != "v0.0.0-20260515015210-fd187751154f" {
		t.Errorf("version = %q, want pseudo-version", version)
	}
}

func TestParseGoVersionOutputGovulncheck(t *testing.T) {
	output := []byte(`/home/user/go/bin/govulncheck: devel go1.25.9
	path	golang.org/x/vuln/cmd/govulncheck
	mod	golang.org/x/vuln	v1.3.0	h1:abc=
`)
	version := ParseModuleVersion(output, "golang.org/x/vuln")
	if version != "v1.3.0" {
		t.Errorf("version = %q, want v1.3.0", version)
	}
}

func TestParseGoVersionUnknown(t *testing.T) {
	output := []byte(`/home/user/go/bin/custom-tool: devel go1.25.9
	path	some/custom/tool
`)
	version := ParseModuleVersion(output, "some/custom/tool")
	if version != "unknown" {
		t.Errorf("version = %q, want unknown", version)
	}
}

func TestCacheHitAndMiss(t *testing.T) {
	c := NewCache()
	c.Store("govulncheck", "v1.3.0")
	c.Store("nilaway", "v0.0.0-20260515")

	v, ok := c.Load("govulncheck")
	if !ok || v != "v1.3.0" {
		t.Errorf("govulncheck = (%q, %v)", v, ok)
	}

	_, ok = c.Load("golangci-lint")
	if ok {
		t.Error("golangci-lint should be a cache miss")
	}
}

func TestCacheUnknownVersion(t *testing.T) {
	c := NewCache()
	c.Store("govulncheck", "unknown")
	v, ok := c.Load("govulncheck")
	if !ok || v != "unknown" {
		t.Errorf("unknown version should be stored and retrievable")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/discover/ -v`
Expected: FAIL — undefined symbols

- [ ] **Step 3: Write implementation**

```go
// internal/discover/discover.go
package discover

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

func ResolveGoBinDir() (string, error) {
	out, err := exec.Command("go", "env", "GOBIN").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOBIN: %w", err)
	}
	if binDir := strings.TrimSpace(string(out)); binDir != "" && binDir != "\n" {
		return binDir, nil
	}

	out, err = exec.Command("go", "env", "GOPATH").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOPATH: %w", err)
	}
	gopath := strings.TrimSpace(string(out))
	if gopath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("os.UserHomeDir: %w", err)
		}
		gopath = filepath.Join(homeDir, "go")
	}
	return filepath.Join(gopath, "bin"), nil
}

type Cache struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewCache() *Cache {
	return &Cache{data: make(map[string]string)}
}

// InstallMu serializes all install operations across concurrent requests.
// Held during the entire slow path: re-check → resolve → install → cache update.
var InstallMu sync.Mutex

func (c *Cache) Load(toolName string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[toolName]
	return v, ok
}

func (c *Cache) Store(toolName, version string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[toolName] = version
}

func ParseModuleVersion(goVersionOutput []byte, modulePath string) string {
	scanner := bufio.NewScanner(bytes.NewReader(goVersionOutput))
	prefix := "mod\t" + modulePath + "\t"
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, prefix) {
			rest := strings.TrimPrefix(line, prefix)
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				return fields[0]
			}
		}
	}
	return "unknown"
}

func ReadInstalledVersion(binDir, toolName, modulePath string) (string, error) {
	binaryPath := filepath.Join(binDir, toolName)
	cmd := exec.Command("go", "version", "-m", binaryPath)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go version -m %s: %w", binaryPath, err)
	}
	version := ParseModuleVersion(output, modulePath)
	return version, nil
}

func ResolveLatest(ctx context.Context, modulePath string) (string, error) {
	args := []string{"list", "-m", "-json", modulePath + "@latest"}
	cmd := exec.CommandContext(ctx, "go", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list -m -json %s@latest: %w", modulePath, err)
	}
	var info struct {
		Version string `json:"Version"`
	}
	if err := json.Unmarshal(output, &info); err != nil {
		return "", fmt.Errorf("parsing go list output: %w", err)
	}
	if info.Version == "" {
		return "", fmt.Errorf("empty version from go list for %s", modulePath)
	}
	return info.Version, nil
}

// InstallResult holds the outcome of EnsureInstalled.
type InstallResult struct {
	Version        string
	NewlyInstalled bool
}

// EnsureInstalled follows the canonical double-check sequence per
// docs/superpowers/plans/install-lock-double-check-sequence.md.
//
// Fast path (no contention):
//   1. RLock cache → check for matching version → return if found.
//
// Slow path (install needed):
//   2. Lock InstallMu
//   3. Re-check cache (another request may have installed while we waited)
//   4. Resolve version if "latest"
//   5. Verify binary on disk
//   6. go install if missing or wrong version
//   7. Verify install succeeded (os.Stat binary)
//   8. Update cache
//   9. Unlock InstallMu
//
// Context cancellation is checked before InstallMu acquire and inside resolve/install.
func EnsureInstalled(
	ctx context.Context,
	cache *Cache,
	binDir, toolName, modulePath, installPath, requestedVersion string,
) (InstallResult, error) {

	// — FAST PATH —

	if v, ok := cache.Load(toolName); ok && (v == "unknown" || v == requestedVersion) {
		return InstallResult{Version: v, NewlyInstalled: false}, nil
	}

	if requestedVersion != "latest" {
		installed, err := ReadInstalledVersion(binDir, toolName, modulePath)
		if err == nil && (installed == "unknown" || installed == requestedVersion) {
			cache.Store(toolName, installed)
			return InstallResult{Version: installed, NewlyInstalled: false}, nil
		}
	}

	// — SLOW PATH —

	select {
	case <-ctx.Done():
		return InstallResult{}, ctx.Err()
	default:
	}

	InstallMu.Lock()
	defer InstallMu.Unlock()

	// Re-check after acquiring lock.
	if v, ok := cache.Load(toolName); ok && (v == "unknown" || v == requestedVersion) {
		return InstallResult{Version: v, NewlyInstalled: false}, nil
	}

	if requestedVersion != "latest" {
		installed, err := ReadInstalledVersion(binDir, toolName, modulePath)
		if err == nil && (installed == "unknown" || installed == requestedVersion) {
			cache.Store(toolName, installed)
			return InstallResult{Version: installed, NewlyInstalled: false}, nil
		}
	}

	select {
	case <-ctx.Done():
		return InstallResult{}, ctx.Err()
	default:
	}

	// Resolve version.  "latest" resolution happens inside InstallMu.
	resolved := requestedVersion
	if requestedVersion == "latest" {
		v, err := ResolveLatest(ctx, modulePath)
		if err != nil {
			return InstallResult{}, fmt.Errorf("resolving latest for %s: %w", toolName, err)
		}
		resolved = v

		// Re-check cache with the now-resolved concrete version.
		if v2, ok := cache.Load(toolName); ok && (v2 == "unknown" || v2 == resolved) {
			return InstallResult{Version: v2, NewlyInstalled: false}, nil
		}
	}

	// Install.
	pkgWithVersion := fmt.Sprintf("%s@%s", installPath, resolved)
	cmd := exec.CommandContext(ctx, "go", "install", pkgWithVersion)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Never cache a failed install.
		return InstallResult{}, fmt.Errorf(
			"install failed: go install %s. exit code %d. stderr: %s",
			pkgWithVersion, cmd.ProcessState.ExitCode(), string(output),
		)
	}

	// Verify binary exists on disk (go install may succeed but produce no binary).
	binaryPath := filepath.Join(binDir, toolName)
	if _, err := os.Stat(binaryPath); err != nil {
		return InstallResult{}, fmt.Errorf("installed %s but binary not found at %s: %w", toolName, binaryPath, err)
	}

	cache.Store(toolName, resolved)
	return InstallResult{Version: resolved, NewlyInstalled: true}, nil
}
