//go:build !windows

package api

import (
	"fmt"
	"syscall"
)

// checkDisk verifies that the filesystem at path has at least minBytes free.
func checkDisk(path string, minBytes uint64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	free := stat.Bavail * uint64(stat.Bsize)
	if free < minBytes {
		return fmt.Errorf("low disk space: %d MB free, need %d MB", free/(1024*1024), minBytes/(1024*1024))
	}
	return nil
}
