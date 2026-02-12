//go:build windows

package api

// checkDisk is a no-op on Windows (disk space check not supported).
func checkDisk(path string, minBytes uint64) error {
	return nil
}
