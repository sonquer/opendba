package embedded

import (
	"runtime"
	"strings"
	"testing"
)

// TestThisProgramCarriesABuild is the check that the build tags line up. Every
// platform this program is released for has a directory of libraries beside
// this file, and a tag that stopped matching would swap the whole lot for the
// empty fallback without anything failing to compile.
func TestThisProgramCarriesABuild(t *testing.T) {
	if !Present() {
		t.Skipf("no build is carried for %s/%s, which is only right for a platform we do not release", runtime.GOOS, runtime.GOARCH)
	}
	files, err := Files()
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if len(files) == 0 {
		t.Fatal("Present() says there is a build and Files() is empty")
	}
	for name, bytes := range files {
		if strings.ContainsAny(name, `/\`) {
			t.Fatalf("%q is a path rather than a name, so it would be written outside the library directory", name)
		}
		if len(bytes) == 0 {
			t.Fatalf("%s is empty", name)
		}
	}
}

// TestFilesIsTheSameEveryTime matters because the caller writes them to disk and
// then loads them: a map assembled per call would be a copy of tens of megabytes
// on a screen redraw.
func TestFilesIsTheSameEveryTime(t *testing.T) {
	first, err := Files()
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	second, err := Files()
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("Files() gave %d names and then %d", len(first), len(second))
	}
	if (len(first) > 0) != Present() {
		t.Fatalf("Present() = %v and Files() gave %d names", Present(), len(first))
	}
}
