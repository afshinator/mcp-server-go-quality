package checkers

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/afshinator/mcp-server-go-quality/internal/diagnostic"
	"github.com/afshinator/mcp-server-go-quality/internal/runner"
)

func RunAllChecks(
	parentCtx context.Context,
	r runner.CommandRunner,
	handlers []Checker,
	projectPath string,
	timeout time.Duration,
) []diagnostic.Diagnostic {
	if len(handlers) == 0 {
		return nil
	}

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	results := make(chan runResult, len(handlers))
	var wg sync.WaitGroup

	for _, h := range handlers {
		wg.Add(1)
		go func(checker Checker) {
			defer wg.Done()

			// Capture name before any potentially-panicking call; recovery block
			// uses this variable so it never calls a method on a nil interface.
			toolName := "unknown"
			defer func() {
				if rec := recover(); rec != nil {
					results <- runResult{
						diagnostics: []diagnostic.Diagnostic{{
							Tool:  toolName,
							Error: fmt.Sprintf("internal panic: %v", rec),
						}},
					}
				}
			}()

			toolName = checker.Name() // panics here if checker is nil — recovery uses "unknown"
			toolCtx, toolCancel := context.WithTimeout(ctx, timeout)
			defer toolCancel()

			diags, err := checker.Run(toolCtx, r, projectPath)
			if err != nil {
				results <- runResult{
					diagnostics: []diagnostic.Diagnostic{{
						Tool:  toolName,
						Error: FormatCheckerError(toolCtx, timeout, err),
					}},
				}
				return
			}
			results <- runResult{diagnostics: diags}
		}(h)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var allDiags []diagnostic.Diagnostic
	for res := range results {
		allDiags = append(allDiags, res.diagnostics...)
	}

	// A total order, not just file and line. Diagnostics that share both are
	// common -- every module-level govulncheck finding has file "" and line 0 --
	// and sort.Slice is not stable, so with only two keys those groups were
	// ordered by whatever the tools happened to emit, making the output
	// irreproducible run to run. The remaining keys make the order total, so the
	// same findings always come out in the same sequence.
	slices.SortStableFunc(allDiags, func(a, b diagnostic.Diagnostic) int {
		return cmp.Or(
			cmp.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line),
			cmp.Compare(a.Column, b.Column),
			cmp.Compare(a.Tool, b.Tool),
			cmp.Compare(a.Severity, b.Severity),
			cmp.Compare(a.Message, b.Message),
			cmp.Compare(a.Error, b.Error),
			bytes.Compare(a.Native, b.Native),
		)
	})

	return allDiags
}

// FormatCheckerError renders a checker error for the caller. A context-expired
// termination states the timeout or cancellation AND the process exit code and
// stderr, discarding neither; it deliberately makes no causal claim, since no
// timing or signal heuristic can prove the context caused the exit.
func FormatCheckerError(ctx context.Context, timeout time.Duration, err error) string {
	message := ""
	if errors.Is(err, context.DeadlineExceeded) {
		message = fmt.Sprintf("timed out after %s", timeout)
	} else if errors.Is(err, context.Canceled) {
		message = "cancelled"
	} else {
		return err.Error()
	}

	if exitCode, ok := runner.ExitCode(err); ok {
		stderr, _ := runner.Stderr(err)
		if stderr != "" {
			return fmt.Sprintf("%s (exit code %d; stderr: %s)", message, exitCode, strings.TrimSpace(stderr))
		}
		return fmt.Sprintf("%s (exit code %d)", message, exitCode)
	}
	return message
}
