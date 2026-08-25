//go:build windows

package config

import (
	"fmt"
	"os"
)

// verifyMode allows every file. Windows has no mode saying who else may read
// one, so there is nothing here to refuse.
func verifyMode(string, os.FileMode) error { return nil }

// enforceDirMode has no mode to tighten, for the same reason. It still reports
// a directory that is not there, so that what Ensure says does not depend on
// the system it is running on.
func enforceDirMode(dir string) error {
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("inspect %s: %w", dir, err)
	}
	return nil
}
