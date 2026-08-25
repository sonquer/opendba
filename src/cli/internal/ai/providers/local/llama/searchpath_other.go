//go:build !windows

package llama

// searchPath is what the loader needs told about a directory, which on systems
// that resolve an import against the directory of the library that made it is
// nothing.
func searchPath(string) error { return nil }
