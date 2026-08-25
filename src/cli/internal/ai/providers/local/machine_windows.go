//go:build windows

package local

import "golang.org/x/sys/windows"

// free is how much room is left where the models are kept. A number nobody
// could take comes back negative, because zero would mean a full disk.
func free(dir string) int64 {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return -1
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(path, &available, nil, nil); err != nil {
		return -1
	}
	return int64(available)
}
