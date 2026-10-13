package toolname

import (
	"fmt"
	"strings"
)

const (
	GolangciLint = "golangci-lint"
	Govulncheck  = "govulncheck"
	Nilaway      = "nilaway"
)

func IsValid(name string) bool {
	switch name {
	case GolangciLint, Govulncheck, Nilaway:
		return true
	default:
		return false
	}
}

// Resolve validates a requested tool selection. An empty selection means every
// tool. Both entrypoints resolve their selection through here so a name that is
// accepted by one is accepted by the other.
// Resolve validates a requested tool selection. An empty selection means every
// tool. Duplicates are removed so a repeated name does not run the same checker
// twice.
func Resolve(names []string) ([]string, error) {
	if len(names) == 0 {
		return All(), nil
	}
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, n := range names {
		if !IsValid(n) {
			return nil, fmt.Errorf("unknown tool %q. valid values: %s", n, strings.Join(All(), ", "))
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil
}

func All() []string {
	return []string{GolangciLint, Govulncheck, Nilaway}
}

func InstallPath(name string) string {
	switch name {
	case GolangciLint:
		return "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
	case Govulncheck:
		return "golang.org/x/vuln/cmd/govulncheck"
	case Nilaway:
		return "go.uber.org/nilaway/cmd/nilaway"
	default:
		return ""
	}
}

// ModulePath is the module a tool's binary is built from. It is the sibling of
// InstallPath: the installer needs both, so they are kept together.
func ModulePath(name string) string {
	switch name {
	case GolangciLint:
		return "github.com/golangci/golangci-lint/v2"
	case Govulncheck:
		return "golang.org/x/vuln"
	case Nilaway:
		return "go.uber.org/nilaway"
	default:
		return ""
	}
}
