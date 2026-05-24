package checkers

import (
	"context"
	"fmt"
	"sort"
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
			defer func() {
				if rec := recover(); rec != nil {
					results <- runResult{
						diagnostics: []diagnostic.Diagnostic{{
							Tool:  checker.Name(),
							Error: fmt.Sprintf("internal panic: %v", rec),
						}},
					}
				}
			}()

			toolCtx, toolCancel := context.WithTimeout(ctx, timeout)
			defer toolCancel()

			diags, err := checker.Run(toolCtx, r, projectPath)
			if err != nil {
				results <- runResult{
					diagnostics: []diagnostic.Diagnostic{{
						Tool:  checker.Name(),
						Error: formatCheckerError(toolCtx, checker.Name(), timeout, err),
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

	sort.Slice(allDiags, func(i, j int) bool {
		if allDiags[i].File != allDiags[j].File {
			return allDiags[i].File < allDiags[j].File
		}
		return allDiags[i].Line < allDiags[j].Line
	})

	return allDiags
}

func formatCheckerError(ctx context.Context, toolName string, timeout time.Duration, err error) string {
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("timed out after %s", timeout)
	}
	if ctx.Err() == context.Canceled {
		return "cancelled"
	}
	return err.Error()
}
