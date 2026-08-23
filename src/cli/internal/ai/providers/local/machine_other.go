//go:build !darwin && !linux

package local

// free reports that nobody asked the right question. Reading the room left on a
// disk is a different call on every system, and a number that is guessed is
// worse than one that is missing: the screen says it does not know, and the
// verdict is made on what is known.
func free(string) int64 { return -1 }
