package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sonquer/tui4db/src/cli/internal/ai/providers/local/embedded"
)

// Build is the llama.cpp release this program is written against. It is a
// number rather than "latest" on purpose: the binding declares the shape of
// llama.cpp's structures in Go and nothing checks that declaration at build
// time, so a library that moved underneath it is memory corruption inside an
// inference loop rather than an error anybody sees. Moving this is a deliberate
// step, taken with the smoke test that generates tokens against it.
const Build = "b10587"

// Binding is the version of the Go binding that Build was chosen for. The two
// have to move together: the binding declares llama.cpp's structures in Go and
// the compatibility window is a table in its README, so a version of one that
// has never been seen beside the other is a guess. A test reads go.mod and
// fails when they drift apart, which is what makes this a guarantee rather than
// a note.
const Binding = "v1.24.0"

// ErrNoAsset is what a machine this program carries no build for reports.
var ErrNoAsset = errors.New("this program carries no inference library for this machine")

// Library is the inference library on this machine.
type Library struct{ dir string }

// NewLibrary reads and writes the library under a directory.
func NewLibrary(dir string) *Library { return &Library{dir: dir} }

// Dir is where the library is kept, which is what the engine is opened with.
func (l *Library) Dir() string { return l.dir }

// Present reports whether enough of the library is here to load a model. Only
// the two that every backend needs are required: a machine without Metal has no
// libggml-metal and is not broken.
func (l *Library) Present() bool { return len(l.Missing()) == 0 }

// Missing lists the libraries that have to be there and are not.
//
// The versioned name counts as much as the plain one. A library that is there
// under its plain name and not under the name its neighbours link against opens
// and then fails to find them, which is a library that is not installed however
// it looks in a directory listing.
func (l *Library) Missing() []string {
	absent := []string{}
	for _, name := range []string{"llama", "ggml-base"} {
		for _, wanted := range []string{libraryFile(name), alias(libraryFile(name))} {
			if wanted == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(l.dir, wanted)); err != nil {
				absent = append(absent, wanted)
			}
		}
	}
	return absent
}

// alias is the second name a library has to answer to, or nothing where a
// system does not use one.
//
// These builds are made with a shared object version, so every library on unix
// declares itself as libfoo.0.dylib or libfoo.so.0 and links against its
// neighbours by that name. Writing only the plain names produces a directory
// that looks complete and opens to "no such file" on the first neighbour.
//
// The processor variants of the compute backend are left alone: ggml finds
// those by looking for their plain names, and a second name in the directory is
// a second candidate for the same backend.
func alias(name string) string {
	if strings.Contains(name, "ggml-cpu-") {
		return ""
	}
	switch runtime.GOOS {
	case "windows":
		return ""
	case "darwin":
		if !strings.HasSuffix(name, ".dylib") {
			return ""
		}
		return strings.TrimSuffix(name, ".dylib") + "." + soVersion + ".dylib"
	default:
		if !strings.HasSuffix(name, ".so") {
			return ""
		}
		return name + "." + soVersion
	}
}

// soVersion is the shared object version these builds carry. It is part of
// every library name on unix, and moving it is part of moving Build.
const soVersion = "0"

// libraryFile is what a library is called on this operating system, in the
// unversioned form the loader looks for.
func libraryFile(name string) string {
	switch runtime.GOOS {
	case "windows":
		return name + ".dll"
	case "darwin":
		return "lib" + name + ".dylib"
	default:
		return "lib" + name + ".so"
	}
}

// Install puts the inference library where it can be opened.
//
// The bytes come out of this program: purego opens a library by path and cannot
// open one out of memory, so they are written once and then loaded from disk.
// What they are not is fetched, which is the difference that matters — a
// release asset can be replaced under the tag it was published on, and these
// bytes are the ones this program was built and tested against.
//
// A machine this program carries nothing for has no local inference, and says
// so. There is no fetching to fall back to: a library pulled off the network at
// run time is the thing embedding was chosen to avoid.
func (l *Library) Install(ctx context.Context, out chan<- Progress) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	held, err := embedded.Files()
	if err != nil {
		return err
	}
	if len(held) == 0 {
		return fmt.Errorf("%w: this program carries none", ErrNoAsset)
	}
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", l.dir, err)
	}
	written := int64(0)
	total := int64(0)
	for _, bytes := range held {
		total += int64(len(bytes))
	}
	for name, bytes := range held {
		if err := l.write(name, strings.NewReader(string(bytes))); err != nil {
			return err
		}
		if err := l.also(name); err != nil {
			return err
		}
		written += int64(len(bytes))
		if err := report(ctx, out, Progress{ID: "the inference library", Bytes: written, Total: total}); err != nil {
			return err
		}
	}
	if absent := l.Missing(); len(absent) > 0 {
		return fmt.Errorf("this program carries no %s", strings.Join(absent, " or "))
	}
	return report(ctx, out, Progress{ID: "the inference library", Bytes: total, Total: total, Done: true})
}

// also gives a library the second name its neighbours link against. It is a
// link where a system has them and a copy where it does not, because what
// matters is that the name resolves, not how.
func (l *Library) also(name string) error {
	second := alias(name)
	if second == "" {
		return nil
	}
	path := filepath.Join(l.dir, second)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace %s: %w", second, err)
	}
	if err := os.Symlink(name, path); err == nil {
		return nil
	}
	read, err := os.ReadFile(filepath.Join(l.dir, name))
	if err != nil {
		return fmt.Errorf("read %s back: %w", name, err)
	}
	if err := os.WriteFile(path, read, 0o700); err != nil {
		return fmt.Errorf("write %s: %w", second, err)
	}
	return nil
}

func (l *Library) write(name string, from io.Reader) error {
	path := filepath.Join(l.dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	defer func() { _ = file.Close() }()
	if _, err := io.Copy(file, from); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// Carried reports whether this program has an inference library for the machine
// it is running on.
func Carried() bool { return embedded.Present() }
