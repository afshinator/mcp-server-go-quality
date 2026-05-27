package discover

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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

// TestEnsureInstalledDoubleCheckPreventsDuplicateInstall verifies that
// when N concurrent requests arrive and none have the tool cached, exactly
// 1 install occurs — the second check under InstallMu prevents redundant installs.
func TestEnsureInstalledDoubleCheckPreventsDuplicateInstall(t *testing.T) {
	var installCount atomic.Int64
	var mu sync.Mutex
	installed := false

	c := NewCache()
	c.installFn = func(_ context.Context, cache *Cache, binDir, toolName, installPath, resolved string) (InstallResult, error) {
		mu.Lock()
		defer mu.Unlock()
		if installed {
			t.Error("installFn called after tool was already marked installed — double-check failed")
		}
		installed = true
		installCount.Add(1)
		cache.Store(toolName, resolved)
		return InstallResult{Version: resolved, NewlyInstalled: true}, nil
	}

	const n = 20
	var wg sync.WaitGroup
	results := make([]InstallResult, n)
	errs := make([]error, n)

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = EnsureInstalled(
				context.Background(), c, "/fake/bin", "testtool",
				"some/module", "some/module/cmd/testtool", "v1.0.0",
			)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}

	if n := installCount.Load(); n != 1 {
		t.Errorf("installCount = %d, want exactly 1", n)
	}

	for i, r := range results {
		if r.Version != "v1.0.0" {
			t.Errorf("goroutine %d: version = %q, want v1.0.0", i, r.Version)
		}
	}
}

// TestEnsureInstalledConcurrentLatestResolution verifies that when N concurrent
// requests all ask for "latest", only 1 version resolution occurs.
func TestEnsureInstalledConcurrentLatestResolution(t *testing.T) {
	var resolveCount atomic.Int64
	var installCount atomic.Int64
	var mu sync.Mutex
	installed := false

	c := NewCache()
	c.resolveLatestFn = func(_ context.Context, _ string) (string, error) {
		resolveCount.Add(1)
		return "v2.0.0", nil
	}
	c.installFn = func(_ context.Context, cache *Cache, binDir, toolName, installPath, resolved string) (InstallResult, error) {
		mu.Lock()
		defer mu.Unlock()
		if installed {
			t.Error("installFn called after tool was already marked installed")
		}
		installed = true
		installCount.Add(1)
		cache.Store(toolName, resolved)
		return InstallResult{Version: resolved, NewlyInstalled: true}, nil
	}

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = EnsureInstalled(
				context.Background(), c, "/fake/bin", "testtool",
				"some/module", "some/module/cmd/testtool", "latest",
			)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}

	if n := resolveCount.Load(); n != 1 {
		t.Errorf("resolveCount = %d, want exactly 1", n)
	}
	if n := installCount.Load(); n != 1 {
		t.Errorf("installCount = %d, want exactly 1", n)
	}
}

// TestEnsureInstalledFailureNotCached verifies that a failed install does not
// poison the cache — a subsequent call should retry the install.
func TestEnsureInstalledFailureNotCached(t *testing.T) {
	var calls atomic.Int64
	fault := errors.New("simulated install failure")

	c := NewCache()
	c.installFn = func(_ context.Context, cache *Cache, binDir, toolName, installPath, resolved string) (InstallResult, error) {
		calls.Add(1)
		if calls.Load() == 1 {
			return InstallResult{}, fault
		}
		cache.Store(toolName, resolved)
		return InstallResult{Version: resolved, NewlyInstalled: true}, nil
	}

	// First call: install fails.
	_, err := EnsureInstalled(context.Background(), c, "/fake/bin", "testtool",
		"some/module", "some/module/cmd/testtool", "v1.0.0")
	if err == nil {
		t.Fatal("expected error from failed install, got nil")
	}
	if v, ok := c.Load("testtool"); ok {
		t.Errorf("cache has entry after failed install: %q", v)
	}

	// Second call: install succeeds.
	r, err := EnsureInstalled(context.Background(), c, "/fake/bin", "testtool",
		"some/module", "some/module/cmd/testtool", "v1.0.0")
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if r.Version != "v1.0.0" {
		t.Errorf("version = %q, want v1.0.0", r.Version)
	}
	if !r.NewlyInstalled {
		t.Error("expected NewlyInstalled = true on second attempt")
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("installFn called %d times, want 2 (fail + succeed)", n)
	}
}

// TestEnsureInstalledUnknownVersionAlwaysMatches verifies that a cached
// "unknown" version satisfies all requested versions without reinstalling.
func TestEnsureInstalledUnknownVersionAlwaysMatches(t *testing.T) {
	var installCount atomic.Int64

	c := NewCache()
	c.Store("testtool", "unknown")
	c.installFn = func(_ context.Context, cache *Cache, binDir, toolName, installPath, resolved string) (InstallResult, error) {
		installCount.Add(1)
		cache.Store(toolName, resolved)
		return InstallResult{Version: resolved, NewlyInstalled: true}, nil
	}

	versions := []string{"v1.0.0", "v2.0.0", "v3.5.1", "latest"}
	for _, ver := range versions {
		r, err := EnsureInstalled(context.Background(), c, "/fake/bin", "testtool",
			"some/module", "some/module/cmd/testtool", ver)
		if err != nil {
			t.Fatalf("version %q: %v", ver, err)
		}
		if r.NewlyInstalled {
			t.Errorf("version %q: NewlyInstalled = true, want false (unknown cache hit)", ver)
		}
	}

	if n := installCount.Load(); n != 0 {
		t.Errorf("installFn called %d times, want 0 — unknown version should always be a cache hit", n)
	}
}

// TestEnsureInstalledContextCancellation verifies that a cancelled context
// causes EnsureInstalled to exit promptly while waiting on the install lock.
func TestEnsureInstalledContextCancellation(t *testing.T) {
	blockCh := make(chan struct{})
	releaseCh := make(chan struct{})

	c := NewCache()
	c.installFn = func(ctx context.Context, cache *Cache, binDir, toolName, installPath, resolved string) (InstallResult, error) {
		close(blockCh)
		<-releaseCh
		return InstallResult{}, ctx.Err()
	}

	// Goroutine 1: holds InstallMu (blocked inside installFn).
	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		_, _ = EnsureInstalled(context.Background(), c, "/fake/bin", "testtool",
			"some/module", "some/module/cmd/testtool", "v1.0.0")
	}()

	// Wait for goroutine 1 to enter installFn (it now holds InstallMu).
	<-blockCh

	// Goroutine 2: tries to acquire InstallMu with a cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done2 := make(chan struct{})
	var err2 error
	go func() {
		defer close(done2)
		_, err2 = EnsureInstalled(ctx, c, "/fake/bin", "testtool",
			"some/module", "some/module/cmd/testtool", "v2.0.0")
	}()

	<-done2
	if err2 == nil {
		t.Error("expected context cancellation error, got nil")
	} else if !errors.Is(err2, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err2)
	}

	// Clean up: release goroutine 1.
	close(releaseCh)
	<-done1
}
