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

// Build is the llama.cpp release this program is written against.
const Build = "b10587"

// Binding is the version of the Go binding that Build was chosen for.
const Binding = "v1.24.0"

// ErrNoAsset is what a machine this program carries no build for reports.
var ErrNoAsset = errors.New("this program carries no inference library for this machine")

// Library is the inference library on this machine.
type Library struct{ dir string }

// NewLibrary reads and writes the library under a directory.
func NewLibrary(dir string) *Library { return &Library{dir: dir} }

// Dir is where the library is kept, which is what the engine is opened with.
func (l *Library) Dir() string { return l.dir }

// Present reports whether enough of the library is here to load a model.
func (l *Library) Present() bool { return len(l.Missing()) == 0 }

// Missing lists the libraries that have to be there and are not.
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

// alias is the second name a library has to answer to, or nothing where a system
// does not use one.
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

// also gives a library the second name its neighbours link against.
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
