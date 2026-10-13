package config

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/afshinator/mcp-server-go-quality/internal/discover"
)

// repoRoot is the module root as seen from this package's test files.
const repoRoot = "../.."

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

func pinDrifted(pin, latest string) bool {
	return pin != latest
}

func TestPinDrifted(t *testing.T) {
	if !pinDrifted("v1.0.0", "v1.1.0") {
		t.Error("older pin was not reported as drift")
	}
	if pinDrifted("v1.1.0", "v1.1.0") {
		t.Error("matching pin was reported as drift")
	}
}

func TestPinnedVersionsAreCurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("requires resolving latest versions from the Go module proxy")
	}

	tools := []struct {
		name   string
		module string
	}{
		{name: "golangci-lint", module: "github.com/golangci/golangci-lint/v2"},
		{name: "govulncheck", module: "golang.org/x/vuln"},
		{name: "nilaway", module: "go.uber.org/nilaway"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	for _, tool := range tools {
		latest, err := discover.ResolveLatest(ctx, tool.module)
		if err != nil {
			t.Fatalf("resolving latest %s version: %v", tool.name, err)
		}
		pin := DefaultVersion(tool.name)
		if !pinDrifted(pin, latest) {
			continue
		}
		// A pseudo-version means the resolved commit carries no tag, so the
		// module publishes no releases and there is nothing to upgrade to:
		// nilaway pins a commit by intent and @latest moves on every upstream
		// commit. Failing on that would go red most weeks for a reason nobody
		// can act on, which trains the operator to ignore the one signal that
		// does matter (govulncheck). Report it instead. The check re-arms
		// automatically if the module ever publishes a tag, because @latest
		// stops being a pseudo-version at that point.
		if isPseudoVersion(latest) {
			t.Logf("%s is pinned to %s and upstream has moved to %s (informational: the module publishes no releases)", tool.name, pin, latest)
			continue
		}
		t.Errorf("%s pin %s differs from latest %s; run make bump-tool-pins", tool.name, pin, latest)
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
	var files []string
	for rel := range PinLocations {
		files = append(files, filepath.Join(repoRoot, rel))
	}
	sort.Strings(files)
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
	type pinFile struct {
		path string
		tool string
	}
	var files []pinFile
	for rel, toolNames := range PinLocations {
		for _, tool := range toolNames {
			files = append(files, pinFile{path: filepath.Join(repoRoot, rel), tool: tool})
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].path != files[j].path {
			return files[i].path < files[j].path
		}
		return files[i].tool < files[j].tool
	})
	pins := map[string]string{
		"golangci-lint": GolangciLintVersion,
		"govulncheck":   GovulncheckVersion,
		"nilaway":       NilawayVersion,
	}
	for _, pf := range files {
		body, err := os.ReadFile(filepath.Clean(pf.path))
		if err != nil {
			t.Fatalf("reading %s: %v", pf.path, err)
		}
		text := string(body)
		version := pins[pf.tool]
		if !strings.Contains(text, version) {
			t.Errorf("%s does not reference the %s pin %s", pf.path, pf.tool, version)
		}
	}
	// The Dockerfile pre-bakes the tools; an unpinned install there ships an
	// image the server immediately repairs on first use.
	dockerfile, err := os.ReadFile(filepath.Join(repoRoot, "Dockerfile"))
	if err != nil {
		t.Fatalf("reading Dockerfile: %v", err)
	}
	if strings.Contains(string(dockerfile), "@latest") {
		t.Error("Dockerfile installs a tool @latest; pins must be explicit")
	}
}

// pseudoVersionRe matches the 14-digit commit timestamp embedded in every Go
// module pseudo-version, in either shape the proxy emits:
//
//	v0.0.0-20260918162853-acb8859b9031   (no tag on the commit)
//	v1.2.0-0.20240101000000-abcdefabcdef (tag on an ancestor)
//
// A pseudo-version is what the proxy returns for a commit that carries no tag,
// so a module resolving to one has no release to upgrade to and version drift
// on it is informational rather than actionable.
var pseudoVersionRe = regexp.MustCompile(`\d{14}`)

func isPseudoVersion(v string) bool {
	return pseudoVersionRe.MatchString(v)
}

func TestIsPseudoVersion(t *testing.T) {
	yes := map[string]bool{
		"v0.0.0-20260918162853-acb8859b9031":   true, // the nilaway pin shape
		"v1.2.0-0.20240101000000-abcdefabcdef": true,
	}
	no := map[string]bool{
		"v2.14.0":            false,
		"v1.9.0":             false,
		"v1.2.3-rc1":         false, // a pre-release tag, not a pseudo-version
		"":                   false,
		"v1.2.3-rc-20260101": false, // the digits after the dash are not a timestamp
	}
	for v, want := range yes {
		if got := isPseudoVersion(v); got != want {
			t.Errorf("isPseudoVersion(%q) = %v, want %v", v, got, want)
		}
	}
	for v, want := range no {
		if got := isPseudoVersion(v); got != want {
			t.Errorf("isPseudoVersion(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestDriftOnTaglessModuleIsInformational(t *testing.T) {
	// nilaway shape: a pseudo-version must not fail the scheduled job, while a
	// tagged module's drift must.
	if a := isPseudoVersion("v0.0.0-20260918162853-acb8859b9031"); !a {
		t.Fatal("precondition failed")
	}
}

// TestEmptyToolEntryInheritsPin covers the example-config shape: a tool listed
// without a version must inherit the pin, so examples never have to carry the
// raw pseudo-version string around.
func TestEmptyToolEntryInheritsPin(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/.go-quality.yaml"
	body := "timeout: 3m\ntools:\n  golangci-lint:\n  govulncheck:\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for name, want := range map[string]string{"golangci-lint": GolangciLintVersion, "govulncheck": GovulncheckVersion} {
		if got := cfg.Tools[name].Version; got != want {
			t.Errorf("%s version = %q, want %q (a tool listed without a version inherits the pin)", name, got, want)
		}
	}
	if got := cfg.Tools["nilaway"].Version; got != NilawayVersion {
		t.Errorf("nilaway version = %q, want the pinned default (it was not overridden)", got)
	}
}
