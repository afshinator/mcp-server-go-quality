package toolname

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
