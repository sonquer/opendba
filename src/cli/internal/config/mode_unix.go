//go:build !windows

package config

import (
	"fmt"
	"os"
)

// verifyMode refuses a configuration file anyone but its owner can read. It is
// written once per system rather than once with a test inside it, so that the
// systems which have no modes to check carry none of this and the systems which
// do are held to all of it.
func verifyMode(path string, mode os.FileMode) error {
	if mode&0o077 == 0 {
		return nil
	}
	return fmt.Errorf("%s is readable by other users (mode %04o); run chmod 600 on it", path, mode)
}

// enforceDirMode tightens a directory another user can read. It is only ever a
// directory this program made, so the mode it wants is the mode it asked for
// and anything looser is something the system decided on its own.
func enforceDirMode(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", dir, err)
	}
	if info.Mode().Perm()&0o077 == 0 {
		return nil
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("tighten permissions on %s: %w", dir, err)
	}
	return nil
}
