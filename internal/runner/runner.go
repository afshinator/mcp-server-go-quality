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
	if stderr == "" {
		return fmt.Sprintf("Tool command failed with exit code %d.", e.ExitCode)
	}
	return fmt.Sprintf("Tool command failed with exit code %d. Stderr: %s", e.ExitCode, stderr)
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
	cmd := exec.CommandContext(ctx, name, args...)
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
			return stdout.Bytes(), &ExitError{
				ExitCode: exitErr.ExitCode(),
				Stderr:   stderr.String(),
				Stdout:   stdout.Bytes(),
				Err:      err,
			}
		}
		return stdout.Bytes(), err
	}

	return stdout.Bytes(), nil
}
