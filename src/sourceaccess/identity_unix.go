//go:build linux || darwin

package sourceaccess

import (
	"fmt"
	"os"
	"syscall"
)

func fileIdentity(f *os.File) (string, error) {
	i, err := f.Stat()
	if err != nil {
		return "", err
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("unsupported file identity")
	}
	return fmt.Sprintf("%x:%x", s.Dev, s.Ino), nil
}
