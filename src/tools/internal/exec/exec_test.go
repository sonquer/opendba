package exec

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResultHelpers(t *testing.T) {
	cases := []struct {
		name   string
		result Result
		output string
		lines  int
		ok     bool
	}{
		{"both streams", Result{Stdout: "out\n", Stderr: "err\n"}, "out\nerr", 2, true},
		{"stdout only", Result{Stdout: "out\n"}, "out", 1, true},
		{"stderr only", Result{Stderr: "err\n"}, "err", 1, true},
		{"empty", Result{}, "", 0, true},
		{"failed", Result{ExitCode: 2, Stdout: "boom"}, "boom", 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.result.Output(); got != c.output {
				t.Errorf("Output() = %q, want %q", got, c.output)
			}
			if got := len(c.result.Lines()); got != c.lines {
				t.Errorf("Lines() = %d, want %d", got, c.lines)
			}
			if got := c.result.OK(); got != c.ok {
				t.Errorf("OK() = %v, want %v", got, c.ok)
			}
		})
	}
}

func TestFormat(t *testing.T) {
	if got := Format("go"); got != "go" {
		t.Errorf("Format() = %q", got)
	}
	if got := Format("go", "test", "./..."); got != "go test ./..." {
		t.Errorf("Format() = %q", got)
	}
}

func TestOSRunSuccess(t *testing.T) {
	result, err := OS{}.Run(context.Background(), t.TempDir(), goBinary(), "version")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK() || !strings.Contains(result.Output(), "go version") {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Duration <= 0 {
		t.Error("duration must be measured")
	}
	if result.Command != goBinary()+" version" {
		t.Errorf("Command = %q", result.Command)
	}
}

func TestOSRunCapturesExitCode(t *testing.T) {
	result, err := OS{}.Run(context.Background(), t.TempDir(), goBinary(), "run", "./does-not-exist")
	if err != nil {
		t.Fatalf("Run must not fail on non-zero exit: %v", err)
	}
	if result.OK() {
		t.Fatal("want non-zero exit code")
	}
	if result.Output() == "" {
		t.Error("want captured diagnostics")
	}
}

func TestOSRunUsesProvidedEnvironment(t *testing.T) {
	result, err := OS{Env: []string{"GOCACHE=" + t.TempDir()}}.Run(context.Background(), t.TempDir(), goBinary(), "version")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK() {
		t.Fatalf("unexpected failure: %+v", result)
	}
}

func TestOSRunMissingBinary(t *testing.T) {
	if _, err := (OS{}).Run(context.Background(), t.TempDir(), "tui4db-not-a-real-binary"); err == nil {
		t.Fatal("want error for missing binary")
	}
}

func TestOSRunHonoursContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	result, err := OS{}.Run(ctx, t.TempDir(), goBinary(), "build", "std")
	if err == nil && result.OK() {
		t.Skip("command finished before the context expired")
	}
}

func goBinary() string {
	if runtime.GOOS == "windows" {
		return "go.exe"
	}
	return "go"
}
