// Command go-quality runs the same three Go quality checkers the MCP server
// exposes, but as a plain CLI for terminals, CI pipelines and scripts.
//
// It shares the whole engine with the MCP server: the same checkers, the same
// pinned tool versions, the same project-root discovery, and the same response
// envelope. `--json` is byte-identical to the MCP tools' structuredContent, so
// the same `jq '.diagnostics'` query works against either.
//
// Exit codes:
//
//	0  no finding reached the --fail-on threshold
//	1  a finding reached the threshold
//	2  the run did not complete: a checker failed to run, the config was
//	   unusable, the project root was not found, or a flag was invalid
//
// stdout carries only the payload for the selected mode. Everything else --
// progress, install messages, the summary line -- goes to stderr, so
// `go-quality --json | jq` is never corrupted by a first-run tool install.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/afshinator/mcp-server-go-quality/internal/checkers"
	"github.com/afshinator/mcp-server-go-quality/internal/config"
	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/discover"
	"github.com/afshinator/mcp-server-go-quality/internal/preflight"
	"github.com/afshinator/mcp-server-go-quality/internal/root"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
	"github.com/afshinator/mcp-server-go-quality/internal/toolname"
	"github.com/afshinator/mcp-server-go-quality/internal/version"
)

const (
	exitOK      = 0
	exitFinding = 1
	exitFailure = 2
)

// failOnValues is the accepted set for --fail-on, in help order.
var failOnValues = []string{"any", "error", "warning", "none"}

// severityRank orders severities most severe first. Only `error` and `warning`
// carry a rank: `any` and `none` are not thresholds on this axis but statements
// about severity in general, so they are handled separately.
var severityRank = map[string]int{"error": 1, "warning": 2}

// Trips reports whether a finding at severity reaches the --fail-on threshold.
//
// `any` trips on every finding regardless of severity and `none` trips on none,
// because neither is a point on the severity axis. For the ranked thresholds it
// is a comparison, not equality: `warning` must trip on an `error`-severity
// finding, or a repository that set the middle tier would let a CVE through.
//
// A severity this build does not know -- a value from a future tool -- is
// treated as the most severe, so an unrecognised severity can never silently
// bypass a gate.
func Trips(threshold, severity string) bool {
	switch threshold {
	case "any":
		return true
	case "none":
		return false
	}
	limit, ok := severityRank[threshold]
	if !ok {
		return true // unreachable: the flag is validated before use
	}
	rank, ok := severityRank[severity]
	if !ok {
		rank = severityRank["error"]
	}
	return rank <= limit
}

// Failures reports whether any diagnostic is a tool failure rather than a
// finding. These force a non-zero exit unconditionally, including under
// --fail-on=none: a checker that never ran is not a clean result.
func Failures(diags []diagnostic.Diagnostic) []diagnostic.Diagnostic {
	var out []diagnostic.Diagnostic
	for _, d := range diags {
		if d.Error != "" {
			out = append(out, d)
		}
	}
	return out
}

// ExitCode derives the process status from the parsed diagnostics. It never
// consults a checker's own exit status: govulncheck and nilaway both exit 0 in
// the JSON mode this engine invokes, even when they report findings.
func ExitCode(diags []diagnostic.Diagnostic, threshold string) int {
	if len(Failures(diags)) > 0 {
		return exitFailure
	}
	for _, d := range diags {
		if Trips(threshold, d.Severity) {
			return exitFinding
		}
	}
	return exitOK
}

// MarshalJSON renders the same envelope the MCP tools return as
// structuredContent.
func MarshalJSON(diags []diagnostic.Diagnostic) ([]byte, error) {
	return json.Marshal(diagnostic.NewResponse(diags))
}

// renderText is the human-readable rendering. It returns a string rather than
// streaming so the caller has exactly one write to check.
func renderText(diags []diagnostic.Diagnostic) string {
	var b strings.Builder
	for _, d := range diags {
		if d.Error != "" {
			fmt.Fprintf(&b, "%s: FAILED: %s\n", d.Tool, d.Error)
			continue
		}
		location := fmt.Sprintf("%s:%d", d.File, d.Line)
		if d.Column > 0 {
			location = fmt.Sprintf("%s:%d", location, d.Column)
		}
		if d.Severity != "" {
			fmt.Fprintf(&b, "%s  %s  %s  %s\n", location, d.Tool, d.Severity, d.Message)
			continue
		}
		fmt.Fprintf(&b, "%s  %s  %s\n", location, d.Tool, d.Message)
	}
	return b.String()
}

// warn writes a progress or diagnostic line to stderr. Nothing here is the
// payload, and a failure to write it is not actionable -- the process is ending
// either way -- so the error is deliberately dropped.
func warn(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

type options struct {
	configPath  string
	projectPath string
	tools       string
	failOn      string
	jsonOut     bool
	installOnly bool
	showVersion bool
}

func parseFlags(args []string, stderr io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("go-quality", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.configPath, "config", "", "path to .go-quality.yaml (default: .go-quality.yaml in the working directory)")
	fs.StringVar(&o.tools, "tools", "", "comma-separated subset of "+strings.Join(toolname.All(), ", ")+" (default: all)")
	fs.StringVar(&o.failOn, "fail-on", "any", "exit 1 when a finding reaches this severity: "+strings.Join(failOnValues, ", "))
	fs.BoolVar(&o.jsonOut, "json", false, "write the {\"diagnostics\": [...]} envelope to stdout instead of text")
	fs.BoolVar(&o.installOnly, "install-only", false, "install the pinned tools and exit without running checks")
	fs.BoolVar(&o.showVersion, "version", false, "print version and exit")
	fs.Usage = func() {
		warn(stderr, "usage: go-quality [flags] [project-path]\n\nflags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return o, err
	}

	valid := false
	for _, v := range failOnValues {
		if o.failOn == v {
			valid = true
		}
	}
	if !valid {
		return o, fmt.Errorf("invalid --fail-on %q: valid values are %s", o.failOn, strings.Join(failOnValues, ", "))
	}

	o.projectPath = "."
	if rest := fs.Args(); len(rest) > 0 {
		if len(rest) > 1 {
			return o, fmt.Errorf("expected at most one project path, got %d", len(rest))
		}
		o.projectPath = rest[0]
	}
	return o, nil
}

func requestedTools(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return toolname.Resolve(nil)
	}
	parts := strings.Split(raw, ",")
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	return toolname.Resolve(names)
}

// loadConfig mirrors the MCP server's precedence exactly: an explicit --config
// is required to exist, otherwise .go-quality.yaml at the working directory is
// used when present, and compiled-in defaults fill the rest.
func loadConfig(configPath string) (config.Config, error) {
	if configPath != "" {
		return config.LoadRequired(configPath)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return config.Config{}, fmt.Errorf("getting working directory: %w", err)
	}
	return config.Load(filepath.Join(cwd, ".go-quality.yaml"))
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseFlags(args, stderr)
	if err != nil {
		if err == flag.ErrHelp {
			return exitOK
		}
		warn(stderr, "go-quality: %v\n", err)
		return exitFailure
	}
	if opts.showVersion {
		warn(stdout, "%s\n", version.String())
		return exitOK
	}

	projectRoot, err := root.Discover(opts.projectPath)
	if err != nil {
		warn(stderr, "go-quality: %v\n", err)
		return exitFailure
	}
	cfg, err := loadConfig(opts.configPath)
	if err != nil {
		warn(stderr, "go-quality: %v\n", err)
		return exitFailure
	}
	names, err := requestedTools(opts.tools)
	if err != nil {
		warn(stderr, "go-quality: %v\n", err)
		return exitFailure
	}

	binDir, err := discover.ResolveGoBinDir()
	if err != nil {
		warn(stderr, "go-quality: %v\n", err)
		return exitFailure
	}
	versionCache := discover.NewCache()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ResolveTimeout())
	defer cancel()

	if err := preflight.EnsureToolsAvailable(ctx, names, cfg, binDir, versionCache); err != nil {
		warn(stderr, "go-quality: %v\n", err)
		return exitFailure
	}
	if opts.installOnly {
		warn(stderr, "go-quality: pinned tools ready (%s)\n", strings.Join(names, ", "))
		return exitOK
	}

	handlers := preflight.BuildHandlers(names, cfg, projectRoot, binDir)
	diags := checkers.RunAllChecks(ctx, &runner.ExecRunner{Dir: projectRoot}, handlers, projectRoot, cfg.ResolveTimeout())

	var writeErr error
	if opts.jsonOut {
		payload, err := MarshalJSON(diags)
		if err != nil {
			warn(stderr, "go-quality: %v\n", err)
			return exitFailure
		}
		_, writeErr = fmt.Fprintf(stdout, "%s\n", payload)
	} else {
		_, writeErr = fmt.Fprint(stdout, renderText(diags))
	}
	if writeErr != nil {
		// stdout is the payload channel; losing it means the run produced
		// nothing usable, so this is a failure rather than a finding.
		warn(stderr, "go-quality: writing result: %v\n", writeErr)
		return exitFailure
	}

	failures := Failures(diags)
	findings := len(diags) - len(failures)
	warn(stderr, "go-quality: %d finding(s), %d tool failure(s)\n", findings, len(failures))

	code := ExitCode(diags, opts.failOn)
	if code == exitFailure {
		warn(stderr, "go-quality: the run did not complete; findings above may be partial\n")
	}
	return code
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
