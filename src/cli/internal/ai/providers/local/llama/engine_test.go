package llama

import (
	"context"
	"os"
	"runtime"
	"testing"

	"github.com/sonquer/opendba/src/cli/internal/ai/providers/local"
)

// TestTheCarriedLibraryOpens is the test the rest of the arrangement rests on.
//
// Everything else about the embedded library can be checked by reading a
// directory listing: the files are there, they are the right length, the plain
// names are all present. None of that is the question. The question is whether
// the loader opens them, and the answer turns on names that never appear in a
// listing at all — the versioned name each library declares itself as and links
// against its neighbours by. A set that is complete by every other measure and
// missing one of those opens and fails on the first neighbour, which is how
// this went wrong once already.
//
// So this writes what the program carries to a directory and opens it, on
// whatever machine is running the tests.
func TestTheCarriedLibraryOpens(t *testing.T) {
	if !local.Carried() {
		t.Skipf("this program carries no inference library for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	dir := libraryRoot(t)
	library := local.NewLibrary(dir)
	if err := library.Install(context.Background(), nil); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if err := New(dir).Ready(); err != nil {
		t.Fatalf("the library this program carries does not open: %v", err)
	}
}

// TestTheOpenLibraryKnowsTheMachine is the other half of it: the fit arithmetic
// asks the library how much memory there is to run a model in, and a library
// that reports no hardware would make every model look as though it fitted.
func TestTheOpenLibraryKnowsTheMachine(t *testing.T) {
	if !local.Carried() {
		t.Skipf("this program carries no inference library for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	dir := libraryRoot(t)
	if err := local.NewLibrary(dir).Install(context.Background(), nil); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	engine := New(dir)
	if err := engine.Ready(); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	devices := engine.Devices()
	if len(devices) == 0 {
		t.Fatal("the library opened and found nothing to compute on")
	}
	var largest int64
	for _, device := range devices {
		if device.Name == "" {
			t.Errorf("a device with no name: %+v", device)
		}
		if device.TotalBytes > largest {
			largest = device.TotalBytes
		}
	}
	if largest <= 0 {
		t.Fatalf("no device would say how much memory it has: %+v", devices)
	}
}

// libraryRoot is where a test unpacks the inference library. On Windows it is
// not t.TempDir(): the loader keeps a handle on a library it has opened, this
// program never unloads one because a process that has cannot open it again,
// and Windows will not unlink a file that is still open. Removing it is best
// effort, so a run leaves a directory behind rather than failing on the way out.
func libraryRoot(t *testing.T) string {
	if runtime.GOOS != "windows" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp("", "opendba-library-")
	if err != nil {
		t.Fatalf("make a directory for the library: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
