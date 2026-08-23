package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/sonquer/tui4db/src/cli/internal/ai/providers/local/embedded"
)

// TestTheBuildIsTiedToTheBinding is the guarantee that this pair moves
// together. The binding declares llama.cpp's structures in Go and nothing
// checks that declaration at build time, so a binding that was bumped without
// anybody revisiting the build it was pinned against would be memory corruption
// inside an inference loop rather than an error.
func TestTheBuildIsTiedToTheBinding(t *testing.T) {
	read, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	var required string
	for _, line := range strings.Split(string(read), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "github.com/hybridgroup/yzma" {
			required = fields[1]
		}
	}
	if required == "" {
		t.Fatal("go.mod no longer requires the binding this package is written against")
	}
	if required != Binding {
		t.Fatalf("go.mod is on yzma %s and the library carried here is llama.cpp %s, which was chosen for yzma %s.\n"+
			"Check the compatibility table in the binding's README, vendor the matching build, run the smoke test, and then move Binding.",
			required, Build, Binding)
	}
}

func carried(t *testing.T) map[string][]byte {
	t.Helper()
	if !embedded.Present() {
		t.Skipf("this program carries no build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	held, err := embedded.Files()
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	return held
}

func names(held map[string][]byte) []string {
	out := make([]string, 0, len(held))
	for name := range held {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestInstallUsesWhatThisProgramCarries is the whole point of embedding: the
// library arrives without anything being fetched, so a machine with no network
// and a release replaced upstream are both none of our business.
func TestInstallUsesWhatThisProgramCarries(t *testing.T) {
	held := carried(t)
	library := NewLibrary(t.TempDir())
	if library.Present() {
		t.Fatal("Present() says a library is there before anything was written")
	}

	out := make(chan Progress, 64)
	done := make(chan []Progress, 1)
	watch(out, done)
	err := library.Install(context.Background(), out)
	close(out)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	progress := <-done
	if len(progress) == 0 || !progress[len(progress)-1].Done {
		t.Fatalf("progress = %+v, want it to end with done", progress)
	}
	if !library.Present() {
		t.Fatalf("nothing was written, missing %v", library.Missing())
	}
	for name, bytes := range held {
		written, err := os.ReadFile(filepath.Join(library.Dir(), name))
		if err != nil {
			t.Fatalf("%s was not written: %v", name, err)
		}
		if len(written) != len(bytes) {
			t.Fatalf("%s is %d bytes on disk and %d in the program", name, len(written), len(bytes))
		}
	}
}

func TestInstallIsSilentAboutNothingToWrite(t *testing.T) {
	if embedded.Present() {
		t.Skip("this program carries a build, so there is nothing to refuse")
	}
	library := NewLibrary(t.TempDir())
	if err := library.Install(context.Background(), nil); !errors.Is(err, ErrNoAsset) {
		t.Fatalf("Install() error = %v, want it to say nothing is carried", err)
	}
	if Carried() {
		t.Fatal("Carried() says otherwise")
	}
}

func TestWhatIsCarriedHoldsAComputeBackend(t *testing.T) {
	held := carried(t)
	var backend bool
	for name := range held {
		if strings.Contains(name, "ggml-cpu") {
			backend = true
		}
	}
	if !backend {
		t.Fatalf("nothing here computes anything: %v", names(held))
	}
	for _, want := range []string{"llama", "ggml-base"} {
		if _, ok := held[libraryFile(want)]; !ok {
			t.Fatalf("%s is not carried: %v", libraryFile(want), names(held))
		}
	}
	if !Carried() {
		t.Fatal("Carried() says this program has nothing")
	}
}

// TestMissingNamesWhatIsNotThere covers both names a library goes by. A
// directory holding the plain names and not the versioned ones looks complete
// and opens to "no such file" on the first neighbour, so it is missing.
func TestMissingNamesWhatIsNotThere(t *testing.T) {
	library := NewLibrary(t.TempDir())
	absent := library.Missing()
	for _, name := range absent {
		if !strings.Contains(name, "llama") && !strings.Contains(name, "ggml-base") {
			t.Fatalf("Missing() names %q, which is not one of the two", name)
		}
	}
	names := 2
	if alias(libraryFile("llama")) != "" {
		names = 4
	}
	if len(absent) != names {
		t.Fatalf("Missing() = %v, want every name the two go by", absent)
	}
}

// TestAPlainNameOnItsOwnIsNotAnInstalledLibrary is the state this program left
// behind before it wrote the versioned names: every file present, nothing that
// opens.
func TestAPlainNameOnItsOwnIsNotAnInstalledLibrary(t *testing.T) {
	if alias(libraryFile("llama")) == "" {
		t.Skip("libraries go by one name on this system")
	}
	dir := t.TempDir()
	for _, name := range []string{"llama", "ggml-base"} {
		if err := os.WriteFile(filepath.Join(dir, libraryFile(name)), []byte("a library"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	library := NewLibrary(dir)
	if library.Present() {
		t.Fatalf("a directory with no versioned names passes for an installed library: %v", library.Missing())
	}
}

// TestTheVersionedNamesAreWritten is the fix itself: what a library declares
// itself as, and links against its neighbours by, has to be in the directory
// beside them.
func TestTheVersionedNamesAreWritten(t *testing.T) {
	held := carried(t)
	library := NewLibrary(t.TempDir())
	if err := library.Install(context.Background(), nil); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	for name := range held {
		second := alias(name)
		if second == "" {
			continue
		}
		read, err := os.ReadFile(filepath.Join(library.Dir(), second))
		if err != nil {
			t.Fatalf("%s links against %s and it is not there: %v", name, second, err)
		}
		if len(read) != len(held[name]) {
			t.Fatalf("%s is %d bytes and %s is %d", name, len(held[name]), second, len(read))
		}
	}
	if strings.Contains(alias("libggml-cpu-haswell.so"), "haswell") {
		t.Fatal("a processor variant was given a second name, which is a second candidate for one backend")
	}
}

func TestInstallSurvivesASecondName(t *testing.T) {
	carried(t)
	library := NewLibrary(t.TempDir())
	if err := library.Install(context.Background(), nil); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if err := library.Install(context.Background(), nil); err != nil {
		t.Fatalf("installing over an installed library: %v", err)
	}
	if !library.Present() {
		t.Fatalf("the second install left %v missing", library.Missing())
	}
}

func TestInstallRefusesADirectoryItCannotMake(t *testing.T) {
	if !embedded.Present() {
		t.Skipf("this program carries no build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	root := t.TempDir()
	blocked := filepath.Join(root, "lib")
	if err := os.WriteFile(blocked, []byte("a file, not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewLibrary(blocked).Install(context.Background(), nil); err == nil {
		t.Fatal("Install() wrote into something that is not a directory")
	}
}

func TestInstallStopsWhenNobodyIsReading(t *testing.T) {
	carried(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewLibrary(t.TempDir()).Install(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Install() error = %v, want context.Canceled", err)
	}
}

func TestLibraryFileFollowsTheMachine(t *testing.T) {
	got := libraryFile("llama")
	want := map[string]string{
		"darwin":  "libllama.dylib",
		"windows": "llama.dll",
	}[runtime.GOOS]
	if want == "" {
		want = "libllama.so"
	}
	if got != want {
		t.Fatalf("libraryFile() = %q, want %q on %s", got, want, runtime.GOOS)
	}
}

func TestASecondNameThatCannotBeWritten(t *testing.T) {
	if alias(libraryFile("llama")) == "" {
		t.Skip("libraries go by one name on this system")
	}
	carried(t)
	dir := t.TempDir()
	blocked := filepath.Join(dir, alias(libraryFile("llama")))
	if err := os.MkdirAll(filepath.Join(blocked, "in the way"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := NewLibrary(dir).Install(context.Background(), nil); err == nil {
		t.Fatal("Install() reported a library whose second name it could not write")
	}
}
