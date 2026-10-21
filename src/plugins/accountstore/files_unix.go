//go:build !windows

package accountstore

import "os"

func protect(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	mode := os.FileMode(0600)
	if info.IsDir() {
		mode = 0700
	}
	return os.Chmod(path, mode)
}
func replace(from, to string) error { return os.Rename(from, to) }
