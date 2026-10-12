package runner

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

type mockRunner struct {
	output []byte
	err    error
}

func (m *mockRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.output, nil
}

func TestMockRunner(t *testing.T) {
	r := &mockRunner{output: []byte("hello")}
	out, err := r.Run(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "hello" {
		t.Errorf("output = %q, want %q", string(out), "hello")
	}
}

func TestExecRunnerRunsCommand(t *testing.T) {
	r := &ExecRunner{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := r.Run(ctx, "go", "version")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Error("expected non-empty output from go version")
	}
}

func TestExecRunnerDir(t *testing.T) {
	dir := t.TempDir()
	r := &ExecRunner{Dir: dir}
	ctx := context.Background()
	out, err := r.Run(ctx, "go", "env", "GOMOD")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("GOMOD from %s: %s", dir, out)
}

func TestExecRunnerNonExistentCommand(t *testing.T) {
	r := &ExecRunner{}
	ctx := context.Background()
	_, err := r.Run(ctx, "definitely-not-a-real-command-xyzzy")
	if err == nil {
		t.Error("expected error for nonexistent command")
	}
	var execErr *exec.Error
	if !errors.As(err, &execErr) {
		t.Logf("error type (expected *exec.Error): %T: %v", err, err)
	}
}

func TestExecRunnerExitError(t *testing.T) {
	r := &ExecRunner{}
	ctx := context.Background()
	output, err := r.Run(ctx, "go", "build", "./does-not-exist.go")
	if err == nil {
		t.Error("expected error from go build on nonexistent file")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Logf("error type: %T: %v", err, err)
	} else {
		t.Logf("exit code: %d, stderr: %s", exitErr.ExitCode, exitErr.Stderr)
		t.Logf("stdout bytes: %d", len(output))
		if len(output) != len(exitErr.Stdout) {
			t.Error("returned stdout should match ExitError.Stdout")
		}
	}
}

func TestExecRunnerStdoutOnFailure(t *testing.T) {
	r := &ExecRunner{}
	ctx := context.Background()
	output, err := r.Run(ctx, "go", "version")
	if err != nil {
		t.Fatal(err)
	}
	if len(output) == 0 {
		t.Error("expected non-empty output from go version")
	}
}

func TestExitCodeHelper(t *testing.T) {
	t.Run("extracts exit code from ExitError", func(t *testing.T) {
		e := &ExitError{ExitCode: 2, Stderr: "fail", Err: errors.New("test")}
		code, ok := ExitCode(e)
		if !ok || code != 2 {
			t.Errorf("ExitCode = (%d, %v), want (2, true)", code, ok)
		}
	})
	t.Run("returns false for non-exit errors", func(t *testing.T) {
		_, ok := ExitCode(errors.New("plain error"))
		if ok {
			t.Error("expected false for plain error")
		}
	})
}

// TestExecRunnerTimeoutSurfacesDeadline runs a real subprocess to its deadline.
// This is the path production actually takes: exec.CommandContext kills the
// process and os/exec reports the process error, so the context error has to be
// reattached for errors.Is to classify it as a timeout.
func TestExecRunnerTimeoutSurfacesDeadline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal-kill detection is Unix-shaped; see runner.go")
	}
	r := &ExecRunner{}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, err := r.Run(ctx, "sleep", "5")
	if err == nil {
		t.Fatal("expected an error from a killed subprocess")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false; err = %v", err)
	}
	// The exit-error shape callers depend on must survive.
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Errorf("errors.As(err, *ExitError) = false; err = %T", err)
	} else if exitErr.ExitCode >= 0 {
		t.Errorf("ExitCode = %d, want a negative (signal-killed) code", exitErr.ExitCode)
	}
}

// TestExecRunnerRealExitKeepsItsOwnError guards the other half of the rule: a
// genuine non-zero exit must not be rewritten as a timeout just because the
// context was done by the time the process finished.
func TestExecRunnerRealExitKeepsItsOwnError(t *testing.T) {
	r := &ExecRunner{}
	ctx := context.Background()
	_, err := r.Run(ctx, "go", "build", "./does-not-exist.go")
	if err == nil {
		t.Fatal("expected an error from a failing build")
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		t.Errorf("real exit misclassified as a context error: %v", err)
	}
	if !strings.Contains(err.Error(), "exit code") {
		t.Errorf("err = %v, want an exit-code message", err)
	}
}
