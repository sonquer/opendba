package local

import "os"

// Room is what a machine has to spare where the models are kept.
//
// Memory is left unknown until the inference library is open, because the only
// honest answer before that needs the backend to report it. Once a model is
// loaded the engine says exactly how much each device has, and a guess made
// beforehand would only disagree with it.
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

// parent is the directory one level up. Room walks up with it until it finds
// something that exists, because the models directory and every level above it
// may be waiting to be made, and it is the disk underneath that has the room.
func parent(dir string) string {
	for i := len(dir) - 1; i > 0; i-- {
		if dir[i] == '/' || dir[i] == '\\' {
			return dir[:i]
		}
	}
	return "."
}
