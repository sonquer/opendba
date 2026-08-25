//go:build windows

package config

import "os"

// verifyMode allows every file. Windows has no mode saying who else may read
// one, so there is nothing here to refuse.
func verifyMode(string, os.FileMode) error { return nil }

// enforceDirMode has nothing to tighten. A Windows directory carries no mode
// saying who else may read it, so there is nothing here to read or to change.
func enforceDirMode(string) error { return nil }
