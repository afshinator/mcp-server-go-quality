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
	data map[string]string
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
	cmd := exec.CommandContext(ctx, "go", "version", "-m", binaryPath)
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

type InstallResult struct {
	Version        string
	NewlyInstalled bool
}

func EnsureInstalled(
	ctx context.Context,
	cache *Cache,
	binDir, toolName, modulePath, installPath, requestedVersion string,
) (InstallResult, error) {

	if v, ok := cache.Load(toolName); ok && (v == "unknown" || v == requestedVersion) {
		return InstallResult{Version: v, NewlyInstalled: false}, nil
	}

	if requestedVersion != "latest" {
		installed, err := ReadInstalledVersion(ctx, binDir, toolName, modulePath)
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

	InstallMu.Lock()
	defer InstallMu.Unlock()

	if v, ok := cache.Load(toolName); ok && (v == "unknown" || v == requestedVersion) {
		return InstallResult{Version: v, NewlyInstalled: false}, nil
	}

	if requestedVersion != "latest" {
		installed, err := ReadInstalledVersion(ctx, binDir, toolName, modulePath)
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

	resolved := requestedVersion
	if requestedVersion == "latest" {
		v, err := ResolveLatest(ctx, modulePath)
		if err != nil {
			return InstallResult{}, fmt.Errorf("resolving latest for %s: %w", toolName, err)
		}
		resolved = v

		if v2, ok := cache.Load(toolName); ok && (v2 == "unknown" || v2 == resolved) {
			return InstallResult{Version: v2, NewlyInstalled: false}, nil
		}
	}

	pkgWithVersion := fmt.Sprintf("%s@%s", installPath, resolved)
	cmd := exec.CommandContext(ctx, "go", "install", pkgWithVersion)
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
