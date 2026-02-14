//go:build !windows

package api

import (
	"fmt"
	"syscall"
)

// checkDisk verifies that the filesystem at path has at least minBytes free.
// NOTE: The caller passes "." (CWD), which may be a different mount than the
// DB path or workspace directory. On split-mount deployments, this check may
// not reflect the free space available to those volumes.
func checkDisk(path string, minBytes uint64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	var bsize uint64
	if stat.Bsize > 0 {
		bsize = uint64(stat.Bsize)
	}
	free := stat.Bavail * bsize
	if free < minBytes {
		return fmt.Errorf("low disk space: %d MB free, need %d MB", free/(1024*1024), minBytes/(1024*1024))
	}
	return nil
}
