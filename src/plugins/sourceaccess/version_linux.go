//go:build linux

package sourceaccess

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func fileChangeVersion(f *os.File) (string, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &stat); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d", stat.Ctim.Sec, stat.Ctim.Nsec), nil
}
