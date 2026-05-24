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
