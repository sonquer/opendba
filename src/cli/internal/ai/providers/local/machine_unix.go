//go:build darwin || linux

package local

import "golang.org/x/sys/unix"

// free is how much room is left where the models are kept. A number nobody
// could take comes back negative, because zero would mean a full disk.
func free(dir string) int64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return -1
	}
	return int64(stat.Bavail) * int64(stat.Bsize)
}
