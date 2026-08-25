//go:build windows

package llama

import "golang.org/x/sys/windows"

// searchPath makes a directory findable by the loader. Passing a full path to
// LoadLibrary opens that file but does not put its directory on the search
// path, so its own imports are still looked for by bare name: ggml.dll asks for
// ggml-base.dll, which sits beside it and would not otherwise be found.
func searchPath(dir string) error { return windows.SetDllDirectory(dir) }
