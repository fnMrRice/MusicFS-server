package config

import (
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func lockConfigFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

func restrictReplacement(_, temporary string, original os.FileInfo) error {
	wanted, ok := original.Sys().(*syscall.Stat_t)
	if !ok {
		return os.ErrPermission
	}
	current, err := os.Stat(temporary)
	if err != nil {
		return err
	}
	actual, ok := current.Sys().(*syscall.Stat_t)
	if !ok {
		return os.ErrPermission
	}
	if wanted.Uid != actual.Uid || wanted.Gid != actual.Gid {
		if err := os.Chown(temporary, int(wanted.Uid), int(wanted.Gid)); err != nil {
			return err
		}
	}
	return os.Chmod(temporary, original.Mode().Perm())
}

func replaceConfig(temporary, path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	// Check support before replacing the old file, and persist the rename afterwards.
	if err := dir.Sync(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	return dir.Sync()
}
