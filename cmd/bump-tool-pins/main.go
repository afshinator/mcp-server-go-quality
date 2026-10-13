package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/afshinator/mcp-server-go-quality/internal/config"
	"github.com/afshinator/mcp-server-go-quality/internal/discover"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
)

type toolPin struct {
	name    string
	module  string
	version string
}

var toolPins = []toolPin{
	{name: toolname.GolangciLint, module: toolname.ModulePath(toolname.GolangciLint), version: config.GolangciLintVersion},
	{name: toolname.Govulncheck, module: toolname.ModulePath(toolname.Govulncheck), version: config.GovulncheckVersion},
	{name: toolname.Nilaway, module: toolname.ModulePath(toolname.Nilaway), version: config.NilawayVersion},
}

var pinFiles = map[string][]string{
	"internal/config/config.go":          {"golangci-lint", "govulncheck", "nilaway"},
	"Dockerfile":                         {"golangci-lint", "govulncheck", "nilaway"},
	"README.md":                          {"golangci-lint", "govulncheck", "nilaway"},
	"docs/agents/AGENTS.md":              {"golangci-lint", "govulncheck", "nilaway"},
	"docs/agents/reference.md":           {"golangci-lint", "govulncheck", "nilaway"},
	"examples/config/ci.yaml":            {"golangci-lint", "govulncheck", "nilaway"},
	"examples/config/dev-iteration.yaml": {"golangci-lint", "govulncheck", "nilaway"},
	"examples/config/monorepo.yaml":      {"golangci-lint", "govulncheck", "nilaway"},
	"Makefile":                           {"golangci-lint"},
	".github/workflows/lint.yml":         {"golangci-lint"},
}

func main() {
	if err := bump(); err != nil {
		fmt.Fprintln(os.Stderr, "bump-tool-pins:", err)
		os.Exit(1)
	}
}

func bump() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	latest := make(map[string]string, len(toolPins))
	for _, tool := range toolPins {
		version, err := discover.ResolveLatest(ctx, tool.module)
		if err != nil {
			return err
		}
		latest[tool.name] = version
	}

	type update struct {
		path string
		data []byte
		mode os.FileMode
	}
	updates := make([]update, 0, len(pinFiles))
	for path, names := range pinFiles {
		body, err := os.ReadFile(path) // #nosec G304 — path comes from the fixed pinFiles list
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		pins := make([]toolPin, 0, len(names))
		for _, name := range names {
			for _, tool := range toolPins {
				if tool.name == name {
					pins = append(pins, tool)
					break
				}
			}
		}
		updated, err := replacePins(string(body), pins, latest)
		if err != nil {
			return fmt.Errorf("updating %s: %w", path, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stating %s: %w", path, err)
		}
		updates = append(updates, update{path: path, data: []byte(updated), mode: info.Mode()})
	}

	changed := 0
	for _, file := range updates {
		original, err := os.ReadFile(file.path) // #nosec G304 — path comes from the fixed pinFiles list
		if err != nil {
			return fmt.Errorf("reading %s before write: %w", file.path, err)
		}
		if string(original) == string(file.data) {
			continue
		}
		if err := os.WriteFile(file.path, file.data, file.mode.Perm()); err != nil {
			return fmt.Errorf("writing %s: %w", file.path, err)
		}
		changed++
		fmt.Println("updated", file.path)
	}
	if changed == 0 {
		fmt.Println("all tool pins are already current")
	}
	for _, tool := range toolPins {
		fmt.Printf("%s: %s\n", tool.name, latest[tool.name])
	}
	return nil
}

func replacePins(content string, pins []toolPin, latest map[string]string) (string, error) {
	replacements := make([]string, 0, len(pins)*2)
	for _, pin := range pins {
		if strings.Count(content, pin.version) == 0 {
			return "", fmt.Errorf("%s pin %s not found", pin.name, pin.version)
		}
		replacements = append(replacements, pin.version, latest[pin.name])
	}
	return strings.NewReplacer(replacements...).Replace(content), nil
}
