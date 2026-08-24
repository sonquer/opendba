//go:build !darwin && !linux

package local

// free reports that nobody asked the right question.
func free(string) int64 { return -1 }
