package local

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// measured reports whether this system can say how much room is left. It asks
// the implementation rather than naming the systems that have one: a list of
// names here goes stale the moment another system learns to answer, and a test
// that pins which systems cannot answer fails the day one of them can.
func measured() bool { return free(os.TempDir()) >= 0 }

func TestRoom(t *testing.T) {
	room := Room(t.TempDir())
	if !measured() {
		if room.FreeDisk != -1 {
			t.Fatalf("FreeDisk = %d, want it reported as unknown on %s", room.FreeDisk, runtime.GOOS)
		}
		return
	}
	if room.FreeDisk <= 0 {
		t.Fatalf("FreeDisk = %d, want the room on a directory that is there", room.FreeDisk)
	}
	if room.Memory != 0 {
		t.Fatalf("Memory = %d, want it left to the engine, which is the only thing that can say", room.Memory)
	}
}

func TestRoomFallsBackToWhereTheDirectoryWouldGo(t *testing.T) {
	inside := filepath.Join(t.TempDir(), "models", "not", "made", "yet")
	if room := Room(inside); measured() && room.FreeDisk <= 0 {
		t.Fatalf("FreeDisk = %d, want the room where it would be made", room.FreeDisk)
	}
}

func TestRoomOnSomewhereThatIsNotThereAtAll(t *testing.T) {
	if room := Room("/nowhere-at-all/models"); room.FreeDisk == 0 {
		t.Fatal("FreeDisk = 0, want either a measurement or an admission that there is none")
	}
}

func TestParent(t *testing.T) {
	cases := map[string]string{
		"/a/b/c":          "/a/b",
		`c:\models\gemma`: `c:\models`,
		"models":          ".",
		"":                ".",
	}
	for given, want := range cases {
		t.Run(given, func(t *testing.T) {
			if got := parent(given); got != want {
				t.Fatalf("parent(%q) = %q, want %q", given, got, want)
			}
		})
	}
}
