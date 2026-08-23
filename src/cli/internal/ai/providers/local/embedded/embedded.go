// Package embedded carries the inference library inside the program.
//
// purego opens a shared library by path, and there is no way on any of these
// systems to open one straight out of memory, so the bytes still reach the disk
// once. What they do not do is reach the network: they come out of the binary
// that was built and signed, rather than out of a release that could have been
// replaced under the tag it was published on.
package embedded

import (
	"fmt"
	"io/fs"
)

// Files is the library this build carries, by file name. A machine nobody
// publishes a build for gets nothing, and the caller fetches instead.
func Files() (map[string][]byte, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := libraries.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read the libraries carried in this program: %w", err)
	}
	held := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		read, err := fs.ReadFile(libraries, dir+"/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s out of this program: %w", entry.Name(), err)
		}
		held[entry.Name()] = read
	}
	return held, nil
}

// Present reports whether this build carries anything at all.
func Present() bool { return dir != "" }
