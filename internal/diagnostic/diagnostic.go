package diagnostic

import "encoding/json"

// DefaultSeverity is the severity given to a finding whose tool reports none.
//
// govulncheck and nilaway have no severity concept, and golangci-lint v2 leaves
// the field empty unless the repository config defines severity rules. Because
// Severity is `omitempty`, an empty value disappears from the JSON, so any
// consumer filtering on it would sort a reachable CVE below a style warning.
// Findings therefore never go unlabelled.
//
// This applies to findings only. Error diagnostics carry Error instead: a tool
// that failed to run is a failure, not a finding.
const DefaultSeverity = "error"

// SeverityOrDefault returns the tool's severity, or DefaultSeverity when the
// tool reports none.
func SeverityOrDefault(toolSeverity string) string {
	if toolSeverity == "" {
		return DefaultSeverity
	}
	return toolSeverity
}

type Diagnostic struct {
	Tool     string          `json:"tool"`
	File     string          `json:"file"`
	Line     int             `json:"line"`
	Column   int             `json:"column,omitempty"`
	Severity string          `json:"severity,omitempty"`
	Message  string          `json:"message"`
	Error    string          `json:"error"`
	Native   json.RawMessage `json:"native"`
}

// Response is the result envelope shared by every entrypoint that reports
// findings. The MCP tools return it as `structuredContent`; the CLI prints it
// in `--json` mode. Keeping one type is what makes the two byte-identical
// rather than merely similar.
//
// It is an object, not a bare array, because MCP requires structuredContent to
// be one.
type Response struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// NewResponse wraps diagnostics, normalising nil to an empty slice so the
// envelope always carries `[]` rather than `null`.
func NewResponse(diags []Diagnostic) Response {
	if diags == nil {
		diags = []Diagnostic{}
	}
	return Response{Diagnostics: diags}
}
