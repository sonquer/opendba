package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sonquer/opendba/src/tools/internal/exec"
)

type gitRunner struct {
	sha  string
	exit int
}

func (g gitRunner) Run(_ context.Context, _ string, name string, args ...string) (exec.Result, error) {
	return exec.Result{Command: exec.Format(name, args...), Stdout: g.sha, ExitCode: g.exit}, nil
}

func versionHarness(t *testing.T, runner exec.Runner, current string) *harness {
	t.Helper()
	h := newHarness(t, runner)
	writeFile(t, filepath.Join(h.root, "VERSION"), current)
	h.app.Now = func() time.Time { return time.Date(2026, 8, 24, 3, 41, 0, 0, time.UTC) }
	t.Setenv("GITHUB_SHA", "")
	return h
}

func TestVersionNames(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "bare", args: []string{"version"}, want: "0.1.0"},
		{name: "tag", args: []string{"version", "--tag"}, want: "v0.1.0"},
		{name: "module tag", args: []string{"version", "--module-tag"}, want: "src/cli/v0.1.0"},
		{name: "module tag wins", args: []string{"version", "--tag", "--module-tag"}, want: "src/cli/v0.1.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := versionHarness(t, fakeRunner{}, "0.1.0\n")
			if code := h.app.Run(context.Background(), c.args); code != ExitOK {
				t.Fatalf("exit = %d\n%s", code, h.err())
			}
			if strings.TrimSpace(h.out()) != c.want {
				t.Errorf("output = %q, want %q", strings.TrimSpace(h.out()), c.want)
			}
		})
	}
}

func TestVersionSnapshotFollowsTheReleaseItWasCutFrom(t *testing.T) {
	h := versionHarness(t, fakeRunner{}, "0.1.0\n")
	code := h.app.Run(context.Background(), []string{"version", "--snapshot", "--commit", "aea1fe3abcdef"})
	if code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	if got, want := strings.TrimSpace(h.out()), "0.1.1-nightly.20260824.aea1fe3"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestVersionSnapshotExactKeepsThePatch(t *testing.T) {
	h := versionHarness(t, fakeRunner{}, "0.1.0\n")
	code := h.app.Run(context.Background(), []string{"version", "--snapshot", "--exact", "--commit", "aea1fe3"})
	if code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	if got, want := strings.TrimSpace(h.out()), "0.1.0-nightly.20260824.aea1fe3"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestVersionSnapshotTakesTheCommitFromTheEnvironment(t *testing.T) {
	h := versionHarness(t, fakeRunner{}, "0.1.0\n")
	t.Setenv("GITHUB_SHA", "0123456789abcdef0123456789abcdef01234567")
	if code := h.app.Run(context.Background(), []string{"version", "--snapshot"}); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	if got, want := strings.TrimSpace(h.out()), "0.1.1-nightly.20260824.0123456"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestVersionSnapshotAsksGitWhenNothingElseKnows(t *testing.T) {
	h := versionHarness(t, gitRunner{sha: "aea1fe3\n"}, "0.1.0\n")
	if code := h.app.Run(context.Background(), []string{"version", "--snapshot"}); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	if got, want := strings.TrimSpace(h.out()), "0.1.1-nightly.20260824.aea1fe3"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestVersionSnapshotFailsWhenGitSaysNothing(t *testing.T) {
	for _, runner := range []exec.Runner{gitRunner{}, gitRunner{sha: "aea1fe3", exit: 1}} {
		h := versionHarness(t, runner, "0.1.0\n")
		if code := h.app.Run(context.Background(), []string{"version", "--snapshot"}); code != ExitFailure {
			t.Fatalf("exit = %d, want a failure", code)
		}
		if !strings.Contains(h.err(), "read the current commit") {
			t.Errorf("stderr = %q", h.err())
		}
	}
}

func TestVersionSnapshotPropagatesRunnerErrors(t *testing.T) {
	h := versionHarness(t, fakeRunner{missing: map[string]bool{"git": true}}, "0.1.0\n")
	if code := h.app.Run(context.Background(), []string{"version", "--snapshot"}); code != ExitFailure {
		t.Fatalf("exit = %d, want a failure", code)
	}
}

func TestVersionCheckGuardsTheTag(t *testing.T) {
	h := versionHarness(t, fakeRunner{}, "0.1.0\n")
	if code := h.app.Run(context.Background(), []string{"version", "--check", "v0.1.0"}); code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	if !strings.Contains(h.out(), "v0.1.0 matches VERSION") {
		t.Errorf("output = %q", h.out())
	}

	wrong := versionHarness(t, fakeRunner{}, "0.1.0\n")
	if code := wrong.app.Run(context.Background(), []string{"version", "--check", "v0.2.0"}); code != ExitFailure {
		t.Fatalf("a mismatched tag must fail, got %d", code)
	}
	if !strings.Contains(wrong.err(), "does not match VERSION") {
		t.Errorf("stderr = %q", wrong.err())
	}
}

func TestVersionSetRewritesTheFile(t *testing.T) {
	cases := []struct {
		name string
		from string
		bump string
		want string
	}{
		{name: "patch", from: "0.1.0\n", bump: "patch", want: "0.1.1"},
		{name: "minor", from: "0.1.4\n", bump: "minor", want: "0.2.0"},
		{name: "major", from: "0.1.4\n", bump: "major", want: "1.0.0"},
		{name: "exact", from: "0.1.0\n", bump: "0.4.2", want: "0.4.2"},
		{name: "exact prerelease", from: "0.1.0\n", bump: "0.0.1-rc.1", want: "0.0.1-rc.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := versionHarness(t, fakeRunner{}, c.from)
			if code := h.app.Run(context.Background(), []string{"version", "--set", c.bump}); code != ExitOK {
				t.Fatalf("exit = %d\n%s", code, h.err())
			}
			if got := strings.TrimSpace(h.out()); got != c.want {
				t.Errorf("output = %q, want %q", got, c.want)
			}
			data, err := os.ReadFile(filepath.Join(h.root, "VERSION"))
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(data)); got != c.want {
				t.Errorf("VERSION = %q, want %q", got, c.want)
			}
		})
	}
}

func TestVersionSetRejectsNonsense(t *testing.T) {
	h := versionHarness(t, fakeRunner{}, "0.1.0\n")
	if code := h.app.Run(context.Background(), []string{"version", "--set", "bigger"}); code != ExitFailure {
		t.Fatalf("exit = %d, want a failure", code)
	}
	data, err := os.ReadFile(filepath.Join(h.root, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "0.1.0" {
		t.Errorf("a rejected bump rewrote VERSION to %q", string(data))
	}
}

func TestVersionSetCannotWriteAReadOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permissions are not enforced on windows")
	}
	h := versionHarness(t, fakeRunner{}, "0.1.0\n")
	path := filepath.Join(h.root, "VERSION")
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if code := h.app.Run(context.Background(), []string{"version", "--set", "patch"}); code != ExitFailure {
		t.Fatalf("exit = %d, want a failure", code)
	}
}

func TestVersionWritesTheOutputsCIReads(t *testing.T) {
	h := versionHarness(t, fakeRunner{}, "0.1.0\n")
	outputs := filepath.Join(t.TempDir(), "outputs")
	code := h.app.Run(context.Background(), []string{"version", "--set", "minor", "--github", outputs})
	if code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, h.err())
	}
	data, err := os.ReadFile(outputs)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"version=0.2.0",
		"tag=v0.2.0",
		"module_tag=src/cli/v0.2.0",
		"branch=release/v0.2.0",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("outputs missing %q:\n%s", want, data)
		}
	}
}

func TestVersionReportsAnUnwritableOutputsFile(t *testing.T) {
	h := versionHarness(t, fakeRunner{}, "0.1.0\n")
	code := h.app.Run(context.Background(), []string{"version", "--github", filepath.Join(t.TempDir(), "missing", "outputs")})
	if code != ExitFailure {
		t.Fatalf("exit = %d, want a failure", code)
	}
}

func TestVersionRejectsUnknownFlags(t *testing.T) {
	h := versionHarness(t, fakeRunner{}, "0.1.0\n")
	if code := h.app.Run(context.Background(), []string{"version", "--nope"}); code != ExitUsage {
		t.Fatalf("exit = %d, want usage", code)
	}
}

func TestVersionHelpIsNotAFailure(t *testing.T) {
	h := versionHarness(t, fakeRunner{}, "0.1.0\n")
	if code := h.app.Run(context.Background(), []string{"version", "-h"}); code != ExitOK {
		t.Fatalf("exit = %d, want a clean exit", code)
	}
}

func TestVersionRejectsAFileItCannotParse(t *testing.T) {
	h := versionHarness(t, fakeRunner{}, "not a version\n")
	if code := h.app.Run(context.Background(), []string{"version"}); code != ExitFailure {
		t.Fatalf("exit = %d, want a failure", code)
	}
	if !strings.Contains(h.err(), "VERSION") {
		t.Errorf("stderr = %q", h.err())
	}
}
