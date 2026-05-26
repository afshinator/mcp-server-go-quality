package discover

import (
	"context"
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

func TestCacheLoadStoreInvalidateResolved(t *testing.T) {
	c := NewCache()

	if _, ok := c.LoadResolved("govulncheck"); ok {
		t.Error("fresh cache should have no resolved entry")
	}

	c.StoreResolved("govulncheck", "v1.3.0")
	v, ok := c.LoadResolved("govulncheck")
	if !ok || v != "v1.3.0" {
		t.Errorf("LoadResolved = (%q, %v), want (v1.3.0, true)", v, ok)
	}

	c.InvalidateResolved("govulncheck")
	if _, ok := c.LoadResolved("govulncheck"); ok {
		t.Error("after InvalidateResolved, LoadResolved should miss")
	}
}

func TestEnsureInstalledCachesResolvedVersion(t *testing.T) {
	callCount := 0
	c := NewCache()
	c.resolveLatestFn = func(_ context.Context, _ string) (string, error) {
		callCount++
		return "v1.5.0", nil
	}
	// Pre-populate installed version cache so EnsureInstalled thinks binary is at v1.5.0.
	c.Store("testtool", "v1.5.0")

	// First call: resolved cache is empty — resolveLatestFn is invoked.
	r1, err := EnsureInstalled(context.Background(), c, "/fake/bin", "testtool",
		"some/module", "some/module/cmd/testtool", "latest")
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if r1.Version != "v1.5.0" {
		t.Errorf("first call version = %q, want v1.5.0", r1.Version)
	}
	if callCount != 1 {
		t.Errorf("after first call: resolveLatest called %d times, want 1", callCount)
	}

	// Second call: resolved cache is warm — resolveLatestFn must NOT be called again.
	r2, err := EnsureInstalled(context.Background(), c, "/fake/bin", "testtool",
		"some/module", "some/module/cmd/testtool", "latest")
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if r2.Version != "v1.5.0" {
		t.Errorf("second call version = %q, want v1.5.0", r2.Version)
	}
	if callCount != 1 {
		t.Errorf("after second call: resolveLatest called %d times, want 1 (should be cached)", callCount)
	}
}

func TestEnsureInstalledInvalidateResolved(t *testing.T) {
	callCount := 0
	c := NewCache()
	c.resolveLatestFn = func(_ context.Context, _ string) (string, error) {
		callCount++
		return "v1.5.0", nil
	}
	c.Store("testtool", "v1.5.0")

	_, _ = EnsureInstalled(context.Background(), c, "/fake/bin", "testtool",
		"some/module", "some/module/cmd/testtool", "latest")
	if callCount != 1 {
		t.Fatalf("expected 1 resolve call after first install, got %d", callCount)
	}

	// Simulate install_tools: invalidate before calling EnsureInstalled.
	c.InvalidateResolved("testtool")
	_, _ = EnsureInstalled(context.Background(), c, "/fake/bin", "testtool",
		"some/module", "some/module/cmd/testtool", "latest")
	if callCount != 2 {
		t.Errorf("resolveLatestFn called %d times after InvalidateResolved, want 2", callCount)
	}
}
