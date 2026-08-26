package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sonquer/opendba/src/cli/internal/config"
)

func reporting(t *testing.T) crash {
	t.Helper()
	return crash{paths: config.Paths{State: t.TempDir()}, version: "1.2.3"}
}

// TestACrashIsWrittenDown is the whole point of the file: a full screen program
// that falls over has already cleared the screen it fell over on, so what it
// printed on the way out went with it.
func TestACrashIsWrittenDown(t *testing.T) {
	report := reporting(t)
	path := report.wrote("answering", "index out of range", []byte("a stack\nanother line"))
	if path == "" {
		t.Fatal("nothing was written")
	}
	read, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the account back: %v", err)
	}
	for _, want := range []string{"1.2.3", "answering", "index out of range", "a stack"} {
		if !strings.Contains(string(read), want) {
			t.Fatalf("the account does not say %q:\n%s", want, read)
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, err = %v; an account of a failure holds a conversation", info.Mode(), err)
	}
}

// TestACrashCarriesTheLibrarysLastWords is what makes a crash in the inference
// library diagnosable at all: it ends the process where it stands, and the last
// thing it wrote is the only evidence there is.
func TestACrashCarriesTheLibrarysLastWords(t *testing.T) {
	report := reporting(t)
	lines := make([]string, 0, engineLogTail+10)
	for i := range cap(lines) {
		lines = append(lines, "line "+string(rune('a'+i%26))+strings.Repeat("", i))
	}
	written := strings.Join(lines, "\n") + "\nggml assert failed: the last word\n"
	if err := os.WriteFile(report.paths.EngineLog(), []byte(written), 0o600); err != nil {
		t.Fatal(err)
	}
	account := report.account("answering", "died", nil)
	if !strings.Contains(account, "ggml assert failed: the last word") {
		t.Fatalf("the account does not carry what the library said:\n%s", account)
	}
	if strings.Count(account, "line ") > engineLogTail {
		t.Fatal("the whole log went in rather than the end of it")
	}
}

func TestNowhereToWriteIsNotAFailure(t *testing.T) {
	if got := (crash{}).wrote("answering", "died", nil); got != "" {
		t.Fatalf("wrote() = %q with nowhere to write it", got)
	}
	report := crash{paths: config.Paths{State: filepath.Join(t.TempDir(), "not there")}}
	if got := report.wrote("answering", "died", nil); got != "" {
		t.Fatalf("wrote() = %q", got)
	}
	if got := report.engineLog(); got != "" {
		t.Fatalf("engineLog() = %q", got)
	}
}

// TestGuardTurnsAPanicIntoSomethingOnTheScreen is the difference between a
// program that disappears and one that says what happened: everything the
// assistant does happens away from the screen.
func TestGuardTurnsAPanicIntoSomethingOnTheScreen(t *testing.T) {
	m := configured(t)
	m.width, m.height = 100, 32
	cmd := m.guard("answering", func() tea.Msg { panic("a nil map") })
	msg, ok := cmd().(crashedMsg)
	if !ok {
		t.Fatal("the panic went up rather than into a message")
	}
	if msg.cause != "a nil map" || msg.doing != "answering" {
		t.Fatalf("msg = %+v", msg)
	}
	if msg.where == "" {
		t.Fatal("nothing was written down")
	}
	if _, err := os.Stat(msg.where); err != nil {
		t.Fatalf("the account is not where it says: %v", err)
	}
	said, _ := m.crashed(msg)
	after := said.(Model)
	if !strings.Contains(after.talk.trouble, "a nil map") || !strings.Contains(after.talk.trouble, msg.where) {
		t.Fatalf("trouble = %q, want what happened and where to look", after.talk.trouble)
	}
	if after.talk.busy || after.talk.loading || after.ai.busy != "" {
		t.Fatal("the screen still thinks the work that died is running")
	}
}

func TestGuardLeavesWorkThatDoesNotPanicAlone(t *testing.T) {
	m := configured(t)
	msg := m.guard("answering", func() tea.Msg { return warmedMsg{memory: 42} })()
	if got, ok := msg.(warmedMsg); !ok || got.memory != 42 {
		t.Fatalf("msg = %#v", msg)
	}
}

func TestAPanicAwayFromTheScreenBecomesAnError(t *testing.T) {
	err := fell("answering", "a nil map", "/tmp/crash.log")
	for _, want := range []string{"answering", "a nil map", "/tmp/crash.log"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want it to say %q", err, want)
		}
	}
	if bare := fell("answering", "a nil map", ""); strings.Contains(bare.Error(), "written in") {
		t.Fatalf("err = %v, want no promise of a file that was never written", bare)
	}
}
