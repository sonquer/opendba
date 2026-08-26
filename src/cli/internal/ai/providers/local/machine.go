package local

import "os"

// Room is what a machine has to spare where the models are kept.
func Room(dir string) Machine {
	at := dir
	for range 64 {
		if _, err := os.Stat(at); err == nil {
			return Machine{FreeDisk: free(at)}
		}
		above := parent(at)
		if above == at {
			break
		}
		at = above
	}
	return Machine{FreeDisk: -1}
}

// parent is the directory one level up.
func parent(dir string) string {
	for i := len(dir) - 1; i > 0; i-- {
		if dir[i] == '/' || dir[i] == '\\' {
			return dir[:i]
		}
	}
	return "."
}
