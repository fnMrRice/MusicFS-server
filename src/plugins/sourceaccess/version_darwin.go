//go:build darwin

package sourceaccess

import (
	"fmt"
	"os"
	"syscall"
)

func fileChangeVersion(f *os.File) (string, error) {
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("file change information unavailable")
	}
	return fmt.Sprintf("%d:%d", stat.Ctimespec.Sec, stat.Ctimespec.Nsec), nil
}
