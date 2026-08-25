package tuitest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/x/term"
)

const (
	fakeVariable  = "TUITEST_FAKE"
	crashVariable = "TUITEST_FAKE_CRASH"
)

// TestMain lets the test binary stand in for the program under test, so that a
// session can be driven without anything being installed or built first.
func TestMain(m *testing.M) {
	if os.Getenv(fakeVariable) == "" {
		os.Exit(m.Run())
	}
	os.Exit(fake())
}

func fake() int {
	restore, err := term.MakeRaw(os.Stdin.Fd())
	if err == nil {
		defer func() { _ = term.Restore(os.Stdin.Fd(), restore) }()
	}
	if state := os.Getenv(crashVariable); state != "" {
		_ = os.MkdirAll(state, 0o755)
		_ = os.WriteFile(filepath.Join(state, "crash-2026.log"), []byte("it fell over"), 0o600)
	}
	fmt.Print("\x1b[2J\x1b[HREADY")
	letter := make([]byte, 1)
	for {
		read, err := os.Stdin.Read(letter)
		if err != nil || read == 0 {
			return 0
		}
		switch letter[0] {
		case 'q', 0x03:
			return 0
		case 'x':
			return 3
		case 'a':
			fmt.Print("\x1b[2J\x1b[HALPHA")
		case 'b':
			fmt.Print("\x1b[2J\x1b[HBETA")
		case 'z':
			fmt.Print("\x1b[2J\x1b[HREADY")
		}
	}
}

func fakeOptions(t *testing.T, width, height int) Options {
	t.Helper()
	return Options{
		Binary: os.Args[0],
		Env:    []string{fakeVariable + "=1", "TERM=xterm-256color", "PATH=" + os.Getenv("PATH")},
		Width:  width,
		Height: height,
		Quiet:  40 * time.Millisecond,
	}
}
