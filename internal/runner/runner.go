package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type ExecRunner struct {
	Dir string
}

type ExitError struct {
	ExitCode int
	Stderr   string
	Stdout   []byte
	Err      error
}

func (e *ExitError) Error() string {
	stderr := strings.TrimSpace(e.Stderr)
	message := fmt.Sprintf("Tool command failed with exit code %d.", e.ExitCode)
	if stderr != "" {
		message = fmt.Sprintf("Tool command failed with exit code %d. Stderr: %s", e.ExitCode, stderr)
	}
	if errors.Is(e.Err, context.DeadlineExceeded) {
		message += " (context deadline exceeded)"
	} else if errors.Is(e.Err, context.Canceled) {
		message += " (context canceled)"
	}
	return message
}

func (e *ExitError) Unwrap() error {
	return e.Err
}

func ExitCode(err error) (int, bool) {
	var e *ExitError
	if errors.As(err, &e) {
		return e.ExitCode, true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}

func Stderr(err error) (string, bool) {
	var e *ExitError
	if errors.As(err, &e) {
		return e.Stderr, true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(exitErr.Stderr), true
	}
	return "", false
}

func (r *ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204,G702 — ExecRunner is the controlled subprocess interface, name comes from handler constants
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			cause := err
			// os/exec prefers the process error over the context error. Keep both
			// facts when the context is done; this does not establish causation.
			if ctx.Err() != nil {
				cause = errors.Join(err, ctx.Err())
			}
			return stdout.Bytes(), &ExitError{
				ExitCode: exitErr.ExitCode(),
				Stderr:   stderr.String(),
				Stdout:   stdout.Bytes(),
				Err:      cause,
			}
		}
		return stdout.Bytes(), err
	}

	return stdout.Bytes(), nil
}
