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
	"golangci-lint": {"--output.text.path", "--output.json.path", "--out-format"},
	"govulncheck":   {"-json"},
	"nilaway":       {"-json", "-pretty-print"},
}

// Pinned tool versions. Keep these in sync with the Dockerfile and the docs;
// TestPinnedVersionsAreConsistent enforces this, and the version of the
// golangci-lint pin is additionally enforced across the Makefile, CI, and
// example configs.
const (
	// GolangciLintVersion is the pinned golangci-lint version.
	GolangciLintVersion = "v2.14.0"
	// GovulncheckVersion is the pinned govulncheck version. The vulnerability
	// database is still fetched at run time; this pins the analysis binary.
	GovulncheckVersion = "v1.9.0"
	// NilawayVersion is the pinned nilaway version (upstream publishes no tags,
	// so this is a pseudo-version). Note: nilaway requires Go >= 1.26.0 to
	// build, so GOTOOLCHAIN=auto fetches a second toolchain to install it.
	NilawayVersion = "v0.0.0-20260918162853-acb8859b9031"
)

// DefaultVersion returns the pinned version for a tool, or "latest" for tools
// without a pin. This is the single source for both Default() and Load().
func DefaultVersion(toolName string) string {
	switch toolName {
	case "golangci-lint":
		return GolangciLintVersion
	case "govulncheck":
		return GovulncheckVersion
	case "nilaway":
		return NilawayVersion
	default:
		return "latest"
	}
}

// PinLocations maps each file that carries a tool pin to the tools pinned in
// it, relative to the repository root. It is the single source of truth for the
// pin surface: cmd/bump-tool-pins rewrites exactly these files, and the
// consistency tests assert every one of them agrees with DefaultVersion.
//
// A file that gains a pin must be added here, or it will be neither bumped nor
// guarded. The example configs under examples/config deliberately omit version
// lines: a tool listed without one inherits DefaultVersion at load time.
var PinLocations = map[string][]string{
	"internal/config/config.go":  {"golangci-lint", "govulncheck", "nilaway"},
	"Dockerfile":                 {"golangci-lint", "govulncheck", "nilaway"},
	"README.md":                  {"golangci-lint", "govulncheck", "nilaway"},
	"docs/agents/AGENTS.md":      {"golangci-lint", "govulncheck", "nilaway"},
	"docs/agents/reference.md":   {"golangci-lint", "govulncheck", "nilaway"},
	"Makefile":                   {"golangci-lint"},
	".github/workflows/lint.yml": {"golangci-lint"},
}

func ReservedFlags(toolName string) []string {
	return reservedByTool[toolName]
}

func Default() Config {
	return Config{
		Timeout: 5 * time.Minute,
		Tools: map[string]ToolConfig{
			"golangci-lint": {Version: DefaultVersion("golangci-lint")},
			"govulncheck":   {Version: DefaultVersion("govulncheck")},
			"nilaway":       {Version: DefaultVersion("nilaway")},
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
			argName, _, _ := strings.Cut(arg, "=")
			for _, r := range reserved {
				if argName == r {
					return fmt.Errorf("config error: extra_args for %s contains reserved flag %s", toolName, r)
				}
			}
		}
	}
	return nil
}

// LoadRequired is like Load but returns an error if the file does not exist.
// Use when the path was explicitly specified by the caller (e.g. --config flag).
func LoadRequired(path string) (Config, error) {
	if _, err := os.Stat(path); err != nil {
		return Config{}, fmt.Errorf("config file not found: %w", err)
	}
	return Load(path)
}

func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path) // #nosec G304 — config file read; path from --config flag or project root, legitimate use
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

	for name := range cfg.Tools {
		tc := cfg.Tools[name]
		if tc.Version == "" {
			tc.Version = DefaultVersion(name)
			cfg.Tools[name] = tc
		}
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}
