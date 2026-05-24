package root

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrNotGoProject = errors.New("not a Go project: no go.mod or go.work found")

func Discover(startPath string) (string, error) {
	dir, err := filepath.Abs(startPath)
	if err != nil {
		return "", err
	}

	root, found := walkUpFor(dir, "go.work")
	if found {
		return root, nil
	}

	root, found = walkUpFor(dir, "go.mod")
	if found {
		return root, nil
	}

	return "", ErrNotGoProject
}

func walkUpFor(start, filename string) (string, bool) {
	dir := start
	for {
		path := filepath.Join(dir, filename)
		if _, err := os.Stat(path); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func WorkspaceModules(projectRoot string) ([]string, error) {
	workFile := filepath.Join(projectRoot, "go.work")
	data, err := os.ReadFile(workFile) // #nosec G304 — workFile built from discovered project root, not user input
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			modFile := filepath.Join(projectRoot, "go.mod")
			modData, modErr := os.ReadFile(modFile) // #nosec G304 — modFile built from project root
			if modErr != nil {
				return nil, fmt.Errorf("reading go.mod: %w", modErr)
			}
			moduleName := extractModuleName(string(modData))
			if moduleName == "" {
				return nil, nil
			}
			return []string{moduleName}, nil
		}
		return nil, fmt.Errorf("reading go.work: %w", err)
	}

	moduleDirs, err := parseUseDirectives(string(data))
	if err != nil {
		return nil, fmt.Errorf("parsing go.work: %w", err)
	}

	var modules []string
	for _, relPath := range moduleDirs {
		modData, err := os.ReadFile(filepath.Join(projectRoot, relPath, "go.mod")) // #nosec G304 — paths from go.work use directives, not user input
		if err != nil {
			continue
		}
		moduleName := extractModuleName(string(modData))
		if moduleName != "" {
			modules = append(modules, moduleName)
		}
	}
	return modules, nil
}

func parseUseDirectives(workContent string) ([]string, error) {
	var dirs []string
	lines := strings.Split(workContent, "\n")
	inBlock := false

	for _, raw := range lines {
		line := strings.TrimSpace(raw)

		if idx := strings.Index(line, "//"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		if line == "" {
			continue
		}

		if line == "use (" {
			inBlock = true
			continue
		}

		if inBlock {
			if line == ")" {
				inBlock = false
				continue
			}
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			dirs = append(dirs, fields[0])
			continue
		}

		if strings.HasPrefix(line, "use ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				dirs = append(dirs, fields[1])
			}
		}
	}

	return dirs, nil
}

func extractModuleName(modContent string) string {
	for _, line := range strings.Split(modContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "module "))
		}
	}
	return ""
}
