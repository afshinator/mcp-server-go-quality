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
	if binDir := strings.TrimSpace(string(out)); binDir != "" {
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
	data map[string]string // toolName → installed concrete semver

	resolved        sync.Map                                                     // toolName → resolved semver for "latest" (process lifetime)
	resolveLatestFn func(ctx context.Context, modulePath string) (string, error) // nil = use ResolveLatest
}

func NewCache() *Cache {
	return &Cache{data: make(map[string]string)}
}

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

func (c *Cache) LoadResolved(toolName string) (string, bool) {
	if v, ok := c.resolved.Load(toolName); ok {
		return v.(string), true //nolint:forcetypeassert // sync.Map only receives string values via StoreResolved
	}
	return "", false
}

func (c *Cache) StoreResolved(toolName, version string) {
	c.resolved.Store(toolName, version)
}

func (c *Cache) InvalidateResolved(toolName string) {
	c.resolved.Delete(toolName)
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

func ReadInstalledVersion(ctx context.Context, binDir, toolName, modulePath string) (string, error) {
	binaryPath := filepath.Join(binDir, toolName)
	cmd := exec.CommandContext(ctx, "go", "version", "-m", binaryPath) // #nosec G204 — binaryPath from trusted binDir, not user input
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go version -m %s: %w", binaryPath, err)
	}
	version := ParseModuleVersion(output, modulePath)
	return version, nil
}

func ResolveLatest(ctx context.Context, modulePath string) (string, error) {
	args := []string{"list", "-m", "-json", modulePath + "@latest"}
	cmd := exec.CommandContext(ctx, "go", args...) // #nosec G204 — args built from internal constants, not user input
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

type InstallResult struct {
	Version        string
	NewlyInstalled bool
}

// resolveVersion returns the concrete semver to install. For "latest" it checks
// the resolved cache first, calling resolveFn (and caching the result) only on a miss.
func resolveVersion(ctx context.Context, cache *Cache, toolName, modulePath, requestedVersion string) (string, error) {
	if requestedVersion != "latest" {
		return requestedVersion, nil
	}
	if cached, ok := cache.LoadResolved(toolName); ok {
		return cached, nil
	}
	resolveFn := cache.resolveLatestFn
	if resolveFn == nil {
		resolveFn = ResolveLatest
	}
	v, err := resolveFn(ctx, modulePath)
	if err != nil {
		return "", fmt.Errorf("resolving latest for %s: %w", toolName, err)
	}
	cache.StoreResolved(toolName, v)
	return v, nil
}

// cacheHit returns a non-empty version string when the in-memory caches show the
// tool is already installed at the right version. It covers both pinned-version and
// "latest" (resolved cache) cases.
func cacheHit(cache *Cache, toolName, requestedVersion string) (string, bool) {
	if v, ok := cache.Load(toolName); ok && (v == "unknown" || v == requestedVersion) {
		return v, true
	}
	if requestedVersion == "latest" {
		if resolved, ok := cache.LoadResolved(toolName); ok {
			if v, ok := cache.Load(toolName); ok && v == resolved {
				return v, true
			}
		}
	}
	return "", false
}

// diskHit reads the installed binary version for a pinned request and returns a hit
// when the on-disk version already satisfies the request.
func diskHit(ctx context.Context, cache *Cache, binDir, toolName, modulePath, requestedVersion string) (string, bool) {
	if requestedVersion == "latest" {
		return "", false
	}
	installed, err := ReadInstalledVersion(ctx, binDir, toolName, modulePath)
	if err != nil {
		return "", false
	}
	if installed == "unknown" || installed == requestedVersion {
		cache.Store(toolName, installed)
		return installed, true
	}
	return "", false
}

// installBinary runs "go install <installPath>@<resolved>" and verifies the binary exists.
func installBinary(ctx context.Context, cache *Cache, binDir, toolName, installPath, resolved string) (InstallResult, error) {
	pkgWithVersion := fmt.Sprintf("%s@%s", installPath, resolved)
	cmd := exec.CommandContext(ctx, "go", "install", pkgWithVersion) // #nosec G204 — install path from internal toolname constants, not user input
	output, err := cmd.CombinedOutput()
	if err != nil {
		return InstallResult{}, fmt.Errorf(
			"install failed: go install %s. exit code %d. stderr: %s",
			pkgWithVersion, cmd.ProcessState.ExitCode(), string(output),
		)
	}
	binaryPath := filepath.Join(binDir, toolName)
	if _, err := os.Stat(binaryPath); err != nil {
		return InstallResult{}, fmt.Errorf("installed %s but binary not found at %s: %w", toolName, binaryPath, err)
	}
	cache.Store(toolName, resolved)
	return InstallResult{Version: resolved, NewlyInstalled: true}, nil
}

func EnsureInstalled(
	ctx context.Context,
	cache *Cache,
	binDir, toolName, modulePath, installPath, requestedVersion string,
) (InstallResult, error) {
	// Fast paths before taking the lock.
	if v, hit := cacheHit(cache, toolName, requestedVersion); hit {
		return InstallResult{Version: v}, nil
	}
	if v, hit := diskHit(ctx, cache, binDir, toolName, modulePath, requestedVersion); hit {
		return InstallResult{Version: v}, nil
	}

	if err := ctx.Err(); err != nil {
		return InstallResult{}, err
	}

	InstallMu.Lock()
	defer InstallMu.Unlock()

	// Re-check under the lock (another goroutine may have installed while we waited).
	if v, hit := cacheHit(cache, toolName, requestedVersion); hit {
		return InstallResult{Version: v}, nil
	}
	if v, hit := diskHit(ctx, cache, binDir, toolName, modulePath, requestedVersion); hit {
		return InstallResult{Version: v}, nil
	}

	if err := ctx.Err(); err != nil {
		return InstallResult{}, err
	}

	resolved, err := resolveVersion(ctx, cache, toolName, modulePath, requestedVersion)
	if err != nil {
		return InstallResult{}, err
	}

	// After resolution the installed version may already match (e.g. v1.5.0 already present).
	if v, ok := cache.Load(toolName); ok && (v == "unknown" || v == resolved) {
		return InstallResult{Version: v}, nil
	}

	return installBinary(ctx, cache, binDir, toolName, installPath, resolved)
}
