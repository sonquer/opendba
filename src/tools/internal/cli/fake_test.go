package cli

import (
	"fmt"
	"os"
	"testing"

	"github.com/charmbracelet/x/term"
)

// TestMain lets the test binary stand in for the program the screens are walked
// through. It is told apart by the arguments the runner starts a program with,
// so nothing in the command itself has to know it is being tested.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "tui" {
		os.Exit(standIn())
	}
	os.Exit(m.Run())
}

func standIn() int {
	restore, err := term.MakeRaw(os.Stdin.Fd())
	if err == nil {
		defer func() { _ = term.Restore(os.Stdin.Fd(), restore) }()
	}
	fmt.Print("\x1b[2J\x1b[HREADY")
	letter := make([]byte, 1)
	for {
		read, err := os.Stdin.Read(letter)
		if err != nil || read == 0 {
			return 0
		}
		if letter[0] == 'q' || letter[0] == 0x03 {
			return 0
		}
	}
}
