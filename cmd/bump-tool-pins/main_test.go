package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestPinFilesCoverEveryCopy(t *testing.T) {
	// pinFiles is config.PinLocations, the single source of truth for the pin
	// surface. The example configs are deliberately absent: a tool listed in
	// them without a version inherits the pin at load time, so there is nothing
	// to rewrite.
	want := map[string][]string{
		"internal/config/config.go":  {"golangci-lint", "govulncheck", "nilaway"},
		"Dockerfile":                 {"golangci-lint", "govulncheck", "nilaway"},
		"README.md":                  {"golangci-lint", "govulncheck", "nilaway"},
		"docs/agents/AGENTS.md":      {"golangci-lint", "govulncheck", "nilaway"},
		"docs/agents/reference.md":   {"golangci-lint", "govulncheck", "nilaway"},
		"Makefile":                   {"golangci-lint"},
		".github/workflows/lint.yml": {"golangci-lint"},
	}
	if len(pinFiles) != len(want) {
		t.Fatalf("pinFiles has %d entries, want %d", len(pinFiles), len(want))
	}
	for path, names := range want {
		if got, ok := pinFiles[path]; !ok || !slices.Equal(got, names) {
			t.Errorf("pinFiles[%q] = %v, want %v", path, got, names)
		}
	}
	// The example configs must keep inheriting the pin rather than hardcoding
	// it, which is what created the drift the bump tool exists to fix.
	for _, rel := range []string{"examples/config/ci.yaml", "examples/config/dev-iteration.yaml", "examples/config/monorepo.yaml"} {
		body, err := os.ReadFile("../../" + rel) //nolint:gosec // fixed path from the test
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}
		if strings.Contains(string(body), "version:") {
			t.Errorf("%s hardcodes a tool version; example configs must inherit the pin", rel)
		}
	}
}

func TestReplacePinsReplacesEveryOccurrence(t *testing.T) {
	pins := []toolPin{
		{name: "lint", version: "v1.0.0"},
		{name: "vuln", version: "v2.0.0"},
	}
	latest := map[string]string{"lint": "v1.1.0", "vuln": "v2.1.0"}
	content := "lint=v1.0.0 again=v1.0.0 vuln=v2.0.0"

	got, err := replacePins(content, pins, latest)
	if err != nil {
		t.Fatal(err)
	}
	want := "lint=v1.1.0 again=v1.1.0 vuln=v2.1.0"
	if got != want {
		t.Errorf("replacePins() = %q, want %q", got, want)
	}
}

func TestReplacePinsFailsWhenExpectedPinIsMissing(t *testing.T) {
	_, err := replacePins("lint=v1.1.0", []toolPin{{name: "lint", version: "v1.0.0"}}, map[string]string{"lint": "v1.2.0"})
	if err == nil || !strings.Contains(err.Error(), "pin v1.0.0 not found") {
		t.Errorf("replacePins() error = %v, want missing-pin error", err)
	}
}
