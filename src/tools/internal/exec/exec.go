package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Result struct {
	Command  string
	Dir      string
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
}

func (r Result) OK() bool { return r.ExitCode == 0 }

func (r Result) Output() string {
	out := strings.TrimRight(r.Stdout, "\n")
	errOut := strings.TrimRight(r.Stderr, "\n")
	switch {
	case out == "":
		return errOut
	case errOut == "":
		return out
	default:
		return out + "\n" + errOut
	}
}

func (r Result) Lines() []string {
	output := r.Output()
	if output == "" {
		return nil
	}
	return strings.Split(output, "\n")
}

type Runner interface {
	Run(ctx context.Context, dir string, name string, args ...string) (Result, error)
}

type OS struct {
	Env []string
}

func (o OS) Run(ctx context.Context, dir string, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if len(o.Env) > 0 {
		cmd.Env = o.Env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	started := time.Now()
	err := cmd.Run()
	result := Result{
		Command:  Format(name, args...),
		Dir:      dir,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(started),
	}

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return result, nil
	case errors.As(err, &exitErr):
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	default:
		return result, fmt.Errorf("run %s: %w", result.Command, err)
	}
}

func Format(name string, args ...string) string {
	if len(args) == 0 {
		return name
	}
	return name + " " + strings.Join(args, " ")
}

func Passthrough(ctx context.Context, dir string, environment []string, name string, args ...string) (int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = environment
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), nil
	default:
		return 0, fmt.Errorf("run %s: %w", Format(name, args...), err)
	}
}
